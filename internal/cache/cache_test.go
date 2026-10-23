package cache

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const sha = "0123456789abcdef0123456789abcdef01234567"
const sha2 = "fedcba9876543210fedcba9876543210fedcba98"

// clock is a controllable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newCache(t *testing.T) (*Cache, *clock) {
	t.Helper()
	ck := &clock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	c := New(t.TempDir())
	c.Now = ck.now
	return c, ck
}

func TestRefTTL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind RefKind
		ttl  time.Duration
		ref  string
	}{
		{Tag, TagTTL, "v4"},
		{Branch, BranchTTL, "main"},
		{Branch, BranchTTL, "feature/x"},
	} {
		t.Run(tc.kind.String()+"/"+tc.ref, func(t *testing.T) {
			t.Parallel()
			c, ck := newCache(t)
			if err := PutRef(c, "Acme", "Tools", tc.kind, tc.ref, sha); err != nil {
				t.Fatal(err)
			}
			got, kind, ok := GetRef(c, "acme", "tools", tc.ref)
			if !ok || got != sha || kind != tc.kind {
				t.Fatalf("fresh: %q %v %v", got, kind, ok)
			}
			ck.advance(tc.ttl - time.Second)
			if _, _, ok := GetRef(c, "acme", "tools", tc.ref); !ok {
				t.Fatal("just inside TTL should hit")
			}
			ck.advance(2 * time.Second)
			if _, _, ok := GetRef(c, "acme", "tools", tc.ref); ok {
				t.Fatal("past TTL should miss")
			}
		})
	}
}

func TestCommitKindNotCached(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	if err := PutRef(c, "a", "b", Commit, "abc1234", sha); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := GetRef(c, "a", "b", "abc1234"); ok {
		t.Fatal("commit refs must not be cached")
	}
}

func TestPutRefReplacesOtherKind(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	_ = PutRef(c, "a", "b", Branch, "x", sha)
	_ = PutRef(c, "a", "b", Tag, "x", sha2)
	got, kind, ok := GetRef(c, "a", "b", "x")
	if !ok || got != sha2 || kind != Tag {
		t.Fatalf("got %q %v %v", got, kind, ok)
	}
	if _, err := os.Stat(filepath.Join(c.Root, SchemaVersion, "refs", "a", "b", "branches", "x")); !os.IsNotExist(err) {
		t.Fatal("stale branch entry should be removed")
	}
}

