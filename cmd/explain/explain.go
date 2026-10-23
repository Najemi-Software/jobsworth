// Package explain holds the entrypoint for `jobsworth explain` (spec-explain):
// a rule in the abstract (delegated to `rules <id>`), a finding in context
// at <file>:<line>, or the findings of the last check run.
package explain

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/najemi-software/jobsworth/internal/workflow"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	rulescmd "github.com/najemi-software/jobsworth/cmd/rules"
	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/explain"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Schema declares what `jobsworth explain` accepts (spec-explain §2).
var Schema = cli.Schema{
	MinArgs:     0,
	MaxArgs:     1,
	ArgNames:    []string{"id|file:line[:col]|n"},
	Description: "explain a rule, or a finding at file:line in context",
	Flags: []cli.Flag{
		{Name: "rule", TakesValue: true, ValueName: "id", Description: "with a file: restrict to one rule"},
		{Name: "checkouts", TakesValue: true, ValueName: "job", Description: "with a file: show what check believes the job's checkout state is, finding or not"},
		{Name: "last", Description: "explain the findings of the last check run in this directory; a positional <n> picks one"},
		{Name: "format", TakesValue: true, ValueName: "format", AllowedValues: []string{"text", "json", "markdown"}, Description: "output format; default: text"},
		{Name: "brief", Description: "context block only; omit the rule doc"},
		{Name: "no-doc", Description: "same as --brief"},
		{Name: "repo", TakesValue: true, Repeatable: true, ValueName: "owner/name=path", Description: "as check"},
		{Name: "offline", Description: "as check"},
		{Name: "enrich", Description: "as check"},
		{Name: "no-cache", Description: "as check"},
		{Name: "cache-dir", TakesValue: true, ValueName: "path", Description: "as check"},
		{Name: "config", TakesValue: true, ValueName: "path", Description: "as check"},
		{Name: "no-config", Description: "ignore any config (rule explanations only)"},
	},
}

// Env is what Run reads from the process; tests inject it.
type Env struct {
	Cwd              string
	Home             string
	Getenv           func(string) string
	GHToken          func() (string, bool)
	Stdout           io.Writer
	Stderr           io.Writer
	StdoutIsTerminal bool
}

// Run is the command entrypoint.
func Run(args []string, flags map[string][]string) error {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return RunWith(args, flags, Env{
		Cwd: cwd, Home: home, Getenv: os.Getenv, GHToken: fetch.GHCLIToken,
		Stdout: os.Stdout, Stderr: os.Stderr,
		StdoutIsTerminal: cli.IsTerminal(os.Stdout),
	})
}

