package cachecmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/uses"
)

var warmSchema = cli.Schema{
	MaxArgs: -1, ArgNames: []string{"path|glob|owner/repo@ref"},
	Description: "fetch everything reachable from the entries into the cache without validating",
	Flags: []cli.Flag{
		cacheDirFlag,
		{Name: "repo", TakesValue: true, Repeatable: true, ValueName: "owner/name=path", Description: "link an external repo to a local checkout, as check"},
		{Name: "config", TakesValue: true, ValueName: "path", Description: "config file, as check"},
		{Name: "refresh", Description: "re-resolve every tag and branch even inside TTL; unchanged SHAs reuse content"},
		{Name: "parallel", TakesValue: true, ValueName: "n", Description: "override fetch.parallel"},
		{Name: "quiet", Description: "summary line only"},
	},
}

// isRefArg reports whether a positional parses as an external uses:
// reference rather than a path (§6).
func isRefArg(arg string) bool {
	if strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "~") || check.IsGlob(arg) {
		return false
	}
	r, err := uses.Parse(arg)
	return err == nil && r.Kind == uses.External
}

func runWarm(args []string, flags map[string][]string, env Env) error {
	var paths, refs []string
	for _, a := range args {
		if isRefArg(a) {
			refs = append(refs, a)
		} else {
			paths = append(paths, a)
		}
	}
	sessFlags := map[string][]string{}
	for _, k := range []string{"repo", "config", "cache-dir"} {
		if v, ok := flags[k]; ok {
			sessFlags[k] = v
		}
	}
	sess, err := checkcmd.NewSession(paths, sessFlags, checkEnv(env), len(refs) == 0 || len(paths) > 0)
	if err != nil {
		return err
	}
	sess.Store.Now = env.Now
	sess.Store.Refresh = has(flags, "refresh")
	if v := first(flags, "parallel"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("--parallel %q is not a positive integer", v))
		}
		fetch.SetConcurrency(sess.Fetcher, n)
	}
	before, _ := cache.Scan(sess.Store)
	var sizeBefore int64
	for _, c := range before.Content {
		sizeBefore += c.Size
	}

	var problems []finding.Finding
	for i, e := range sess.Entries {
		r := check.RunEntry(e, check.Options{Links: sess.Links, Fetcher: sess.Fetcher, Config: sess.Configs[sess.Roots[i]], LoadOnly: true})
		problems = append(problems, check.AllFindings(r)...)
	}
	for _, raw := range refs {
		r := check.WarmRef(raw, check.Options{Links: sess.Links, Fetcher: sess.Fetcher, Config: sess.Base})
		problems = append(problems, check.AllFindings(r)...)
	}
	checkcmd.WriteRunStats(sess.Store, sess.Fetcher, "warm")

	st := &sess.Store.Stats
	refsSeen := st.RefHits.Load() + st.RefMisses.Load() + st.Stale.Load()
	if !has(flags, "quiet") {
		same := ""
		if n := st.SameSHA.Load(); n > 0 {
			same = fmt.Sprintf(" (%d re-resolved, same SHA)", n)
		}
		fmt.Fprintf(env.Stderr, "resolving %s … %d cached, %d fetched%s\n", plural(int(refsSeen), "ref"), st.RefHits.Load(), st.RefMisses.Load()+st.Stale.Load(), same)
		fmt.Fprintf(env.Stderr, "fetching %s … done\n", plural(int(st.ContentMisses.Load()), "file"))
		for _, p := range problems {
			fmt.Fprintf(env.Stderr, "  %s: %s\n", p.Rule, p.Message)
		}
	}
	after, _ := cache.Scan(sess.Store)
	var sizeAfter int64
	for _, c := range after.Content {
		sizeAfter += c.Size
	}
	fmt.Fprintf(env.Stdout, "warm: %s, %s, %s in cache (added %s)\n",
		plural(len(after.Refs), "ref"), plural(len(after.Content), "file"), humanSize(sizeAfter), humanSize(sizeAfter-sizeBefore))

	fs := &sess.Fetcher.Stats
	if fs.Successes.Load() == 0 && fs.AuthFailures.Load() > 0 {
		return cli.Exit(cli.ExitFindings, errors.New("warm: nothing could be fetched (authentication or rate limit)"))
	}
	return nil
}