func TestContentNeverExpires(t *testing.T) {
	t.Parallel()
	c, ck := newCache(t)
	if err := PutContent(c, "a", "b", sha, ".github/workflows/x.yml", []byte("on: push\n")); err != nil {
		t.Fatal(err)
	}
	ck.advance(1000 * time.Hour)
	got, ok := GetContent(c, "A", "B", sha, ".github/workflows/x.yml")
	if !ok || string(got) != "on: push\n" {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestCorruptEntriesAreMisses(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(c.Root, SchemaVersion, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, c.Now(), c.Now())
	}
	write("refs/a/b/tags/short", "abc123\n")
	write("refs/a/b/tags/upper", strings.ToUpper(sha)+"\n")
	write("refs/a/b/tags/empty", "")
	write("content/a/b/"+sha+"/empty.yml", "")
	for _, ref := range []string{"short", "upper", "empty", "absent"} {
		if _, _, ok := GetRef(c, "a", "b", ref); ok {
			t.Errorf("ref %s should miss", ref)
		}
	}
	if _, ok := GetContent(c, "a", "b", sha, "empty.yml"); ok {
		t.Error("empty content should miss")
	}
	if _, ok := GetContent(c, "a", "b", sha, "absent.yml"); ok {
		t.Error("absent content should miss")
	}
	// A miss is overwritten by the next put.
	_ = PutRef(c, "a", "b", Tag, "short", sha)
	if got, _, ok := GetRef(c, "a", "b", "short"); !ok || got != sha {
		t.Errorf("overwrite failed: %q %v", got, ok)
	}
}

func TestSchemaVersionIsolates(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	_ = PutRef(c, "a", "b", Tag, "v1", sha)
	if _, err := os.Stat(filepath.Join(c.Root, SchemaVersion, "refs", "a", "b", "tags", "v1")); err != nil {
		t.Fatalf("entry not under %s: %v", SchemaVersion, err)
	}
	// Entries under another schema version are invisible.
	old := filepath.Join(c.Root, "v0", "refs", "a", "b", "tags", "v2")
	_ = os.MkdirAll(filepath.Dir(old), 0o755)
	_ = os.WriteFile(old, []byte(sha), 0o644)
	if _, _, ok := GetRef(c, "a", "b", "v2"); ok {
		t.Fatal("v0 entry should be invisible")
	}
}

func TestUnsafeKeysAreIgnored(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	for _, ref := range []string{"../x", "a/../b", "", "/abs", "a//b"} {
		if err := PutRef(c, "a", "b", Tag, ref, sha); err != nil {
			t.Errorf("PutRef(%q) = %v", ref, err)
		}
		if _, _, ok := GetRef(c, "a", "b", ref); ok {
			t.Errorf("GetRef(%q) should miss", ref)
		}
	}
	if err := PutContent(c, "a", "b", sha, "../escape.yml", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, SchemaVersion, "content", "a", "b", "escape.yml")); err == nil {
		t.Fatal("path traversal wrote outside the sha dir")
	}
	if err := PutContent(c, "a", "b", "notasha", "x.yml", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := PutRef(c, "a/x", "b", Tag, "v1", sha); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledAndNil(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	c.Disabled = true
	_ = PutRef(c, "a", "b", Tag, "v1", sha)
	_ = PutContent(c, "a", "b", sha, "x.yml", []byte("x"))
	if _, _, ok := GetRef(c, "a", "b", "v1"); ok {
		t.Fatal("disabled cache should miss")
	}
	if _, ok := GetContent(c, "a", "b", sha, "x.yml"); ok {
		t.Fatal("disabled cache should miss")
	}
	entries, _ := os.ReadDir(c.Root)
	if len(entries) != 0 {
		t.Fatal("disabled cache wrote to disk")
	}
	var nilc *Cache
	if _, _, ok := GetRef(nilc, "a", "b", "v1"); ok {
		t.Fatal("nil cache should miss")
	}
	if err := PutRef(nilc, "a", "b", Tag, "v1", sha); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicWriteLeavesNoTemp(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	_ = PutContent(c, "a", "b", sha, "x.yml", []byte("one"))
	_ = PutContent(c, "a", "b", sha, "x.yml", []byte("two"))
	dir := filepath.Join(c.Root, SchemaVersion, "content", "a", "b", sha)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "x.yml" || entries[1].Name() != "x.yml"+SidecarSuffix {
		t.Fatalf("dir = %v", entries)
	}
	side, _ := os.ReadFile(filepath.Join(dir, "x.yml"+SidecarSuffix))
	if strings.TrimSpace(string(side)) != SHA256Hex([]byte("two")) {
		t.Fatalf("sidecar = %q", side)
	}
	got, _ := GetContent(c, "a", "b", sha, "x.yml")
	if string(got) != "two" {
		t.Fatalf("got %q", got)
	}
}

func TestConcurrentWriters(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := []byte(strings.Repeat("x", 100+i))
			for j := 0; j < 20; j++ {
				_ = PutContent(c, "a", "b", sha, "x.yml", payload)
				_ = PutRef(c, "a", "b", Tag, "v1", sha)
				if got, ok := GetContent(c, "a", "b", sha, "x.yml"); ok && len(got) < 100 {
					t.Errorf("torn read: %d bytes", len(got))
				}
				if got, _, ok := GetRef(c, "a", "b", "v1"); ok && got != sha {
					t.Errorf("torn ref: %q", got)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestDefaultRoot(t *testing.T) {
	t.Parallel()
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	if got, _ := DefaultRoot("/flag", env(map[string]string{"JOBSWORTH_CACHE_DIR": "/env"})); got != "/flag" {
		t.Errorf("flag wins: %q", got)
	}
	if got, _ := DefaultRoot("", env(map[string]string{"JOBSWORTH_CACHE_DIR": "/env"})); got != "/env" {
		t.Errorf("env: %q", got)
	}
	if got, _ := DefaultRoot("", env(map[string]string{"XDG_CACHE_HOME": "/xdg"})); got != "/xdg/jobsworth" {
		t.Errorf("XDG must apply on every platform: %q", got)
	}
	got, err := DefaultRoot("", env(nil))
	if err != nil || !strings.HasSuffix(got, "jobsworth") {
		t.Errorf("default: %q %v", got, err)
	}
}

func TestIsSHA(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		sha: true, strings.ToUpper(sha): false, sha[:39]: false, sha + "0": false, "v4": false, "": false,
	} {
		if got := IsSHA(s); got != want {
			t.Errorf("IsSHA(%q) = %v", s, got)
		}
	}
}

func TestStatsCounters(t *testing.T) {
	t.Parallel()
	c, ck := newCache(t)
	// Each distinct key counts once, by its first outcome.
	if _, _, ok := GetRef(c, "a", "b", "v1"); ok {
		t.Fatal("unexpected hit")
	}
	_ = PutRef(c, "a", "b", Tag, "v1", sha)
	_ = PutRef(c, "a", "b", Tag, "v1", sha) // same SHA again
	GetRef(c, "a", "b", "v1")               // a hit, but v1 was already counted as a miss
	_ = PutRef(c, "a", "b", Tag, "v2", sha)
	GetRef(c, "a", "b", "v2") // first lookup of v2: hit
	_ = PutRef(c, "a", "b", Tag, "v3", sha)
	ck.advance(TagTTL + time.Second)
	GetRef(c, "a", "b", "v3") // first lookup of v3: stale
	_ = PutContent(c, "a", "b", sha, "x.yml", []byte("x"))
	GetContent(c, "a", "b", sha, "x.yml")
	GetContent(c, "a", "b", sha, "x.yml") // repeat: not counted
	GetContent(c, "a", "b", sha, "nope.yml")
	if h, m, st, same := c.Stats.Hits(), c.Stats.Misses(), c.Stats.Stale.Load(), c.Stats.SameSHA.Load(); h != 2 || m != 2 || st != 1 || same != 1 {
		t.Fatalf("hits %d misses %d stale %d same %d", h, m, st, same)
	}
	if c.Stats.RefHits.Load() != 1 || c.Stats.ContentHits.Load() != 1 || c.Stats.RefMisses.Load() != 1 || c.Stats.ContentMisses.Load() != 1 {
		t.Fatalf("breakdown: %+v", &c.Stats)
	}
	if c.Stats.Distinct() != 5 {
		t.Fatalf("distinct = %d", c.Stats.Distinct())
	}
}

func TestRefreshMissesWithinTTL(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	_ = PutRef(c, "a", "b", Tag, "v1", sha)
	c.Refresh = true
	if _, _, ok := GetRef(c, "a", "b", "v1"); ok {
		t.Fatal("refresh should miss")
	}
	if c.Stats.Stale.Load() != 1 {
		t.Fatal("refresh miss should count as stale")
	}
	_ = PutContent(c, "a", "b", sha, "x.yml", []byte("x"))
	if _, ok := GetContent(c, "a", "b", sha, "x.yml"); !ok {
		t.Fatal("content is immutable and must hit under refresh")
	}
}

func TestRunStats(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	if _, ok := ReadRunStats(c); ok {
		t.Fatal("absent stats should not read")
	}
	WriteRunStats(c, RunStats{Command: "check", At: c.Now(), Hits: 3, APICalls: 1})
	rs, ok := ReadRunStats(c)
	if !ok || rs.Command != "check" || rs.Hits != 3 || rs.APICalls != 1 {
		t.Fatalf("got %+v %v", rs, ok)
	}
	_ = os.WriteFile(filepath.Join(c.Root, SchemaVersion, StatsFile), []byte("{nope"), 0o644)
	if _, ok := ReadRunStats(c); ok {
		t.Fatal("corrupt stats should be ignored")
	}
}

func TestLock(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	l1, err := AcquireLock(c, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(c, 100*time.Millisecond); err != ErrLocked {
		t.Fatalf("second lock err = %v", err)
	}
	blocked := make(chan struct{})
	var once sync.Once
	LockContended = func() { once.Do(func() { close(blocked) }) }
	defer func() { LockContended = nil }()
	done := make(chan error, 1)
	go func() {
		l2, err := AcquireLock(c, 5*time.Second)
		if err == nil {
			Unlock(l2)
		}
		done <- err
	}()
	<-blocked // the waiter found the lock held
	Unlock(l1)
	if err := <-done; err != nil {
		t.Fatalf("waiter should succeed after release: %v", err)
	}
}

// TestCrashBeforeRenameKeepsOld is the §4 "write, crash before rename,
// read sees old" case: a temp file left by an interrupted writeAtomic
// (same naming) never shadows the committed body.
func TestCrashBeforeRenameKeepsOld(t *testing.T) {
	t.Parallel()
	c, _ := newCache(t)
	if err := PutContent(c, "a", "b", sha, "x.yml", []byte("old")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(c.Root, SchemaVersion, "content", "a", "b", sha)
	// The writer got as far as the temp file, then died.
	tmp := filepath.Join(dir, ".x.yml.tmp-123456")
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := GetContent(c, "a", "b", sha, "x.yml")
	if !ok || string(got) != "old" {
		t.Fatalf("got %q ok=%v, want the committed body", got, ok)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// The leftover is inert: a later successful write replaces the body
	// and the sidecar, and reads keep seeing committed data only.
	if err := PutContent(c, "a", "b", sha, "x.yml", []byte("newer")); err != nil {
		t.Fatal(err)
	}
	got, _ = GetContent(c, "a", "b", sha, "x.yml")
	if string(got) != "newer" {
		t.Fatalf("after rewrite got %q (dir before: %s)", got, strings.Join(names, ","))
	}
}
