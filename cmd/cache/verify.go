package cachecmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
)

var verifySchema = cli.Schema{
	Description: "read every entry and report corrupt, unparseable, or unexpected ones",
	Flags: []cli.Flag{
		cacheDirFlag,
		{Name: "fix", Description: "remove corrupt and unexpected entries; unparseable entries are kept"},
		formatFlag,
	},
}

func runVerify(flags map[string][]string, env Env) error {
	c, err := open(flags, env)
	if err != nil {
		return err
	}
	res, err := cache.Verify(c)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, p := range res.Problems {
		counts[p.Kind]++
	}

	fixed := 0
	if has(flags, "fix") && counts["corrupt"]+counts["unexpected"] > 0 {
		lock, err := cache.AcquireLock(c, LockWait)
		if err != nil {
			if errors.Is(err, cache.ErrLocked) {
				return cli.Exit(cli.ExitUsage, err)
			}
			return cli.Exit(cli.ExitUsage, fmt.Errorf("cannot lock cache: %w", err))
		}
		var rms []cache.Removal
		for _, p := range res.Problems {
			if p.Kind == "corrupt" || p.Kind == "unexpected" {
				rms = append(rms, cache.Removal{Label: p.Path, Files: []string{p.File}})
				fixed++
			}
		}
		err = cache.Remove(c, rms)
		cache.Unlock(lock)
		if err != nil {
			return cli.Exit(cli.ExitUsage, fmt.Errorf("fixing: %w", err))
		}
	}

	if first(flags, "format") == "json" {
		out := struct {
			Refs     int                   `json:"refs"`
			Files    int                   `json:"files"`
			Problems []cache.VerifyProblem `json:"problems"`
			Fixed    int                   `json:"fixed"`
		}{res.Refs, res.Files, res.Problems, fixed}
		if out.Problems == nil {
			out.Problems = []cache.VerifyProblem{}
		}
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(env.Stdout, "checked %s, %s\n", plural(res.Refs, "ref"), plural(res.Files, "file"))
		for _, kind := range []string{"corrupt", "unparseable", "unexpected"} {
			fmt.Fprintf(env.Stdout, "  %d %s:", counts[kind], kind)
			first := true
			for _, p := range res.Problems {
				if p.Kind != kind {
					continue
				}
				sep := "  "
				if !first {
					sep = ", "
				}
				first = false
				fmt.Fprintf(env.Stdout, "%s%s", sep, p.Path)
				if p.Reason != "" {
					fmt.Fprintf(env.Stdout, " (%s)", p.Reason)
				}
			}
			fmt.Fprintln(env.Stdout)
		}
		if fixed > 0 {
			fmt.Fprintf(env.Stdout, "removed %s\n", plural(fixed, "entry"))
		}
	}
	if len(res.Problems) > 0 && !has(flags, "fix") {
		return cli.Exit(cli.ExitFindings, nil)
	}
	return nil
}
