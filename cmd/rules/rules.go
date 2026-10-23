// Package rules holds the entrypoint for `jobsworth rules` (spec-rules §2):
// list the registry, explain one rule, or generate the reference page.
package rules

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Schema declares what `jobsworth rules [<id>]` accepts (spec-rules §2.1).
var Schema = cli.Schema{
	MinArgs:     0,
	MaxArgs:     1,
	ArgNames:    []string{"id"},
	Description: "list the rule registry, or explain one rule",
	Flags: []cli.Flag{
		{Name: "family", TakesValue: true, Repeatable: true, ValueName: "name",
			Description: "only rules in that family (list)"},
		{Name: "severity", TakesValue: true, ValueName: "level", AllowedValues: []string{"error", "warning", "note", "off"},
			Description: "only rules whose effective severity is that; off lists disabled rules (list)"},
		{Name: "category", TakesValue: true, Repeatable: true, ValueName: "name",
			AllowedValues: rules.Categories, Description: "only rules of this category: correctness, security, hygiene, performance (list)"},
		{Name: "enabled", Description: "only rules that run (list)"},
		{Name: "disabled", Description: "only rules that are off (list)"},
		{Name: "changed", Description: "only rules whose effective severity differs from the default (list)"},
		{Name: "config", TakesValue: true, ValueName: "path",
			Description: "config file; default: .jobsworth.yml of the repo at the current directory, if any"},
		{Name: "no-config", Description: "ignore any config; show defaults only"},
		{Name: "format", TakesValue: true, ValueName: "format", AllowedValues: []string{"table", "json", "markdown"},
			Description: "output format; default: table, or markdown with --doc"},
		{Name: "doc", Description: "emit the full rules reference in markdown (list)"},
	},
}

// Env is what Run reads from the process; tests inject it.
type Env struct {
	Cwd              string
	Home             string
	Stdout           io.Writer
	Stderr           io.Writer
	StdoutIsTerminal bool
}

// Run is the command entrypoint.
func Run(args []string, flags map[string][]string) error {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return RunWith(args, flags, Env{
		Cwd: cwd, Home: home, Stdout: os.Stdout, Stderr: os.Stderr,
		StdoutIsTerminal: isTerminal(os.Stdout),
	})
}

// RunWith is Run with an explicit environment.
func RunWith(args []string, flags map[string][]string, env Env) error {
	first := func(name string) string {
		if v := flags[name]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	has := func(name string) bool { _, ok := flags[name]; return ok }

	// Config (spec-rules §2.1, §6).
	var cfg config.Config
	cfgPath := ""
	if !has("no-config") {
		if p := first("config"); p != "" {
			c, err := config.Load(check.ExpandTilde(p, env.Home), rules.IsKnown)
			if err != nil {
				return cli.Exit(cli.ExitUsage, err)
			}
			cfg, cfgPath = c, filepath.Join(c.Dir, config.FileName)
			if filepath.Base(p) != config.FileName {
				cfgPath = check.ExpandTilde(p, env.Home)
			}
		} else {
			root := check.RepoRoot(env.Cwd)
			c, err := config.LoadDefault(root, rules.IsKnown)
			if err != nil {
				return cli.Exit(cli.ExitUsage, err)
			}
			if c.Dir != "" {
				cfg, cfgPath = c, filepath.Join(c.Dir, config.FileName)
			}
		}
	}
	noConfig := has("no-config")

	format := first("format")
	if has("doc") && format == "" {
		format = "markdown"
	}
	if format == "" {
		format = "table"
	}

	if len(args) == 1 {
		return explain(args[0], format, cfg, cfgPath, noConfig, env)
	}

	// List (spec-rules §4.1, §4.3, §4.4).
	var sel []entry
	families := flags["family"]
	for _, r := range rules.Rules {
		e := newEntry(r, cfg)
		if len(families) > 0 && !contains(families, r.Family) {
			continue
		}
		if cats := flags["category"]; len(cats) > 0 && !contains(cats, rules.CategoryOf(r)) {
			continue
		}
		if s := first("severity"); s != "" && e.effectiveName() != s {
			continue
		}
		if has("enabled") && e.off {
			continue
		}
		if has("disabled") && !e.off {
			continue
		}
		if has("changed") && !e.changed {
			continue
		}
		sel = append(sel, e)
	}
	for _, f := range families {
		if !contains(rules.Families, f) {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("unknown family %q (known: %s)", f, strings.Join(rules.Families, ", ")))
		}
	}

	switch format {
	case "json":
		return writeListJSON(env.Stdout, sel, cfgPath)
	case "markdown":
		if has("doc") {
			return writeDoc(env.Stdout, sel, noConfig)
		}
		writeListMarkdown(env.Stdout, sel, noConfig)
		return nil
	default:
		writeListTable(env.Stdout, sel, families, cfgPath, noConfig, env.StdoutIsTerminal)
		return nil
	}
}

// explain renders one rule (spec-rules §4.2, §4.3).
func explain(id, format string, cfg config.Config, cfgPath string, noConfig bool, env Env) error {
	r, canonical, ok := rules.Lookup(id)
	if !ok {
		near := rules.Nearest(id, 3)
		return cli.Exit(cli.ExitFindings, fmt.Errorf("unknown rule %q; did you mean: %s", id, strings.Join(near, ", ")))
	}
	doc, err := rules.LoadDoc(canonical)
	if err != nil {
		return err
	}
	e := newEntry(r, cfg)
	switch format {
	case "json":
		return writeExplainJSON(env.Stdout, e, doc, cfgPath)
	case "markdown":
		if id != canonical {
			fmt.Fprintf(env.Stdout, "> note: '%s' is an alias of '%s'\n\n", id, canonical)
		}
		writeHeaderTable(env.Stdout, e, noConfig)
		fmt.Fprintln(env.Stdout)
		fmt.Fprint(env.Stdout, stripTitle(doc.Markdown))
		return nil
	default:
		if id != canonical {
			fmt.Fprintf(env.Stdout, "note: '%s' is an alias of '%s'\n\n", id, canonical)
		}
		writeExplainTable(env.Stdout, e, doc, noConfig, env.StdoutIsTerminal)
		return nil
	}
}

// entry is a rule with its effective severity under the current config.
type entry struct {
	rules.Rule
	effective finding.Severity
	off       bool
	changed   bool
}

func newEntry(r rules.Rule, cfg config.Config) entry {
	sev, off, changed := rules.Effective(r, cfg)
	return entry{Rule: r, effective: sev, off: off, changed: changed}
}

func (e entry) defaultName() string {
	if e.Off || e.Status == rules.Experimental {
		return "off"
	}
	return e.Severity.String()
}

func (e entry) effectiveName() string {
	if e.off {
		return "off"
	}
	return e.effective.String()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isTerminal(f *os.File) bool { return cli.IsTerminal(f) }
