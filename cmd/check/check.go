// Package check holds the entrypoint for `jobsworth check`: validate GitHub
// workflow and action files, following their `uses:` graph.
package check

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/report"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Schema declares what `jobsworth check` accepts (spec §1.1–1.2): any number
// of positional entries (paths or globs) and the flags below. main parses and
// validates argv against this before calling Run.
var Schema = cli.Schema{
	MinArgs:     0,
	MaxArgs:     -1,
	ArgNames:    []string{"path|glob"},
	Description: "validate workflow and action files, following their uses: graph",
	Flags: []cli.Flag{
		{
			Name:        "repo",
			TakesValue:  true,
			Repeatable:  true,
			ValueName:   "owner/name=path",
			Description: "link an external repo to a local checkout; the checkout is validated instead of fetching",
		},
		{
			Name:        "offline",
			Description: "never touch the network; cache hits are used, misses become warnings",
		},
		{
			Name:        "no-cache",
			Description: "ignore the on-disk cache for this run (read and write)",
		},
		{
			Name:        "cache-dir",
			TakesValue:  true,
			ValueName:   "path",
			Description: "override the cache root; default: $JOBSWORTH_CACHE_DIR or the user cache dir",
		},
		{
			Name:          "fail-on",
			TakesValue:    true,
			ValueName:     "severity",
			AllowedValues: []string{"error", "warning", "note"},
			Description:   "lowest severity that makes the exit code non-zero; default: error",
		},
		{
			Name:          "format",
			TakesValue:    true,
			ValueName:     "format",
			AllowedValues: []string{"text", "json"},
			Description:   "output format; default: text",
		},
		{
			Name:        "config",
			TakesValue:  true,
			ValueName:   "path",
			Description: "config file; default: .jobsworth.yml at the entry's repo root, if present",
		},
		{
			Name:        "show-suppressed",
			Description: "also print findings hidden by jobsworth-ignore comments, with their reasons",
		},
		{
			Name:        "category",
			TakesValue:  true,
			Repeatable:  true,
			ValueName:   "name[,name]",
			Description: "only findings of these categories: correctness, security, hygiene, performance",
		},
		{
			Name:        "input",
			TakesValue:  true,
			Repeatable:  true,
			ValueName:   "k=v",
			Description: "seed a workflow_dispatch input of the entry for condition evaluation (advanced §4.3)",
		},
		{
			Name:        "enrich",
			Description: "make read-only GitHub API lookups that suppress or upgrade findings (advanced §11)",
		},
	},
}

// Env is everything Run reads from the process, injected so tests can run
// the full command without touching the real environment.
type Env struct {
	Cwd    string
	Home   string
	Getenv func(string) string
	// GHToken is the `gh auth token` source; nil disables it.
	GHToken func() (string, bool)
	Stdout  io.Writer
	Stderr  io.Writer
	// StderrIsTerminal enables progress lines (§8.1); StdoutIsTerminal
	// enables colour in commands that use it (graph).
	StderrIsTerminal bool
	StdoutIsTerminal bool
}

// Run is the command entrypoint. It receives the clean (args, flags) pair —
// already parsed and validated by main — and never touches argv itself.
func Run(args []string, flags map[string][]string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return cli.Exit(cli.ExitUsage, err)
	}
	home, _ := os.UserHomeDir()
	env := Env{
		Cwd:              cwd,
		Home:             home,
		Getenv:           os.Getenv,
		GHToken:          fetch.GHCLIToken,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		StderrIsTerminal: isTerminal(os.Stderr),
		StdoutIsTerminal: isTerminal(os.Stdout),
	}
	return RunWith(args, flags, env)
}

