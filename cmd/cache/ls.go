package cachecmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
)

var lsSchema = cli.Schema{
	MaxArgs: 1, ArgNames: []string{"owner[/repo]"},
	Description: "list cache contents, most recently used first",
	Flags: []cli.Flag{
		cacheDirFlag,
		{Name: "expired", Description: "only refs past their TTL"},
		{Name: "kind", TakesValue: true, ValueName: "kind", AllowedValues: []string{"tag", "branch", "sha"}, Description: "only that kind"},
		{Name: "files", Description: "one line per cached file under each ref"},
		formatFlag,
	},
}

// lsRow is one ls line; also the JSON object shape (§2.2).
type lsRow struct {
	TTLSec  *int64   `json:"ttl_seconds"` // nil for sha kinds
	Ref     string   `json:"ref"`
	Kind    string   `json:"kind"`
	SHA     string   `json:"sha"`
	Paths   []string `json:"paths,omitempty"`
	AgeSec  int64    `json:"age_seconds"`
	Size    int64    `json:"size_bytes"`
	Files   int      `json:"files"`
	Expired bool     `json:"expired"`
	// Unreferenced marks content with no ref entry.
	Unreferenced bool `json:"unreferenced"`
}

// lsRows builds the rows for a listing at now, applying filters.
func lsRows(l cache.Listing, now time.Time, expiredOnly bool, kind string, withFiles bool) (live, unreferenced []lsRow) {
	bySHA := cache.ContentBySHA(l)
	sizeOf := func(cs []cache.ContentEntry) (int64, int, []string) {
		var size int64
		var paths []string
		for _, c := range cs {
			size += c.Size
			if withFiles {
				paths = append(paths, c.Path+"  "+humanSize(c.Size))
			}
		}
		return size, len(cs), paths
	}
	for _, r := range l.Refs {
		if r.Corrupt {
			continue
		}
		expired := cache.Expired(r, now)
		if expiredOnly && !expired {
			continue
		}
		if kind != "" && kind != r.Kind.String() {
			continue
		}
		key := strings.ToLower(r.Owner+"/"+r.Repo) + "@" + r.SHA
		size, files, paths := sizeOf(bySHA[key])
		ttl := int64(r.Expires.Sub(now).Seconds())
		if ttl < 0 {
			ttl = 0
		}
		live = append(live, lsRow{
			Ref: r.Owner + "/" + r.Repo + "@" + r.Ref, Kind: r.Kind.String(), SHA: short(r.SHA),
			AgeSec: int64(now.Sub(r.Written).Seconds()), TTLSec: &ttl, Expired: expired,
			Size: size, Files: files, Paths: paths,
		})
	}
	if expiredOnly || (kind != "" && kind != "sha") {
		return live, nil
	}
	for _, key := range cache.Unreferenced(l) {
		cs := bySHA[key]
		size, files, paths := sizeOf(cs)
		newest := time.Time{}
		for _, c := range cs {
			if c.Written.After(newest) {
				newest = c.Written
			}
		}
		at := strings.LastIndexByte(key, '@')
		unreferenced = append(unreferenced, lsRow{
			Ref: cs[0].Owner + "/" + cs[0].Repo + "@" + short(key[at+1:]) + "…", Kind: "sha", SHA: short(key[at+1:]),
			AgeSec: int64(now.Sub(newest).Seconds()), Size: size, Files: files, Unreferenced: true, Paths: paths,
		})
	}
	sort.SliceStable(unreferenced, func(i, j int) bool { return unreferenced[i].AgeSec < unreferenced[j].AgeSec })
	return live, unreferenced
}

func runLs(args []string, flags map[string][]string, env Env) error {
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
	live, unref := lsRows(l, now, has(flags, "expired"), first(flags, "kind"), has(flags, "files"))

	if first(flags, "format") == "json" {
		all := append([]lsRow{}, live...)
		all = append(all, unref...)
		if all == nil {
			all = []lsRow{}
		}
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	}

	if !l.Exists {
		if len(l.OtherSchemas) > 0 {
			fmt.Fprintf(env.Stdout, "no %s cache at %s; found %s (older binary)\n", cache.SchemaVersion, c.Root, strings.Join(l.OtherSchemas, ", "))
		} else {
			fmt.Fprintf(env.Stdout, "empty cache at %s\n", c.Root)
		}
		return nil
	}
	rows := [][]string{{"REF", "KIND", "SHA", "AGE", "TTL", "SIZE", "FILES"}}
	var totalFiles int
	var totalSize int64
	add := func(r lsRow) {
		ttl := "∞"
		if r.TTLSec != nil {
			if r.Expired {
				ttl = "expired"
			} else {
				ttl = humanAge(time.Duration(*r.TTLSec) * time.Second)
			}
		}
		rows = append(rows, []string{r.Ref, r.Kind, r.SHA, humanAge(time.Duration(r.AgeSec) * time.Second), ttl, humanSize(r.Size), fmt.Sprint(r.Files)})
		for _, p := range r.Paths {
			rows = append(rows, []string{"  " + p, "", "", "", "", "", ""})
		}
		totalFiles += r.Files
		totalSize += r.Size
	}
	for _, r := range live {
		add(r)
	}
	if len(unref) > 0 {
		rows = append(rows, []string{"(unreferenced)", "", "", "", "", "", ""})
		for _, r := range unref {
			add(r)
		}
	}
	table(env.Stdout, rows)
	summary := fmt.Sprintf("%s, %s, %s", plural(len(live), "ref"), plural(totalFiles, "file"), humanSize(totalSize))
	if len(unref) > 0 {
		summary = fmt.Sprintf("%s, %s unreferenced, %s, %s", plural(len(live), "ref"), plural(len(unref), "SHA"), plural(totalFiles, "file"), humanSize(totalSize))
	}
	fmt.Fprintf(env.Stdout, "\n%s\n", summary)
	return nil
}
