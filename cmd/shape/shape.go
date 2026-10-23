// Package shape holds the entrypoint for `jobsworth shape`: what the tool
// believes an expression is, and why (spec-shape).
package shape

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/shapeq"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Schema declares what `jobsworth shape` accepts (spec-shape §2).
var Schema = cli.Schema{
	MinArgs:     1,
	MaxArgs:     2,
	ArgNames:    []string{"path", "expression"},
	Description: "show the inferred shape of an expression, and why",
	Flags: []cli.Flag{
		{Name: "at", TakesValue: true, ValueName: "job[/step]", Description: "evaluation point; a step is an id or #n; for an action, just the step"},
		{Name: "format", TakesValue: true, ValueName: "format", AllowedValues: []string{"text", "json"}, Description: "output format; default: text"},
		{Name: "why", Description: "include the provenance chain (default on for text; adds it to --all json)"},
		{Name: "no-why", Description: "omit the provenance chain from text output"},
		{Name: "depth", TakesValue: true, ValueName: "n", Description: "maximum nesting shown for object and array shapes; default: 4"},
		{Name: "all", Description: "list every expression in the file with its shape"},
		{Name: "repo", TakesValue: true, Repeatable: true, ValueName: "owner/name=path", Description: "as check"},
		{Name: "offline", Description: "as check"},
		{Name: "no-cache", Description: "as check"},
		{Name: "cache-dir", TakesValue: true, ValueName: "path", Description: "as check"},
		{Name: "config", TakesValue: true, ValueName: "path", Description: "as check"},
	},
}

// Run is the command entrypoint.
func Run(args []string, flags map[string][]string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return cli.Exit(cli.ExitUsage, err)
	}
	home, _ := os.UserHomeDir()
	return RunWith(args, flags, checkcmd.Env{
		Cwd: cwd, Home: home, Getenv: os.Getenv, GHToken: fetch.GHCLIToken,
		Stdout: os.Stdout, Stderr: os.Stderr,
		StderrIsTerminal: cli.IsTerminal(os.Stderr), StdoutIsTerminal: cli.IsTerminal(os.Stdout),
	})
}

// RunWith is Run with an explicit environment.
func RunWith(args []string, flags map[string][]string, env checkcmd.Env) error {
	first := func(name string) string {
		if v := flags[name]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	has := func(name string) bool { _, ok := flags[name]; return ok }
	all := has("all")
	if !all && len(args) != 2 {
		return cli.Exit(cli.ExitUsage, errors.New("expected <path> '<expression>' (or --all)"))
	}
	if all && len(args) != 1 {
		return cli.Exit(cli.ExitUsage, errors.New("--all takes the path only"))
	}
	if check.IsGlob(args[0]) {
		return cli.Exit(cli.ExitUsage, fmt.Errorf("shape takes one path, not a glob: %s", args[0]))
	}
	format := first("format")
	if format == "" {
		format = "text"
	}
	depth := 4
	if v := first("depth"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("--depth: expected a positive integer, got %q", v))
		}
		depth = n
	}
	why := format == "text" && !has("no-why") || has("why")

	sess, err := checkcmd.NewSession(args[:1], flags, env, true)
	if err != nil {
		return err
	}
	entry := sess.Entries[0]
	tr := check.NewTrace()
	var follow func(workflow.Uses) bool
	if !all {
		follow = shapeq.FollowFilter(entry, first("at"), args[1])
	}
	res := check.RunEntry(entry, check.Options{Links: sess.Links, Fetcher: sess.Fetcher, Config: sess.Configs[sess.Roots[0]], Trace: tr, FollowRef: follow})
	ft := tr.Get(entry)
	if ft == nil {
		msg := "entry could not be loaded"
		if fs := check.AllFindings(res); len(fs) > 0 {
			msg = fs[0].Message
		}
		return cli.Exit(cli.ExitUsage, fmt.Errorf("%s: %s", check.RelOrAbs(entry, env.Cwd), msg))
	}
	if !has("no-cache") {
		checkcmd.WriteRunStats(sess.Store, sess.Fetcher, "shape")
	}

	if all {
		if at := first("at"); at != "" && ft.File.Kind == workflow.KindWorkflow && workflow.JobByName(ft.File, at) == nil {
			return cli.Exit(cli.ExitFindings, fmt.Errorf("no job named '%s'", at))
		}
		return runAll(ft, entry, first("at"), format, why, depth, env)
	}

	text := args[1]
	point, perr := shapeq.ResolvePoint(ft, first("at"), text)
	if perr != nil {
		var pe *shapeq.PointError
		if errors.As(perr, &pe) && ft.File.Kind != workflow.KindWorkflow && strings.Contains(pe.Msg, "actions have no jobs") {
			return cli.Exit(cli.ExitUsage, perr)
		}
		return cli.Exit(cli.ExitFindings, perr)
	}
	a, err := shapeq.Query(ft, point, text)
	if err != nil {
		a = shapeq.Answer{Point: point, Expression: strings.TrimSpace(text), Error: &shapeq.Error{Rule: "expression-syntax", Message: "expression does not parse: " + err.Error()}}
	}
	a.File = entry
	switch format {
	case "json":
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(shapeq.JSON(a, true, depth)); err != nil {
			return err
		}
	default:
		fmt.Fprint(env.Stdout, shapeq.Text(a, shapeq.Options{Depth: depth, Why: why}))
	}
	if !a.OK() {
		return cli.Exit(cli.ExitFindings, nil)
	}
	return nil
}

// runAll lists every expression with its shape (spec-shape §6), streamed.
func runAll(ft *check.FileTrace, entry, job, format string, why bool, depth int, env checkcmd.Env) error {
	if format == "json" {
		rows := shapeq.All(ft, job)
		docs := []shapeq.Document{}
		for _, r := range rows {
			r.Answer.File = entry
			d := shapeq.JSON(r.Answer, why, depth)
			d.Line = r.Expr.Pos.Line
			d.Site = r.Site
			docs = append(docs, d)
		}
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(docs)
	}
	fmt.Fprintf(env.Stdout, "%5s  %-28s %-56s %s\n", "line", "at", "expression", "shape")
	resolved, anys, total := 0, 0, 0
	shapeq.Each(ft, job, func(r shapeq.Row) { // one line per expression as it is known (§6)
		total++
		render := ""
		switch {
		case r.Answer.Error != nil:
			render = "✗ " + r.Answer.Error.Message
		case r.Answer.Shape == nil || r.Answer.Shape.Kind == shape.Any:
			anys++
			render = "Any"
			if reason := shapeq.OpaqueReason(r.Answer); reason != "" {
				render += "   (opaque: " + reason + ")"
			}
		default:
			resolved++
			render = shape.Render(r.Answer.Shape, depth)
		}
		fmt.Fprintf(env.Stdout, "%5d  %-28s %-56s %s\n", r.Expr.Pos.Line, truncate(r.Site, 28), truncate(r.Expr.Text, 56), render)
	})
	fmt.Fprintf(env.Stdout, "\n%d expressions: %d resolved, %d Any\n", total, resolved, anys)
	return nil
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
