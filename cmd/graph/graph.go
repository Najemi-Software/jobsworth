// Package graph holds the entrypoint for `jobsworth graph`: the same walk
// as check with the rules turned off and the traversal itself as output
// (spec-graph).
package graph

import (
	"fmt"
	"os"
	"strconv"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/graph"
	"github.com/najemi-software/jobsworth/internal/repolink"
)

// Schema declares what `jobsworth graph` accepts (spec-graph §2.1).
var Schema = cli.Schema{
	MinArgs:     1,
	MaxArgs:     1,
	ArgNames:    []string{"path"},
	Description: "show what check would look at and where each dependency comes from",
	Flags: []cli.Flag{
		{Name: "format", TakesValue: true, ValueName: "format", AllowedValues: []string{"tree", "json", "dot", "mermaid"}, Description: "output format; default: tree"},
		{Name: "depth", TakesValue: true, ValueName: "n", Description: "stop descending n levels below the entry; default: unlimited"},
		{Name: "external-only", Description: "collapse local nodes: show only the first external node on each path and its subtree"},
		{Name: "no-dedupe", Description: "print a node's subtree at every occurrence instead of once"},
		{Name: "callers", Description: "inverse mode: who reaches the given local file"},
		{Name: "with", Description: "include each edge's with: keys in tree and json output"},
		{Name: "no-fetch", Description: "fetch nothing; unlinked, uncached external nodes are shown as unresolved"},
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
	if check.IsGlob(args[0]) {
		return cli.Exit(cli.ExitUsage, fmt.Errorf("graph takes one path, not a glob: %s", args[0]))
	}
	depth := -1
	if v := first("depth"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("--depth: expected a non-negative integer, got %q", v))
		}
		depth = n
	}
	format := first("format")
	if format == "" {
		format = "tree"
	}

	sess, err := checkcmd.NewSession(args, flags, env, true)
	if err != nil {
		return err
	}
	entry := sess.Entries[0]
	cfg := sess.Configs[sess.Roots[0]]
	topts := graph.TreeOptions{Cwd: env.Cwd, Home: env.Home, Color: env.StdoutIsTerminal && format == "tree",
		ExternalOnly: has("external-only"), NoDedupe: has("no-dedupe"), Depth: depth}

	var g *check.Graph
	if has("callers") {
		slug := ""
		if lk, ok := repolink.Detect(sess.Roots[0], repolink.Entry); ok {
			slug = lk.Display
		}
		g, err = graph.FindCallers(graph.CallersOptions{Target: entry, Root: sess.Roots[0], Links: sess.Links, Config: cfg, RepoSlug: slug,
			ExtraRoots: []string{check.RepoRoot(env.Cwd)}})
		if err != nil {
			return cli.Exit(cli.ExitUsage, err)
		}
		check.NodeByID(g, g.Entry).Display = check.RelOrAbs(entry, env.Cwd)
	} else {
		g = check.NewGraph()
		opts := check.Options{Links: sess.Links, Fetcher: sess.Fetcher, Config: cfg, LoadOnly: true,
			Graph: g, GraphWith: has("with"), NoFetch: has("no-fetch")}
		if depth >= 0 {
			opts.Depth = &depth
		}
		res := check.RunEntry(entry, opts)
		if len(g.Nodes) == 0 {
			msg := "entry could not be loaded"
			if fs := check.AllFindings(res); len(fs) > 0 {
				msg = fs[0].Message
			}
			return cli.Exit(cli.ExitUsage, fmt.Errorf("%s: %s", check.RelOrAbs(entry, env.Cwd), msg))
		}
		check.NodeByID(g, g.Entry).Display = check.RelOrAbs(entry, env.Cwd)
		if reset, hit := fetch.RateLimitReset(sess.Fetcher); hit {
			fmt.Fprintf(env.Stderr, "warning: GitHub API rate limit exceeded; further fetches were skipped (resets at %s)\n", reset.Local().Format("15:04:05"))
		}
		if !has("no-fetch") {
			checkcmd.WriteRunStats(sess.Store, sess.Fetcher, "graph")
		}
	}

	switch format {
	case "json":
		return graph.JSON(env.Stdout, g, sess.Fetcher.Stats.APICalls.Load())
	case "dot":
		graph.Dot(env.Stdout, g, env.Cwd)
	case "mermaid":
		graph.Mermaid(env.Stdout, g, env.Cwd)
	default:
		if has("callers") {
			graph.Callers(env.Stdout, g, topts)
		} else {
			graph.Tree(env.Stdout, g, topts)
		}
	}
	return nil
}
