// Package cachecmd holds the `jobsworth cache` command group (spec-cache):
// path, ls, clear, warm, verify, stats.
package cachecmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/fetch"
)

// Schema describes the group itself; subcommands carry their own.
var Schema = cli.Schema{Description: "inspect and manage the fetch cache"}

var cacheDirFlag = cli.Flag{Name: "cache-dir", TakesValue: true, ValueName: "path",
	Description: "cache root; default: $JOBSWORTH_CACHE_DIR or the user cache dir"}

var formatFlag = cli.Flag{Name: "format", TakesValue: true, ValueName: "format", AllowedValues: []string{"table", "json"},
	Description: "output format; default: table"}

// Env is what the subcommands read from the process; tests inject it.
type Env struct {
	Cwd    string
	Home   string
	Getenv func(string) string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// StdinIsTerminal enables confirmation prompts.
	StdinIsTerminal bool
	// Now is the clock for ages and TTLs; nil means time.Now.
	Now func() time.Time
	// GHToken is the `gh auth token` source for warm; nil disables it.
	GHToken func() (string, bool)
}

func defaultEnv() Env {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return Env{
		Cwd: cwd, Home: home, Getenv: os.Getenv,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdinIsTerminal: cli.IsTerminal(os.Stdin),
		GHToken:         fetch.GHCLIToken,
	}
}

// Subcommands returns the dispatch table for main.
func Subcommands() map[string]cli.Command {
	wrap := func(name string) func([]string, map[string][]string) error {
		return func(args []string, flags map[string][]string) error {
			return RunWith(name, args, flags, defaultEnv())
		}
	}
	return map[string]cli.Command{
		"path":   {Run: wrap("path"), Schema: pathSchema},
		"ls":     {Run: wrap("ls"), Schema: lsSchema},
		"clear":  {Run: wrap("clear"), Schema: clearSchema},
		"warm":   {Run: wrap("warm"), Schema: warmSchema},
		"verify": {Run: wrap("verify"), Schema: verifySchema},
		"stats":  {Run: wrap("stats"), Schema: statsSchema},
	}
}

// RunWith dispatches one subcommand with an explicit environment.
func RunWith(sub string, args []string, flags map[string][]string, env Env) error {
	if env.Now == nil {
		env.Now = time.Now
	}
	switch sub {
	case "path":
		return runPath(flags, env)
	case "ls":
		return runLs(args, flags, env)
	case "clear":
		return runClear(args, flags, env)
	case "warm":
		return runWarm(args, flags, env)
	case "verify":
		return runVerify(flags, env)
	case "stats":
		return runStats(flags, env)
	}
	return cli.Exit(cli.ExitUsage, fmt.Errorf("unknown cache subcommand %q", sub))
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

// open resolves the cache root (flag, env, default) and returns a cache
// with the env's clock. A root that exists but is not a directory is a
// usage error.
func open(flags map[string][]string, env Env) (*cache.Cache, error) {
	flag := first(flags, "cache-dir")
	if flag == "~" || strings.HasPrefix(flag, "~/") {
		flag = filepath.Join(env.Home, strings.TrimPrefix(flag, "~"))
	}
	root, err := cache.DefaultRoot(flag, env.Getenv)
	if err != nil {
		return nil, cli.Exit(cli.ExitUsage, err)
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(env.Cwd, root)
	}
	if st, err := os.Stat(root); err == nil && !st.IsDir() {
		return nil, cli.Exit(cli.ExitUsage, fmt.Errorf("cache dir %s is not a directory", root))
	}
	c := cache.New(root)
	c.Now = env.Now
	return c, nil
}

// --- path (§2.1) ------------------------------------------------------------

var pathSchema = cli.Schema{Description: "print the absolute cache root", Flags: []cli.Flag{cacheDirFlag}}

func runPath(flags map[string][]string, env Env) error {
	c, err := open(flags, env)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, c.Root)
	return nil
}

// --- shared rendering -------------------------------------------------------

// humanSize renders bytes like du -h: 512, 12K, 2.1M.
func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d", n)
	case n < 1024*1024:
		return fmt.Sprintf("%dK", (n+512)/1024)
	case n < 10*1024*1024:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%dM", (n+512*1024)/(1024*1024))
	}
}

// humanAge renders a duration: 45s, 3m, 2h 10m, 5d.
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func short(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}

// table renders aligned columns; the last column is not padded.
func table(w io.Writer, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths)-1 && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			if i == len(r)-1 {
				b.WriteString(c)
			} else {
				fmt.Fprintf(&b, "%-*s  ", widths[i], c)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	switch unit {
	case "branch":
		return fmt.Sprintf("%d branches", n)
	case "entry":
		return fmt.Sprintf("%d entries", n)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// confirm asks a y/N question when stdin is a terminal and --yes is absent.
func confirm(flags map[string][]string, env Env, question string) bool {
	if has(flags, "yes") || !env.StdinIsTerminal {
		return true
	}
	fmt.Fprintf(env.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(env.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// checkEnv converts to the check command's environment for sessions.
func checkEnv(env Env) checkcmd.Env {
	return checkcmd.Env{
		Cwd: env.Cwd, Home: env.Home, Getenv: env.Getenv, GHToken: env.GHToken,
		Stdout: env.Stdout, Stderr: env.Stderr,
	}
}
