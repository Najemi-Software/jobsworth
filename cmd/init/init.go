// Package initcmd holds the entrypoint for `jobsworth init` (spec-init):
// inspect the repo and write a .jobsworth.yml, optionally a CI workflow.
package initcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/initialize"
	"github.com/najemi-software/jobsworth/internal/repolink"
)

// Version is the running binary's version, set by main (like cmd/version).
var Version = "0.0.0"

// Schema declares what `jobsworth init [<dir>]` accepts (spec-init §2.1).
var Schema = cli.Schema{
	MinArgs:     0,
	MaxArgs:     1,
	ArgNames:    []string{"dir"},
	Description: "write a .jobsworth.yml for a repository, pre-filled from its workflows",
	Flags: []cli.Flag{
		{Name: "force", Description: "overwrite an existing .jobsworth.yml"},
		{Name: "minimal", Description: "write only keys with non-default values; no comments"},
		{Name: "full", Description: "write every key with its default, commented"},
		{Name: "probe", Description: "resolve every external reference with the current token and offer unfetchable ones as skip entries (network)"},
		{Name: "no-detect", Description: "skip repo inspection; write the template only"},
		{Name: "workflow", Description: "also write .github/workflows/jobsworth.yml"},
		{Name: "pre-commit", Description: "print a .pre-commit-config.yaml snippet to stdout; writes nothing"},
		{Name: "yes", Description: "non-interactive: accept every detected value (implied when stdin is not a terminal)"},
		{Name: "print", Description: "write the config to stdout instead of a file; implies --yes"},
	},
}

// Env is what Run reads from the process; tests inject it.
type Env struct {
	Cwd    string
	Home   string
	Getenv func(string) string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// StdinIsTerminal enables prompts; false implies --yes.
	StdinIsTerminal bool
	// GHToken is the `gh auth token` source for --probe; nil disables it.
	GHToken func() (string, bool)
}

// Run is the command entrypoint. Ctrl-C exits 1 with nothing written: the
// config is only ever written atomically at the end (§7).
func Run(args []string, flags map[string][]string) error {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		if _, ok := <-interrupts; ok {
			fmt.Fprintln(os.Stderr, "\ninterrupted; nothing written")
			os.Exit(cli.ExitFindings)
		}
	}()
	return RunWith(args, flags, Env{
		Cwd: cwd, Home: home, Getenv: os.Getenv,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdinIsTerminal: isTerminal(os.Stdin), GHToken: fetch.GHCLIToken,
	})
}

// ErrDeclined is returned when the user declined at a prompt (exit 1).
var ErrDeclined = errors.New("nothing written")