// RunWith is Run with an explicit environment.
func RunWith(args []string, flags map[string][]string, env Env) error {
	first := func(name string) string {
		if v := flags[name]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	_, showSuppressed := flags["show-suppressed"]
	failOn := finding.Error
	if v := first("fail-on"); v != "" {
		failOn, _ = finding.ParseSeverity(v)
	}
	format := first("format")
	if format == "" {
		format = "text"
	}

	categories := map[string]bool{}
	for _, v := range flags["category"] {
		for _, c := range strings.Split(v, ",") {
			c = strings.TrimSpace(c)
			if !contains(rules.Categories, c) {
				return cli.Exit(cli.ExitUsage, fmt.Errorf("--category: unknown category %q (known: %s)", c, strings.Join(rules.Categories, ", ")))
			}
			categories[c] = true
		}
	}
	inputs := map[string]string{}
	for _, kv := range flags["input"] {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("--input: expected k=v, got %q", kv))
		}
		inputs[kv[:eq]] = kv[eq+1:]
	}
	_, enrich := flags["enrich"]
	if _, offline := flags["offline"]; offline && enrich {
		return cli.Exit(cli.ExitUsage, errors.New("--offline and --enrich cannot be combined"))
	}

	sess, err := NewSession(args, flags, env, true)
	if err != nil {
		return err
	}
	if first("fail-on") == "" {
		failOn = sess.Base.FailOn
	}
	entries, links, fetcher, store := sess.Entries, sess.Links, sess.Fetcher, sess.Store

	// Run (§3), streaming text per entry (§8.1). On a terminal, findings
	// are numbered so `explain --last <n>` can name them (spec-explain §5).
	// Two passes (medium review 4 D11): every entry runs first, so a
	// callee that is itself an entry is reported under its own listing
	// even when a caller sorts before it; then the text is printed with
	// one cross-entry memory, a callee's finding once per run (N11).
	results := make([]check.Result, 0, len(entries))
	for i, e := range entries {
		r := check.RunEntry(e, check.Options{Links: links, Fetcher: fetcher, Config: sess.Configs[sess.Roots[i]], Inputs: inputs, Enrich: enrich, Store: store})
		if len(categories) > 0 {
			r = filterCategories(r, categories)
		}
		results = append(results, r)
	}
	var numberer *report.Numberer
	// The numbers go to stdout, so stdout decides (explain review N4).
	if env.StdoutIsTerminal && format == "text" {
		numberer = &report.Numberer{}
	}
	if format == "text" {
		dedupe := report.NewDedupe()
		report.Seed(dedupe, results)
		for _, r := range results {
			report.TextDedupe(env.Stdout, r, failOn, env.Cwd, showSuppressed, numberer, dedupe)
		}
	}
	cache.WriteLastRun(store, env.Cwd, lastRun(results, flags, sess))
	if reset, hit := fetch.RateLimitReset(fetcher); hit {
		fmt.Fprintf(env.Stderr, "warning: GitHub API rate limit exceeded; further fetches were skipped (resets at %s)\n", reset.Local().Format("15:04:05"))
	}

	WriteRunStats(store, fetcher, "check")

	summary := report.Summarize(results, failOn)
	switch format {
	case "json":
		if err := report.JSON(env.Stdout, results, failOn); err != nil {
			return err
		}
	default:
		report.TextSummary(env.Stdout, summary)
	}
	if summary.Failed > 0 {
		return cli.Exit(cli.ExitFindings, nil)
	}
	return nil
}

// filterCategories keeps only findings whose rule is in the wanted
// categories (advanced §10).
func filterCategories(r check.Result, want map[string]bool) check.Result {
	for i := range r.Files {
		var kept []finding.Finding
		for _, f := range r.Files[i].Findings {
			if want[rules.CategoryOfID(f.Rule)] {
				kept = append(kept, f)
			}
		}
		r.Files[i].Findings = kept
	}
	return r
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// lastRun builds the record `explain --last` reads (spec-explain §5): the
// findings in printed order across all entries.
func lastRun(results []check.Result, flags map[string][]string, sess *Session) cache.RunRecord {
	rec := cache.RunRecord{At: time.Now().UTC(), Completed: true, Flags: map[string][]string{}}
	for k, v := range flags {
		rec.Flags[k] = v
	}
	if sess.Base.Dir != "" {
		rec.Config = filepath.Join(sess.Base.Dir, config.FileName)
	}
	d := report.NewDedupe() // the same order and memory as the printed numbers
	report.Seed(d, results)
	for _, r := range results {
		rec.Entries = append(rec.Entries, r.Entry)
		for _, f := range report.Fresh(d, r) {
			rec.Findings = append(rec.Findings, cache.RunFinding{Entry: r.Entry, Finding: f})
		}
	}
	return rec
}

// isTerminal reports whether f is an interactive terminal.
func isTerminal(f *os.File) bool { return cli.IsTerminal(f) }
