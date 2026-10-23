package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RefEntry is one refs/ file as seen by ls/clear/verify (spec-cache §2.2).
type RefEntry struct {
	Owner, Repo string
	Kind        RefKind
	Ref         string
	SHA         string
	// Written is the entry's mtime; Expires is Written + TTL (zero for SHA
	// kinds, which never expire).
	Written time.Time
	Expires time.Time
	Path    string
	// Corrupt is set by Verify when the SHA is not 40 hex.
	Corrupt bool
}

// Expired reports whether the ref is past its TTL at now.
func Expired(e RefEntry, now time.Time) bool {
	return !e.Expires.IsZero() && now.After(e.Expires)
}

// ContentEntry is one content/ file.
type ContentEntry struct {
	Owner, Repo, SHA, Path string
	Size                   int64
	Written                time.Time
	FilePath               string
}

// Listing is the full cache contents.
type Listing struct {
	Refs    []RefEntry
	Content []ContentEntry
	// Unexpected are files outside the schema (stray files, wrong depth),
	// relative to the schema directory.
	Unexpected []string
	// OtherSchemas lists schema directories other than the current one.
	OtherSchemas []string
	// Exists is false when the root or schema directory is absent.
	Exists bool
}

// Scan walks the schema directory (spec-cache §2.2). It never reads content
// bytes, so it is cheap on large caches.
func Scan(c *Cache) (Listing, error) {
	var l Listing
	if c == nil || c.Root == "" {
		return l, nil
	}
	if entries, err := os.ReadDir(c.Root); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != SchemaVersion && strings.HasPrefix(e.Name(), "v") {
				l.OtherSchemas = append(l.OtherSchemas, e.Name())
			}
		}
	}
	base := filepath.Join(c.Root, SchemaVersion)
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		return l, nil
	}
	l.Exists = true
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		rel = filepath.ToSlash(rel)
		if rel == StatsFile || rel == LockFile {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, "/")
		switch {
		case parts[0] == "refs" && len(parts) >= 5 && (parts[3] == "tags" || parts[3] == "branches"):
			kind := Tag
			if parts[3] == "branches" {
				kind = Branch
			}
			e := RefEntry{Owner: parts[1], Repo: parts[2], Kind: kind, Ref: strings.Join(parts[4:], "/"), Written: info.ModTime(), Path: p}
			if data, err := os.ReadFile(p); err == nil {
				e.SHA = strings.TrimSpace(string(data))
			}
			e.Corrupt = !IsSHA(e.SHA)
			e.Expires = e.Written.Add(ttlFor(c, kind))
			l.Refs = append(l.Refs, e)
		case parts[0] == "content" && len(parts) >= 5 && IsSHA(parts[3]):
			if strings.HasSuffix(rel, SidecarSuffix) {
				return nil
			}
			l.Content = append(l.Content, ContentEntry{
				Owner: parts[1], Repo: parts[2], SHA: parts[3], Path: strings.Join(parts[4:], "/"),
				Size: info.Size(), Written: info.ModTime(), FilePath: p,
			})
		default:
			l.Unexpected = append(l.Unexpected, rel)
		}
		return nil
	})
	if err != nil {
		return l, err
	}
	sort.Slice(l.Refs, func(i, j int) bool { return l.Refs[i].Written.After(l.Refs[j].Written) })
	sort.Slice(l.Content, func(i, j int) bool { return l.Content[i].FilePath < l.Content[j].FilePath })
	return l, nil
}

// Filter narrows a listing to an owner or owner/repo (case-insensitive).
// An empty selector keeps everything.
func Filter(l Listing, selector string) Listing {
	if selector == "" {
		return l
	}
	sel := strings.ToLower(strings.TrimSuffix(selector, "/"))
	match := func(owner, repo string) bool {
		slug := strings.ToLower(owner + "/" + repo)
		return slug == sel || strings.ToLower(owner) == sel
	}
	out := Listing{Exists: l.Exists, Unexpected: l.Unexpected, OtherSchemas: l.OtherSchemas}
	for _, r := range l.Refs {
		if match(r.Owner, r.Repo) {
			out.Refs = append(out.Refs, r)
		}
	}
	for _, c := range l.Content {
		if match(c.Owner, c.Repo) {
			out.Content = append(out.Content, c)
		}
	}
	return out
}