// RunWith is Run with an explicit environment.
func RunWith(args []string, flags map[string][]string, env Env) error {
	has := func(name string) bool { _, ok := flags[name]; return ok }
	if has("minimal") && has("full") {
		return cli.Exit(cli.ExitUsage, errors.New("--minimal and --full are mutually exclusive"))
	}
	mode := initialize.ModeDefault
	if has("minimal") {
		mode = initialize.ModeMinimal
	} else if has("full") {
		mode = initialize.ModeFull
	}
	print := has("print")
	yes := has("yes") || print || !env.StdinIsTerminal

	// --pre-commit only prints (§6).
	if has("pre-commit") {
		fmt.Fprint(env.Stdout, initialize.RenderPreCommit(Version))
		return nil
	}

	// Target (§2).
	dir := env.Cwd
	if len(args) == 1 {
		dir = expandTilde(args[0], env.Home)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(env.Cwd, dir)
		}
	}
	root, err := initialize.TargetRoot(dir, repolink.GitRoot)
	if err != nil {
		return cli.Exit(cli.ExitUsage, err)
	}
	cfgPath := filepath.Join(root, config.FileName)
	wfPath := filepath.Join(root, ".github", "workflows", "jobsworth.yml")
	if !print {
		if _, err := os.Stat(cfgPath); err == nil && !has("force") {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("%s exists; use --force to overwrite", cfgPath))
		}
		if has("workflow") {
			if _, err := os.Stat(wfPath); err == nil && !has("force") {
				return cli.Exit(cli.ExitUsage, fmt.Errorf("%s exists; use --force to overwrite", wfPath))
			}
		}
	}

	ask := newAsker(env, yes)

	// Detection (§3).
	var det initialize.Detected
	var privateRepos []string
	if !has("no-detect") {
		fmt.Fprintf(env.Stderr, "Inspecting %s … ", root)
		in := initialize.Inspect(root)
		fmt.Fprintln(env.Stderr, initialize.Summary(in))
		for _, e := range in.ParseErrors {
			fmt.Fprintf(env.Stderr, "skipped %s\n", e)
		}

		// §3.2 links.
		lookup := initialize.LookupEnv{Home: env.Home, ReposPath: env.Getenv("JOBSWORTH_REPOS")}
		for _, r := range initialize.SortedRepos(in) {
			co, found := initialize.FindCheckout(root, r.Slug, lookup)
			if !found {
				if initialize.IsWellKnownOwner(r.Slug) {
					continue // actions/*, github/*: nobody checks these out next to their repo
				}
				fmt.Fprintf(env.Stderr, "\n%s is referenced in %s. No local checkout found.\n", r.Slug, plural(r.Files, "file"))
				fmt.Fprintln(env.Stderr, "(Add it later under repos:, or clone it next to this repo and re-run init.)")
				det.Unlinked = append(det.Unlinked, r)
				continue
			}
			origin := "origin matches"
			if !co.OriginVerified {
				origin = "origin not verified"
			}
			fmt.Fprintf(env.Stderr, "\n%s is referenced in %s.\nFound a checkout at %s (%s).", r.Slug, plural(r.Files, "file"), co.Display, origin)
			ok, err := ask(" Link it?", true)
			if err != nil {
				return err
			}
			if ok {
				det.Links = append(det.Links, co)
			} else {
				det.Unlinked = append(det.Unlinked, r)
			}
		}
		det.NonBashSteps = in.NonBashSteps

		// §3.3 probe.
		if has("probe") {
			client := probeClient(env)
			results, abort := initialize.Probe(in, client, det.Links)
			if abort != nil {
				fmt.Fprintf(env.Stderr, "\n%v; writing config without skip entries\n", abort)
			} else if len(results) > 0 {
				fmt.Fprintf(env.Stderr, "\n%s could not be fetched with the current token:\n", plural(len(results), "external reference"))
				var offered []initialize.ProbeResult
				notFound := 0
				for _, r := range results {
					note := ""
					if r.Linked {
						note = "  [linked locally]"
					}
					fmt.Fprintf(env.Stderr, "  %d  %-45s (used in %s)%s\n", r.Status, r.Base+"@"+r.Ref, plural(r.Files, "file"), note)
					if r.Status == 404 {
						notFound++
						continue
					}
					if r.Linked {
						// Fetchable locally through the link, but CI's
						// GITHUB_TOKEN will not reach it: name it in the
						// workflow comment instead of the skip list.
						if slug := ownerRepo(r.Skip); !contains(privateRepos, slug) {
							privateRepos = append(privateRepos, slug)
						}
						continue
					}
					offered = append(offered, r)
				}
				if len(privateRepos) > 0 {
					fmt.Fprintf(env.Stderr, "Linked repos that need more than GITHUB_TOKEN in CI: %s\n", strings.Join(privateRepos, ", "))
				}
				if len(offered) > 0 {
					ok, err := ask("Add the 401/403s to skip?", true)
					if err != nil {
						return err
					}
					if ok {
						seen := map[string]bool{}
						for _, r := range offered {
							if seen[r.Skip] {
								continue
							}
							seen[r.Skip] = true
							if r.IsWorkflow {
								det.SkipWorkflows = append(det.SkipWorkflows, r.Skip)
							} else {
								det.SkipActions = append(det.SkipActions, r.Skip)
							}
						}
					}
				}
				if notFound == 1 {
					fmt.Fprintln(env.Stderr, "The 404 is probably a bug; it will not be added.")
				} else if notFound > 1 {
					fmt.Fprintf(env.Stderr, "The %d 404s are probably bugs; they will not be added.\n", notFound)
				}
			} else {
				fmt.Fprintln(env.Stderr, "\nEvery external reference is fetchable with the current token.")
			}
		}
	}

	content := initialize.Render(det, mode)
	workflow := ""
	if has("workflow") {
		// The tool's own action is pinned to the commit its tag points at,
		// as uses-unpinned asks of everyone else (test review 5 D4). The
		// lookup is network, so it rides --probe; without it the tag stays,
		// with a note.
		sha := ""
		if has("probe") {
			sha = resolveActionSHA(env)
		}
		if sha == "" {
			fmt.Fprintf(env.Stderr, "note: the workflow pins %s/jobsworth-action@%s by tag; run init with --probe to pin the commit SHA, or replace it by hand before merging\n", initialize.ActionOwner, initialize.ActionRef(Version))
		}
		workflow = initialize.RenderWorkflowPinned(Version, privateRepos, sha)
	}
	if print {
		fmt.Fprint(env.Stdout, content)
		if has("workflow") {
			fmt.Fprintf(env.Stdout, "\n# --- %s ---\n", filepath.ToSlash(filepath.Join(".github", "workflows", "jobsworth.yml")))
			fmt.Fprint(env.Stdout, workflow)
		}
		return nil
	}

	// Final confirmation only when overwriting interactively (§7, §8);
	// --yes (or a non-terminal stdin) accepts it.
	if has("force") && !yes {
		if _, err := os.Stat(cfgPath); err == nil {
			ok, err := ask(fmt.Sprintf("%s exists and will be regenerated. Hand edits will be lost. Continue?", config.FileName), false)
			if err != nil {
				return err
			}
			if !ok {
				return cli.Exit(cli.ExitFindings, ErrDeclined)
			}
		}
	}
	if err := initialize.WriteFile(cfgPath, []byte(content), has("force")); err != nil {
		return cli.Exit(cli.ExitUsage, err)
	}
	fmt.Fprintf(env.Stderr, "\nWrote %s\n", rel(env.Cwd, cfgPath))
	if has("workflow") {
		if err := initialize.WriteFile(wfPath, []byte(workflow), has("force")); err != nil {
			return cli.Exit(cli.ExitUsage, err)
		}
		fmt.Fprintf(env.Stderr, "Wrote %s\n", rel(env.Cwd, wfPath))
	}
	fmt.Fprintln(env.Stderr, "Next: jobsworth check")
	return nil
}

