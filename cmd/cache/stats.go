package cachecmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
)

var statsSchema = cli.Schema{
	Description: "aggregate cache numbers and the last run's hit/miss counts",
	Flags:       []cli.Flag{cacheDirFlag, formatFlag},
}

type statsDoc struct {
	Root         string          `json:"root"`
	Schema       int             `json:"schema"`
	Refs         int             `json:"refs"`
	Tags         int             `json:"tags"`
	Branches     int             `json:"branches"`
	Expired      int             `json:"expired"`
	Files        int             `json:"files"`
	SizeBytes    int64           `json:"size_bytes"`
	SHAs         int             `json:"shas"`
	Unreferenced int             `json:"unreferenced_shas"`
	UnrefBytes   int64           `json:"unreferenced_bytes"`
	OldestSec    *int64          `json:"oldest_seconds"`
	NewestSec    *int64          `json:"newest_seconds"`
	OtherSchemas []string        `json:"other_schemas"`
	LastRun      *cache.RunStats `json:"last_run"`
}

func runStats(flags map[string][]string, env Env) error {
	c, err := open(flags, env)
	if err != nil {
		return err
	}
	l, err := cache.Scan(c)
	if err != nil {
		return err
	}
	now := env.Now()
	d := statsDoc{Root: filepath.Join(c.Root, cache.SchemaVersion), Schema: 1, OtherSchemas: l.OtherSchemas}
	if d.OtherSchemas == nil {
		d.OtherSchemas = []string{}
	}
	var oldest, newest time.Time
	touch := func(t time.Time) {
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
		if t.After(newest) {
			newest = t
		}
	}
	for _, r := range l.Refs {
		d.Refs++
		if r.Kind == cache.Tag {
			d.Tags++
		} else {
			d.Branches++
		}
		if cache.Expired(r, now) {
			d.Expired++
		}
		touch(r.Written)
	}
	bySHA := cache.ContentBySHA(l)
	d.SHAs = len(bySHA)
	for _, ce := range l.Content {
		d.Files++
		d.SizeBytes += ce.Size
		touch(ce.Written)
	}
	unref := cache.Unreferenced(l)
	d.Unreferenced = len(unref)
	for _, key := range unref {
		for _, ce := range bySHA[key] {
			d.UnrefBytes += ce.Size
		}
	}
	if !oldest.IsZero() {
		o, n := int64(now.Sub(oldest).Seconds()), int64(now.Sub(newest).Seconds())
		d.OldestSec, d.NewestSec = &o, &n
	}
	if rs, ok := cache.ReadRunStats(c); ok {
		d.LastRun = &rs
	}

	if first(flags, "format") == "json" {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}
	if !l.Exists && len(l.OtherSchemas) > 0 {
		fmt.Fprintf(env.Stdout, "no %s cache at %s; found %s (older binary)\n", cache.SchemaVersion, c.Root, strings.Join(l.OtherSchemas, ", "))
		return nil
	}
	fmt.Fprintf(env.Stdout, "root         %s\n", d.Root)
	fmt.Fprintf(env.Stdout, "schema       %d\n", d.Schema)
	fmt.Fprintf(env.Stdout, "refs         %d   (%s, %s; %d expired)\n", d.Refs, plural(d.Tags, "tag"), plural(d.Branches, "branch"), d.Expired)
	fmt.Fprintf(env.Stdout, "content      %s, %s across %s\n", plural(d.Files, "file"), humanSize(d.SizeBytes), plural(d.SHAs, "SHA"))
	fmt.Fprintf(env.Stdout, "unreferenced %s, %s\n", plural(d.Unreferenced, "SHA"), humanSize(d.UnrefBytes))
	if d.OldestSec != nil {
		fmt.Fprintf(env.Stdout, "oldest       %s   newest  %s\n", humanAge(time.Duration(*d.OldestSec)*time.Second), humanAge(time.Duration(*d.NewestSec)*time.Second))
	}
	if d.LastRun != nil {
		fmt.Fprintf(env.Stdout, "last run     hits %d / misses %d / stale %d / api calls %d     (from the most recent %s)\n",
			d.LastRun.Hits, d.LastRun.Misses, d.LastRun.Stale, d.LastRun.APICalls, d.LastRun.Command)
	}
	return nil
}
