// Package cache is the on-disk cache of ref resolutions and fetched content
// (spec §6). Layout under <root>/v1: refs/<owner>/<repo>/{tags,branches}/<ref>
// holding a 40-hex SHA, and content/<owner>/<repo>/<sha>/<path> holding raw
// bytes. Writes are atomic; reads validate and treat anything odd as a miss.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SchemaVersion is the layout version directory; bump to invalidate all.
const SchemaVersion = "v1"

// TTLs per §6.3. Content at a SHA never expires.
const (
	TagTTL    = 4 * time.Hour
	BranchTTL = 10 * time.Minute
)

// RefKind classifies a resolved ref for TTL purposes.
type RefKind int

const (
	// Tag: cached 4h.
	Tag RefKind = iota
	// Branch: cached 10m.
	Branch
	// Commit: a SHA given directly or abbreviated; never cached under refs/.
	Commit
)

func (k RefKind) String() string {
	switch k {
	case Tag:
		return "tag"
	case Branch:
		return "branch"
	case Commit:
		return "commit"
	}
	return "unknown"
}

// Cache is the handle. A nil *Cache or Disabled cache misses on every read
// and drops every write.
type Cache struct {
	Root     string
	Disabled bool
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// TagTTL/BranchTTL override the §6.3 defaults when non-zero.
	TagTTL    time.Duration
	BranchTTL time.Duration
	// Refresh treats every tag and branch entry as expired (spec-cache
	// §2.4 --refresh): reads miss, writes still happen.
	Refresh bool
	// Stats counts this run's cache activity (spec-cache §3.2).
	Stats Stats
}

// Stats are per-run counters, safe for concurrent use. Each distinct key
// (owner/repo@ref, or owner/repo@sha/path) is counted once, by the outcome
// of its first lookup, so a ref used by three workflows is one ref
// (spec-cache review N2).
type Stats struct {
	RefHits       atomic.Int64
	RefMisses     atomic.Int64
	ContentHits   atomic.Int64
	ContentMisses atomic.Int64
	// Stale counts ref reads that found an entry past its TTL (or under
	// Refresh).
	Stale atomic.Int64
	// SameSHA counts ref writes whose SHA equalled the previous entry.
	SameSHA atomic.Int64

	mu   sync.Mutex
	seen map[string]bool
}

// first reports whether key has not been counted yet, marking it.
func (s *Stats) first(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[key] {
		return false
	}
	s.seen[key] = true
	return true
}

// Distinct returns the number of distinct keys looked up.
func (s *Stats) Distinct() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

// Hits sums ref and content hits.
func (s *Stats) Hits() int64 { return s.RefHits.Load() + s.ContentHits.Load() }

// Misses sums ref and content misses (stale reads are counted separately).
func (s *Stats) Misses() int64 { return s.RefMisses.Load() + s.ContentMisses.Load() }

// New returns a cache rooted at root.
func New(root string) *Cache {
	return &Cache{Root: root}
}

// DefaultRoot resolves the cache root per §6.1: flag, $JOBSWORTH_CACHE_DIR,
// then the OS user cache dir (which honours $XDG_CACHE_HOME on Linux).
func DefaultRoot(flag string, getenv func(string) string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := getenv("JOBSWORTH_CACHE_DIR"); v != "" {
		return v, nil
	}
	if v := getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "jobsworth"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine cache dir: %w", err)
	}
	return filepath.Join(base, "jobsworth"), nil
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// IsSHA reports whether s is a full 40-hex commit SHA.
func IsSHA(s string) bool {
	return shaRe.MatchString(strings.ToLower(s)) && s == strings.ToLower(s)
}

func ttlFor(c *Cache, k RefKind) time.Duration {
	if k == Branch {
		if c.BranchTTL > 0 {
			return c.BranchTTL
		}
		return BranchTTL
	}
	if c.TagTTL > 0 {
		return c.TagTTL
	}
	return TagTTL
}

func active(c *Cache) bool {
	return c != nil && !c.Disabled && c.Root != ""
}

// Active reports whether the cache is in use (non-nil, enabled, rooted).
func Active(c *Cache) bool { return active(c) }

func now(c *Cache) time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// refPath builds refs/<owner>/<repo>/<kind>/<ref>; ok=false for refs that
// cannot be stored safely (path traversal, empty).
func refPath(c *Cache, owner, repo string, kind RefKind, ref string) (string, bool) {
	var dir string
	switch kind {
	case Tag:
		dir = "tags"
	case Branch:
		dir = "branches"
	default:
		return "", false
	}
	if !safeSegment(owner) || !safeSegment(repo) || !safePath(ref) {
		return "", false
	}
	return filepath.Join(c.Root, SchemaVersion, "refs", strings.ToLower(owner), strings.ToLower(repo), dir, filepath.FromSlash(ref)), true
}

func contentPath(c *Cache, owner, repo, sha, path string) (string, bool) {
	if !safeSegment(owner) || !safeSegment(repo) || !IsSHA(sha) || !safePath(path) {
		return "", false
	}
	return filepath.Join(c.Root, SchemaVersion, "content", strings.ToLower(owner), strings.ToLower(repo), sha, filepath.FromSlash(path)), true
}

func safeSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}

func safePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// GetRef returns the cached SHA for a tag or branch ref if present and
// within TTL (§6.3, §6.5). Both kinds are checked; the kind found is
// returned.
func GetRef(c *Cache, owner, repo, ref string) (sha string, kind RefKind, ok bool) {
	if !active(c) {
		return "", 0, false
	}
	count := c.Stats.first("ref:" + strings.ToLower(owner+"/"+repo) + "@" + ref)
	stale := false
	for _, k := range []RefKind{Tag, Branch} {
		p, ok := refPath(c, owner, repo, k, ref)
		if !ok {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		ttl := ttlFor(c, k)
		if c.Refresh || now(c).Sub(st.ModTime()) > ttl {
			stale = true
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		if !IsSHA(s) {
			continue
		}
		if count {
			c.Stats.RefHits.Add(1)
		}
		return s, k, true
	}
	if count {
		if stale {
			c.Stats.Stale.Add(1)
		} else {
			c.Stats.RefMisses.Add(1)
		}
	}
	return "", 0, false
}

// PeekRef returns a stored tag or branch entry regardless of TTL or
// Refresh, so a re-resolution that yields the same SHA can reuse the kind
// without a second API call (spec-cache §2.4). It counts nothing.
func PeekRef(c *Cache, owner, repo, ref string) (sha string, kind RefKind, ok bool) {
	if !active(c) {
		return "", 0, false
	}
	for _, k := range []RefKind{Tag, Branch} {
		p, ok := refPath(c, owner, repo, k, ref)
		if !ok {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		if IsSHA(s) {
			return s, k, true
		}
	}
	return "", 0, false
}

// PutRef stores a resolved tag or branch. Commit kinds are not stored.
// Storing one kind removes any stale entry of the other kind so a ref that
// changed from branch to tag does not resurrect.
func PutRef(c *Cache, owner, repo string, kind RefKind, ref, sha string) error {
	if !active(c) || kind == Commit || !IsSHA(sha) {
		return nil
	}
	p, ok := refPath(c, owner, repo, kind, ref)
	if !ok {
		return nil
	}
	other := Branch
	if kind == Branch {
		other = Tag
	}
	if op, ok := refPath(c, owner, repo, other, ref); ok {
		_ = os.Remove(op)
	}
	if prev, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(prev)) == sha {
		c.Stats.SameSHA.Add(1)
	}
	return writeAtomic(p, []byte(sha+"\n"), now(c))
}

// GetContent returns cached bytes for a file at a SHA. Empty content is a
// miss (§6.5).
func GetContent(c *Cache, owner, repo, sha, path string) ([]byte, bool) {
	if !active(c) {
		return nil, false
	}
	p, ok := contentPath(c, owner, repo, sha, path)
	if !ok {
		return nil, false
	}
	count := c.Stats.first("content:" + strings.ToLower(owner+"/"+repo) + "@" + sha + "/" + path)
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		if count {
			c.Stats.ContentMisses.Add(1)
		}
		return nil, false
	}
	if count {
		c.Stats.ContentHits.Add(1)
	}
	return data, true
}

// PutContent stores fetched bytes. Empty content is not stored.
func PutContent(c *Cache, owner, repo, sha, path string, data []byte) error {
	if !active(c) || len(data) == 0 {
		return nil
	}
	p, ok := contentPath(c, owner, repo, sha, path)
	if !ok {
		return nil
	}
	if err := writeAtomic(p, data, now(c)); err != nil {
		return err
	}
	// Checksum sidecar (spec-cache §3.1), written after the content.
	return writeAtomic(p+SidecarSuffix, []byte(SHA256Hex(data)+"\n"), now(c))
}

// SidecarSuffix is appended to a content path for its checksum file.
const SidecarSuffix = ".sha256"

// SHA256Hex is the hex sha256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeAtomic writes data to a temp file in the target's directory, fsyncs,
// and renames over the target (§6.4). mtime is set explicitly so TTL is
// measured against the cache's clock.
func writeAtomic(target string, data []byte, mtime time.Time) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chtimes(tmpName, mtime, mtime); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return err
	}
	return nil
}

// --- meta partition (advanced §11) ---------------------------------------------

// metaPath is v1/meta/<sha256 of key>.json.
func metaPath(c *Cache, key string) string {
	return filepath.Join(c.Root, SchemaVersion, "meta", SHA256Hex([]byte(key))+".json")
}

// GetMeta returns a cached enrichment payload younger than ttl.
func GetMeta(c *Cache, key string, ttl time.Duration) ([]byte, bool) {
	if !active(c) {
		return nil, false
	}
	p := metaPath(c, key)
	st, err := os.Stat(p)
	if err != nil || now(c).Sub(st.ModTime()) > ttl {
		return nil, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return data, true
}

// PutMeta stores an enrichment payload.
func PutMeta(c *Cache, key string, data []byte) error {
	if !active(c) {
		return nil
	}
	return writeAtomic(metaPath(c, key), data, now(c))
}