// asker returns the answer to a yes/no question. With yes=true every
// question gets its default without reading stdin. EOF (Ctrl-C/D) is
// ErrDeclined with exit 1.
type asker func(question string, def bool) (bool, error)

func newAsker(env Env, yes bool) asker {
	reader := bufio.NewReader(env.Stdin)
	return func(question string, def bool) (bool, error) {
		hint := "[Y/n]"
		if !def {
			hint = "[y/N]"
		}
		fmt.Fprintf(env.Stderr, "%s %s ", question, hint)
		if yes {
			fmt.Fprintln(env.Stderr, map[bool]string{true: "y", false: "n"}[def])
			return def, nil
		}
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(env.Stderr)
			return false, cli.Exit(cli.ExitFindings, ErrDeclined)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
}

// resolveActionSHA resolves the tool action's tag to a commit SHA through
// the API, "" when that is not possible (offline, no network, no token).
func resolveActionSHA(env Env) string {
	c := probeClient(env)
	r, err := fetch.ResolveRef(c, initialize.ActionOwner, "jobsworth-action", initialize.ActionRef(Version))
	if err != nil {
		return ""
	}
	return r.SHA
}

func probeClient(env Env) *fetch.Client {
	root, _ := cache.DefaultRoot("", env.Getenv)
	token, _ := fetch.ResolveToken(env.Getenv, env.GHToken)
	c := fetch.New(env.Getenv("JOBSWORTH_API_URL"), token, cache.New(root))
	return c
}

func ownerRepo(slug string) string {
	parts := strings.SplitN(slug, "/", 3)
	if len(parts) < 2 {
		return slug
	}
	return parts[0] + "/" + parts[1]
}

func isLinked(links []initialize.Checkout, slug string) bool {
	for _, l := range links {
		if strings.EqualFold(l.Slug, slug) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func rel(cwd, p string) string {
	if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

func expandTilde(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func isTerminal(f *os.File) bool { return cli.IsTerminal(f) }