func first(flags map[string][]string, name string) string {
	if v := flags[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func has(flags map[string][]string, name string) bool {
	_, ok := flags[name]
	return ok
}

// RunWith is Run with an explicit environment.
func RunWith(args []string, flags map[string][]string, env Env) error {
	format := first(flags, "format")
	if format == "" {
		format = "text"
	}
	brief := has(flags, "brief") || has(flags, "no-doc")

	if has(flags, "last") {
		return runLast(args, flags, env, format, brief)
	}
	if len(args) == 0 {
		return cli.Exit(cli.ExitUsage, errors.New("expected a rule id, a file:line location, or --last"))
	}
	arg := args[0]

	// A registered id (not written as a path) is the rule in the abstract.
	if _, _, ok := rules.Lookup(arg); ok && !strings.HasPrefix(arg, "./") && !strings.HasPrefix(arg, "/") {
		rflags := map[string][]string{}
		for _, k := range []string{"config", "no-config"} {
			if v, ok := flags[k]; ok {
				rflags[k] = v
			}
		}
		rf := format
		if rf == "text" {
			rf = "table"
		}
		rflags["format"] = []string{rf}
		return rulescmd.RunWith([]string{arg}, rflags, rulescmd.Env{
			Cwd: env.Cwd, Home: env.Home, Stdout: env.Stdout, Stderr: env.Stderr, StdoutIsTerminal: env.StdoutIsTerminal,
		})
	}

	loc, err := explain.ParseLocation(strings.TrimPrefix(arg, "./"), env.Cwd)
	if err != nil {
		if _, _, ok := rules.Lookup(strings.TrimPrefix(arg, "./")); !ok {
			near := rules.Nearest(arg, 1)
			if !strings.Contains(arg, ":") && !strings.Contains(arg, "/") && !strings.Contains(arg, ".") && len(near) > 0 {
				return cli.Exit(cli.ExitFindings, fmt.Errorf("unknown rule %q; did you mean: %s", arg, near[0]))
			}
		}
		return cli.Exit(cli.ExitUsage, err)
	}
	if job := first(flags, "checkouts"); job != "" {
		return runCheckouts(loc, job, flags, env, format)
	}
	if loc.Line == 0 && first(flags, "rule") == "" {
		return cli.Exit(cli.ExitUsage, fmt.Errorf("%s: give a line (file:line) or --rule <id>", arg))
	}
	if r := first(flags, "rule"); r != "" {
		if _, canon, ok := rules.Lookup(r); ok {
			flags["rule"] = []string{canon}
		} else {
			return cli.Exit(cli.ExitFindings, fmt.Errorf("unknown rule %q; did you mean: %s", r, strings.Join(rules.Nearest(r, 3), ", ")))
		}
	}
	return runLocation(loc, flags, env, format, brief, "", nil)
}

// runLocation checks the file and explains the findings at loc.
// checked is one file's check run, reused across the findings `--last`
// explains in it (explain review 2 N2).
type checked struct {
	res     check.Result
	tr      *check.Trace
	fetcher *fetch.Client
}

// runCheck runs check over the location's file with a trace.
func runCheck(loc explain.Location, flags map[string][]string, env Env) (*checked, error) {
	sess, err := checkcmd.NewSession(nil, sessionFlags(flags), checkEnv(env), false)
	if err != nil {
		return nil, err
	}
	tr := check.NewTrace()
	var res check.Result
	if loc.Remote {
		res = check.RunRef(loc.File, check.Options{Links: sess.Links, Fetcher: sess.Fetcher, Config: sess.Base, Trace: tr})
	} else {
		root := check.RepoRoot(filepath.Dir(loc.File))
		cfg := sess.Base
		if c, ok := sess.Configs[root]; ok {
			cfg = c
		} else if first(flags, "config") == "" {
			if c, err := loadRepoConfig(root); err == nil {
				cfg = c
			}
		}
		_, enrich := flags["enrich"]
		res = check.RunEntry(loc.File, check.Options{Links: sess.Links, Fetcher: sess.Fetcher, Config: cfg, Trace: tr, Enrich: enrich, Store: sess.Store})
	}
	return &checked{res: res, tr: tr, fetcher: sess.Fetcher}, nil
}

// runLocation explains the findings at loc. memo, when non-nil, caches
// the check run per file so several findings in one file re-check it
// once.
func runLocation(loc explain.Location, flags map[string][]string, env Env, format string, brief bool, header string, memo map[string]*checked) error {
	c := memo[loc.File]
	if c == nil {
		var err error
		if c, err = runCheck(loc, flags, env); err != nil {
			return err
		}
		if memo != nil {
			memo[loc.File] = c
		}
	}
	_, source := fetch.ResolveToken(env.Getenv, env.GHToken)
	opts := explain.Options{Rule: first(flags, "rule"), Brief: brief, Env: explain.Env{TokenSource: string(source), Cwd: env.Cwd}}
	for _, l := range fetch.Log(c.fetcher) {
		opts.Env.FetchLog = append(opts.Env.FetchLog, explain.FetchLogEntry(l))
	}
	result := explain.AtLocation(c.res, c.tr, loc, opts)
	if header != "" && format == "text" {
		fmt.Fprintln(env.Stdout, header)
	}
	switch format {
	case "json":
		if err := explain.JSON(env.Stdout, result); err != nil {
			return err
		}
	case "markdown":
		explain.Markdown(env.Stdout, result, env.Cwd)
	default:
		explain.Text(env.Stdout, result, env.Cwd, explain.Style{Terminal: env.StdoutIsTerminal})
	}
	if len(result.Explanations) == 0 {
		return cli.Exit(cli.ExitFindings, nil)
	}
	return nil
}

// runCheckouts is `explain --checkouts <job> <file>` (checkout-inference
// review 1 N2): the checkout block of a job without a finding to hang it
// on, so a user can see why a job is (or is not) clean.
func runCheckouts(loc explain.Location, job string, flags map[string][]string, env Env, format string) error {
	c, err := runCheck(loc, flags, env)
	if err != nil {
		return err
	}
	ft := c.tr.Get(loc.File)
	if ft == nil {
		msg := "entry could not be loaded"
		if fs := check.AllFindings(c.res); len(fs) > 0 {
			msg = fs[0].Message
		}
		return cli.Exit(cli.ExitUsage, fmt.Errorf("%s: %s", check.RelOrAbs(loc.File, env.Cwd), msg))
	}
	if ft.File.Kind == workflow.KindWorkflow && workflow.JobByName(ft.File, job) == nil {
		var names []string
		for _, j := range ft.File.Jobs {
			names = append(names, j.Name)
		}
		return cli.Exit(cli.ExitFindings, fmt.Errorf("no job named '%s' (jobs: %s)", job, strings.Join(names, ", ")))
	}
	if ft.File.Kind != workflow.KindWorkflow {
		job = ""
	}
	lines, data, active := explain.CheckoutLines(ft, job, -1, "")
	if format == "json" {
		doc := map[string]any{"schema": 1, "file": check.RelOrAbs(loc.File, env.Cwd), "job": job}
		for k, v := range data {
			doc[k] = v
		}
		if active != nil {
			doc["active"] = map[string]any{"uses": active.Uses, "step": active.Index + 1, "repository": active.Repository, "path": active.Path}
		}
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	fmt.Fprintf(env.Stdout, "%s  job '%s'\n", check.RelOrAbs(loc.File, env.Cwd), job)
	for _, l := range lines {
		fmt.Fprintln(env.Stdout, l)
	}
	switch {
	case active == nil:
		fmt.Fprintln(env.Stdout, "at the end of the job: no root checkout; local actions are reported as local-action-no-checkout")
	default:
		fmt.Fprintf(env.Stdout, "at the end of the job: root checkout from step #%d (%s)\n", active.Index+1, active.Uses)
	}
	return nil
}

func loadRepoConfig(root string) (config.Config, error) {
	return config.LoadDefault(root, rules.IsKnown)
}

// runLast explains findings from the recorded last run (spec-explain §5).
func runLast(args []string, flags map[string][]string, env Env, format string, brief bool) error {
	root, err := cache.DefaultRoot(first(flags, "cache-dir"), env.Getenv)
	if err != nil {
		return cli.Exit(cli.ExitUsage, err)
	}
	rec, ok := cache.ReadLastRun(cache.New(root), env.Cwd)
	if !ok {
		return cli.Exit(cli.ExitUsage, errors.New("no recorded check run for this directory; run `jobsworth check` first"))
	}
	if !rec.Completed {
		return cli.Exit(cli.ExitUsage, errors.New("last run did not complete"))
	}
	age := time.Since(rec.At).Round(time.Minute)
	if len(args) == 1 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 || n > len(rec.Findings) {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("--last %s: the last run printed %d finding(s)", args[0], len(rec.Findings)))
		}
		rf := rec.Findings[n-1]
		loc := explain.Location{File: rf.Finding.File, Line: rf.Finding.Line, Column: rf.Finding.Column}
		if !filepath.IsAbs(loc.File) {
			loc.Remote = true
		}
		flags["rule"] = []string{rf.Finding.Rule}
		err = runLocation(loc, flags, env, format, brief, "", nil)
		if code := cli.ExitCode(err, cli.ExitFindings); code == cli.ExitFindings && err != nil && err.Error() == "" {
			return cli.Exit(cli.ExitFindings, fmt.Errorf("finding [%d] from %s ago no longer occurs in %s", n, age, display(loc.File, env.Cwd)))
		}
		return err
	}
	if len(rec.Findings) == 0 {
		fmt.Fprintln(env.Stdout, "the last run had no findings")
		return nil
	}
	if format == "text" {
		fmt.Fprintf(env.Stdout, "%d finding(s) in the last run (%s ago); use `jobsworth explain --last <n>` for one\n\n", len(rec.Findings), age)
	}
	// Errors first, grouped by file, in printed order within each group.
	type key struct {
		sev  finding.Severity
		file string
	}
	groups := map[key][]int{}
	var order []key
	for i, rf := range rec.Findings {
		k := key{rf.Finding.Severity, rf.Finding.File}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i+1)
	}
	var failed error
	memo := map[string]*checked{} // one check run per file, not per finding
	for sev := finding.Error; sev <= finding.Note; sev++ {
		for _, k := range order {
			if k.sev != sev {
				continue
			}
			for _, n := range groups[k] {
				rf := rec.Findings[n-1]
				loc := explain.Location{File: rf.Finding.File, Line: rf.Finding.Line, Column: rf.Finding.Column, Remote: !filepath.IsAbs(rf.Finding.File)}
				f2 := map[string][]string{}
				for kk, v := range flags {
					f2[kk] = v
				}
				f2["rule"] = []string{rf.Finding.Rule}
				if err := runLocation(loc, f2, env, format, brief, fmt.Sprintf("[%d]", n), memo); err != nil && failed == nil {
					failed = err
				}
				if format == "text" {
					fmt.Fprintln(env.Stdout)
				}
			}
		}
	}
	return failed
}

func sessionFlags(flags map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, k := range []string{"repo", "offline", "enrich", "no-cache", "cache-dir", "config"} {
		if v, ok := flags[k]; ok {
			out[k] = v
		}
	}
	return out
}

func checkEnv(env Env) checkcmd.Env {
	return checkcmd.Env{Cwd: env.Cwd, Home: env.Home, Getenv: env.Getenv, GHToken: env.GHToken, Stdout: io.Discard, Stderr: env.Stderr}
}

func display(name, cwd string) string {
	if rel, err := filepath.Rel(cwd, name); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return name
}