// ContentBySHA groups content by owner/repo@sha (lower-cased key).
func ContentBySHA(l Listing) map[string][]ContentEntry {
	m := map[string][]ContentEntry{}
	for _, c := range l.Content {
		m[shaKey(c.Owner, c.Repo, c.SHA)] = append(m[shaKey(c.Owner, c.Repo, c.SHA)], c)
	}
	return m
}

func shaKey(owner, repo, sha string) string {
	return strings.ToLower(owner+"/"+repo) + "@" + sha
}

// Unreferenced returns the SHA keys that have content but no ref entry
// pointing at them (live or expired).
func Unreferenced(l Listing) []string {
	referenced := map[string]bool{}
	for _, r := range l.Refs {
		referenced[shaKey(r.Owner, r.Repo, r.SHA)] = true
	}
	var out []string
	for key := range ContentBySHA(l) {
		if !referenced[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// Removal is one planned or executed removal.
type Removal struct {
	// Label is "owner/repo@ref" for a ref, "owner/repo@sha" for content.
	Label string
	IsRef bool
	Files []string
	Size  int64
}

// RemovalTotals sums a plan: ref entries removed, content files removed,
// and content bytes.
func RemovalTotals(rs []Removal) (refs, files int, size int64) {
	for _, r := range rs {
		if r.IsRef {
			refs++
			continue
		}
		files += len(r.Files)
		size += r.Size
	}
	return refs, files, size
}

// Remove deletes the planned files one by one (never a directory tree, so
// a concurrent reader sees a hit or a miss, never a partial directory) and
// prunes directories left empty. The schema directory itself is kept.
func Remove(c *Cache, plan []Removal) error {
	base := filepath.Join(c.Root, SchemaVersion)
	var firstErr error
	for _, r := range plan {
		for _, f := range r.Files {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
			_ = os.Remove(f + SidecarSuffix)
			pruneEmpty(filepath.Dir(f), base)
		}
	}
	return firstErr
}

func pruneEmpty(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// VerifyProblem is one finding of Verify.
type VerifyProblem struct {
	// Kind is "corrupt", "unparseable", or "unexpected".
	Kind   string
	Path   string // relative to the schema directory
	Reason string
	// File is the absolute path, for --fix.
	File string
}

// VerifyResult summarises a verify pass.
type VerifyResult struct {
	Refs, Files int
	Problems    []VerifyProblem
}

// Verify reads every entry and checks it (spec-cache §2.5).
func Verify(c *Cache) (VerifyResult, error) {
	l, err := Scan(c)
	if err != nil {
		return VerifyResult{}, err
	}
	base := filepath.Join(c.Root, SchemaVersion)
	var res VerifyResult
	rel := func(p string) string {
		r, _ := filepath.Rel(base, p)
		return filepath.ToSlash(r)
	}
	for _, r := range l.Refs {
		res.Refs++
		if r.Corrupt {
			res.Problems = append(res.Problems, VerifyProblem{Kind: "corrupt", Path: rel(r.Path), Reason: "not a 40-hex SHA", File: r.Path})
		}
	}
	for _, ce := range l.Content {
		res.Files++
		data, err := os.ReadFile(ce.FilePath)
		if err != nil || len(data) == 0 {
			res.Problems = append(res.Problems, VerifyProblem{Kind: "corrupt", Path: rel(ce.FilePath), Reason: "empty", File: ce.FilePath})
			continue
		}
		side, err := os.ReadFile(ce.FilePath + SidecarSuffix)
		if err != nil {
			res.Problems = append(res.Problems, VerifyProblem{Kind: "corrupt", Path: rel(ce.FilePath), Reason: "missing sha256 sidecar", File: ce.FilePath})
			continue
		}
		if strings.TrimSpace(string(side)) != SHA256Hex(data) {
			res.Problems = append(res.Problems, VerifyProblem{Kind: "corrupt", Path: rel(ce.FilePath), Reason: "sha256 mismatch", File: ce.FilePath})
			continue
		}
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			res.Problems = append(res.Problems, VerifyProblem{Kind: "unparseable", Path: rel(ce.FilePath), Reason: "yaml: " + firstLine(err.Error()), File: ce.FilePath})
		}
	}
	for _, u := range l.Unexpected {
		res.Problems = append(res.Problems, VerifyProblem{Kind: "unexpected", Path: u, File: filepath.Join(base, filepath.FromSlash(u))})
	}
	sort.SliceStable(res.Problems, func(i, j int) bool { return res.Problems[i].Path < res.Problems[j].Path })
	return res, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
