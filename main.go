package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"sort"

	cachecmd "github.com/najemi-software/jobsworth/cmd/cache"
	"github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/cmd/explain"
	graphcmd "github.com/najemi-software/jobsworth/cmd/graph"
	initcmd "github.com/najemi-software/jobsworth/cmd/init"
	rulescmd "github.com/najemi-software/jobsworth/cmd/rules"
	shapecmd "github.com/najemi-software/jobsworth/cmd/shape"
	versioncmd "github.com/najemi-software/jobsworth/cmd/version"
	"github.com/najemi-software/jobsworth/internal/cli"
)

//go:embed VERSION
var version string

// command is one entry of the multi-command dispatch table: the entrypoint and
// the arg/flag schema main parses and validates against before calling it.
type command struct {
	run    func(args []string, flags map[string][]string) error
	schema cli.Schema
	// subs, when set, makes this verb a group: the next token selects the
	// subcommand, which is parsed against its own schema.
	subs map[string]cli.Command
}

// commands maps each CLI verb to its entrypoint and schema. Verbs are parsed
// and validated here in main; entrypoints receive the clean (args, flags) pair
// and never touch argv themselves.
var commands = map[string]command{
	"cache":   {schema: cachecmd.Schema, subs: cachecmd.Subcommands()},
	"check":   {run: check.Run, schema: check.Schema},
	"rules":   {run: rulescmd.Run, schema: rulescmd.Schema},
	"shape":   {run: shapecmd.Run, schema: shapecmd.Schema},
	"explain": {run: explain.Run, schema: explain.Schema},
	"graph":   {run: graphcmd.Run, schema: graphcmd.Schema},
	"init":    {run: initcmd.Run, schema: initcmd.Schema},
	"version": {run: versioncmd.Run, schema: versioncmd.Schema},
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "usage: jobsworth <command> [flags]\n\nCommands:\n")
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, n := range names {
		fmt.Fprintf(w, "  %-*s  %s\n", width, n, commands[n].schema.Description)
	}
	fmt.Fprintf(w, "\nRun 'jobsworth <command> --help' for a command's flags.\n")
}

// groupUsage prints a command group's subcommand list.
func groupUsage(w io.Writer, verb string, cmd command) {
	fmt.Fprintf(w, "usage: jobsworth %s <subcommand> [flags]\n\n  %s\n\nSubcommands:\n", verb, cmd.schema.Description)
	names := make([]string, 0, len(cmd.subs))
	for name := range cmd.subs {
		names = append(names, name)
	}
	sort.Strings(names)
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, n := range names {
		fmt.Fprintf(w, "  %-*s  %s\n", width, n, cmd.subs[n].Schema.Description)
	}
	fmt.Fprintf(w, "\nRun 'jobsworth %s <subcommand> --help' for a subcommand's flags.\n", verb)
}

// wantsHelp reports whether the tokens ask for help: a literal --help or -h
// anywhere before a bare "--" (everything after "--" is positional and never
// treated as a help request).
func wantsHelp(tokens []string) bool {
	for _, tok := range tokens {
		if tok == "--" {
			return false
		}
		if tok == "--help" || tok == "-h" {
			return true
		}
	}
	return false
}

func main() {
	// The VERSION embed lives here (go:embed can't reach the repo root from
	// cmd/version), so hand it over before any dispatch can print it.
	versioncmd.Version = version
	initcmd.Version = version

	verb, rest, ok := cli.SplitCommand(os.Args)
	if !ok {
		usage(os.Stderr)
		os.Exit(cli.ExitUsage)
	}

	// Flag spellings handled at the top level: -v/--version print the version
	// like the `version` command; --help/-h print the command list.
	switch verb {
	case "-v", "--version":
		if err := versioncmd.Run(nil, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case "--help", "-h":
		usage(os.Stdout)
		return
	}

	cmd, registered := commands[verb]
	if !registered {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", verb)
		usage(os.Stderr)
		os.Exit(cli.ExitUsage)
	}

	// A command group dispatches on its first token (e.g. `cache ls`).
	if cmd.subs != nil {
		if len(rest) == 0 || rest[0] == "--help" || rest[0] == "-h" {
			groupUsage(os.Stdout, verb, cmd)
			if len(rest) == 0 {
				os.Exit(cli.ExitUsage)
			}
			return
		}
		sub, ok := cmd.subs[rest[0]]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown %s subcommand: %s\n\n", verb, rest[0])
			groupUsage(os.Stderr, verb, cmd)
			os.Exit(cli.ExitUsage)
		}
		verb, rest, cmd = verb+" "+rest[0], rest[1:], command{run: sub.Run, schema: sub.Schema}
	}

	// --help is answered here from the command's schema — it never reaches the
	// command's entrypoint (and would otherwise be rejected as an unknown flag).
	if wantsHelp(rest) {
		fmt.Print(cli.Help("jobsworth "+verb, cmd.schema))
		return
	}

	args, flags, err := cli.ParseArgs(cmd.schema, rest)
	if err == nil {
		err = cli.Validate(cmd.schema, args, flags)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", verb, err)
		os.Exit(cli.ExitUsage)
	}

	if err := cmd.run(args, flags); err != nil {
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(cli.ExitCode(err, cli.ExitFindings))
	}
}
