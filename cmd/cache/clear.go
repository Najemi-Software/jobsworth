package cachecmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
)

var clearSchema = cli.Schema{
	MaxArgs: 1, ArgNames: []string{"owner[/repo]"},
	Description: "remove cache entries; everything by default",
	Flags: []cli.Flag{
		cacheDirFlag,
		{Name: "refs", Description: "only the refs/ partition (tags and branches re-resolve next use; content stays)"},
		{Name: "tags", Description: "only tag refs"},
		{Name: "branches", Description: "only branch refs"},
		{Name: "content", Description: "only the content/ partition; refs stay"},
		{Name: "expired", Description: "only refs past TTL and content no live ref points at"},
		{Name: "older-than", TakesValue: true, ValueName: "duration", Description: "refs and unreferenced content older than the duration, e.g. 30d"},
		{Name: "dry-run", Description: "print what would be removed; remove nothing"},
		{Name: "yes", Description: "skip the confirmation (implied when stdin is not a terminal)"},
		{Name: "all-schemas", Description: "also remove cache directories of other schema versions"},
	},
}

// LockWait is how long clear waits for another process's lock.
var LockWait = 5 * time.Second

// parseDuration accepts Go durations plus a "d" suffix for days.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		var n float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%g", &n); err == nil {
			return time.Duration(n * float64(24*time.Hour)), nil
		}
	}
	return time.ParseDuration(s)
}

// plan selects what clear removes (§2.3).
func plan(l cache.Listing, flags map[string][]string, now time.Time) ([]cache.Removal, error) {
	wantTags := has(flags, "refs") || has(flags, "tags")
	wantBranches := has(flags, "refs") || has(flags, "branches")
	wantContent := has(flags, "content")
	if !wantTags && !wantBranches && !wantContent {
		wantTags, wantBranches, wantContent = true, true, true
	}
	expired := has(flags, "expired")
	var olderThan time.Duration
	if v := first(flags, "older-than"); v != "" {
		d, err := parseDuration(v)
		if err != nil || d < 0 {
			return nil, cli.Exit(cli.ExitUsage, fmt.Errorf("--older-than %q is not a duration (e.g. 30d, 12h)", v))
		}
		olderThan = d
	}
	ageOK := func(written time.Time) bool {
		return olderThan == 0 || now.Sub(written) >= olderThan
	}

	// Refs first. A SHA stays "live" only while a non-expired ref that
	// survives this clear points at it; content under any other SHA is
	// unreferenced for --expired and --older-than.
	var out []cache.Removal
	liveSHA := map[string]bool{}
	for _, r := range l.Refs {
		isExpired := cache.Expired(r, now) || r.Corrupt
		removing := !((r.Kind == cache.Tag && !wantTags) || (r.Kind == cache.Branch && !wantBranches)) &&
			(!expired || isExpired) && ageOK(r.Written)
		if removing {
			out = append(out, cache.Removal{Label: r.Owner + "/" + r.Repo + "@" + r.Ref, IsRef: true, Files: []string{r.Path}})
		} else if !isExpired {
			liveSHA[strings.ToLower(r.Owner+"/"+r.Repo)+"@"+r.SHA] = true
		}
	}
	if wantContent {
		for key, cs := range cache.ContentBySHA(l) {
			if (expired || olderThan > 0) && liveSHA[key] {
				continue
			}
			rm := cache.Removal{Label: cs[0].Owner + "/" + cs[0].Repo + "@" + short(cs[0].SHA)}
			for _, c := range cs {
				if !ageOK(c.Written) {
					continue
				}
				rm.Files = append(rm.Files, c.FilePath)
				rm.Size += c.Size
			}
			if len(rm.Files) > 0 {
				out = append(out, rm)
			}
		}
	}
	return out, nil
}

func runClear(args []string, flags map[string][]string, env Env) error {
	c, err := open(flags, env)
	if err != nil {
		return err
	}
	l, err := cache.Scan(c)
	if err != nil {
		return err
	}
	if len(args) == 1 {
		l = cache.Filter(l, args[0])
	}
	now := env.Now()
	rms, err := plan(l, flags, now)
	if err != nil {
		return err
	}
	refs, files, size := cache.RemovalTotals(rms)
	otherSchemas := l.OtherSchemas
	if !has(flags, "all-schemas") {
		otherSchemas = nil
	}

	if has(flags, "dry-run") {
		for _, r := range rms {
			if r.IsRef {
				fmt.Fprintf(env.Stdout, "would remove %s\n", r.Label)
			}
		}
		for _, s := range otherSchemas {
			fmt.Fprintf(env.Stdout, "would remove schema directory %s\n", s)
		}
		fmt.Fprintf(env.Stdout, "would remove %s, %s, %s\n", plural(refs, "ref"), plural(files, "file"), humanSize(size))
		return nil
	}
	if len(rms) == 0 && len(otherSchemas) == 0 {
		fmt.Fprintf(env.Stdout, "removed 0 refs, 0 files, 0\n")
		return nil
	}
	question := fmt.Sprintf("Remove %s, %s, %s from %s?", plural(refs, "ref"), plural(files, "file"), humanSize(size), c.Root)
	if len(args) == 0 && !has(flags, "refs") && !has(flags, "tags") && !has(flags, "branches") && !has(flags, "content") && !has(flags, "expired") && first(flags, "older-than") == "" {
		question = fmt.Sprintf("Remove everything (%s, %s, %s) from %s?", plural(refs, "ref"), plural(files, "file"), humanSize(size), c.Root)
	}
	if !confirm(flags, env, question) {
		return cli.Exit(cli.ExitFindings, errors.New("nothing removed"))
	}

	lock, err := cache.AcquireLock(c, LockWait)
	if err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return cli.Exit(cli.ExitUsage, err)
		}
		return cli.Exit(cli.ExitUsage, fmt.Errorf("cannot lock cache: %w", err))
	}
	defer cache.Unlock(lock)

	if err := cache.Remove(c, rms); err != nil {
		return cli.Exit(cli.ExitUsage, fmt.Errorf("removing: %w", err))
	}
	for _, s := range otherSchemas {
		if err := os.RemoveAll(filepath.Join(c.Root, s)); err != nil {
			return cli.Exit(cli.ExitUsage, err)
		}
	}
	if refs <= 20 {
		for _, r := range rms {
			if r.IsRef {
				fmt.Fprintf(env.Stdout, "removed %s\n", r.Label)
			}
		}
	}
	for _, s := range otherSchemas {
		fmt.Fprintf(env.Stdout, "removed schema directory %s\n", s)
	}
	fmt.Fprintf(env.Stdout, "removed %s, %s, %s\n", plural(refs, "ref"), plural(files, "file"), humanSize(size))
	return nil
}
