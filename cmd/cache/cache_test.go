package cachecmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

// t0 is the frozen "now" for goldens; cache entries are stamped 3 minutes
// earlier so ages and TTLs are deterministic.
var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

type result struct {
	stdout, stderr string
	code           int
}

type harness struct {
	kit *testkit.Kit
	now time.Time
	// stdin feeds confirmation prompts; terminal enables them.
	stdin    string
	terminal bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{kit: testkit.New(t, testdata), now: t0}
}

func (h *harness) run(t *testing.T, sub string, tokens ...string) result {
	t.Helper()
	schema := Subcommands()[sub].Schema
	args, flags, err := cli.ParseArgs(schema, tokens)
	if err == nil {
		err = cli.Validate(schema, args, flags)
	}
	if err != nil {
		t.Fatalf("parse %s %q: %v", sub, tokens, err)
	}
	env := map[string]string{"JOBSWORTH_CACHE_DIR": h.kit.CacheDir, "JOBSWORTH_API_URL": h.kit.Server.URL, "JOBSWORTH_TOKEN": "t"}
	var out, errb bytes.Buffer
	now := h.now
	err = RunWith(sub, args, flags, Env{
		Cwd: t.TempDir(), Home: t.TempDir(), Getenv: func(k string) string { return env[k] },
		Stdin: strings.NewReader(h.stdin), Stdout: &out, Stderr: &errb, StdinIsTerminal: h.terminal,
		Now: func() time.Time { return now },
	})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	return result{stdout: strings.ReplaceAll(out.String(), h.kit.CacheDir, "<ROOT>"), stderr: errb.String(), code: cli.ExitCode(err, cli.ExitFindings)}
}

// warm populates the cache from the fake server and freezes every entry's
// mtime at t0-3m.
func (h *harness) warm(t *testing.T, refs ...string) {
	t.Helper()
	testkit.Warm(t, h.kit, refs...)
	h.freeze(t, t0.Add(-3*time.Minute))
}

func (h *harness) freeze(t *testing.T, at time.Time) {
	t.Helper()
	_ = filepath.WalkDir(h.kit.CacheDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = os.Chtimes(p, at, at)
		}
		return nil
	})
}

func (h *harness) store() *cache.Cache {
	c := cache.New(h.kit.CacheDir)
	now := h.now
	c.Now = func() time.Time { return now }
	return c
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", "cache", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if string(want) != got {
		t.Fatalf("golden %s differs (run with -update to accept)\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

const three = 3 // refs warmed by warmThree

func (h *harness) warmThree(t *testing.T) {
	h.warm(t, "actions/checkout@v4", "actions/checkout@main", "acme/shared/.github/workflows/lint.yml@v2")
}

// --- path (§2.1, §6) --------------------------------------------------------

func TestPath(t *testing.T) {
	h := newHarness(t)
	r := h.run(t, "path")
	if r.code != 0 || r.stdout != "<ROOT>\n" {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(h.kit.CacheDir, "v1")); err == nil {
		t.Fatal("path must not create the cache")
	}
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o644)
	if r := h.run(t, "path", "--cache-dir", file); r.code != 2 {
		t.Fatalf("file as cache dir: %+v", r)
	}
}

// --- ls (§2.2) --------------------------------------------------------------

func TestLs(t *testing.T) {
	h := newHarness(t)
	if r := h.run(t, "ls"); r.code != 0 || !strings.Contains(r.stdout, "empty cache") {
		t.Fatalf("empty: %+v", r)
	}
	h.warmThree(t)
	golden(t, "ls.txt", h.run(t, "ls").stdout)
	golden(t, "ls_files.txt", h.run(t, "ls", "--files").stdout)
	golden(t, "ls.json", h.run(t, "ls", "--format", "json").stdout)

	if r := h.run(t, "ls", "actions"); strings.Contains(r.stdout, "acme/") || !strings.Contains(r.stdout, "2 refs") {
		t.Fatalf("owner filter:\n%s", r.stdout)
	}
	if r := h.run(t, "ls", "acme/shared"); !strings.Contains(r.stdout, "1 ref,") || strings.Contains(r.stdout, "checkout") {
		t.Fatalf("repo filter:\n%s", r.stdout)
	}
	if r := h.run(t, "ls", "--kind", "tag"); strings.Contains(r.stdout, "@main") || !strings.Contains(r.stdout, "2 refs") {
		t.Fatalf("kind filter:\n%s", r.stdout)
	}
	if r := h.run(t, "ls", "--expired"); !strings.Contains(r.stdout, "0 refs") {
		t.Fatalf("nothing expired yet:\n%s", r.stdout)
	}
	h.now = t0.Add(11 * time.Minute) // branch TTL is 10m
	r := h.run(t, "ls", "--expired")
	if !strings.Contains(r.stdout, "actions/checkout@main") || !strings.Contains(r.stdout, "expired") || !strings.Contains(r.stdout, "1 ref,") {
		t.Fatalf("expired:\n%s", r.stdout)
	}
	// An expired ref is still listed in the full listing.
	if r := h.run(t, "ls"); !strings.Contains(r.stdout, "expired") || !strings.Contains(r.stdout, "3 refs") {
		t.Fatalf("full listing after expiry:\n%s", r.stdout)
	}
}

func TestLsUnreferenced(t *testing.T) {
	h := newHarness(t)
	h.warmThree(t)
	if r := h.run(t, "clear", "--refs", "--yes"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	golden(t, "ls_unreferenced.txt", h.run(t, "ls").stdout)
	if r := h.run(t, "ls", "--kind", "sha"); !strings.Contains(r.stdout, "(unreferenced)") || !strings.Contains(r.stdout, "0 refs, 3 SHAs unreferenced") {
		t.Fatalf("kind sha:\n%s", r.stdout)
	}
	if r := h.run(t, "ls", "--kind", "tag"); strings.Contains(r.stdout, "(unreferenced)") {
		t.Fatalf("kind tag should hide unreferenced:\n%s", r.stdout)
	}
}

// --- stats (§2.6, §3.2) -----------------------------------------------------

func TestStats(t *testing.T) {
	h := newHarness(t)
	if r := h.run(t, "stats"); r.code != 0 || !strings.Contains(r.stdout, "refs         0") {
		t.Fatalf("empty: %+v", r)
	}
	h.warmThree(t)
	cache.WriteRunStats(h.store(), cache.RunStats{Command: "check", At: t0, Hits: 39, Misses: 2, Stale: 3, APICalls: 5, FetchedBytes: 48120})
	h.freeze(t, t0.Add(-3*time.Minute))
	golden(t, "stats.txt", h.run(t, "stats").stdout)
	golden(t, "stats.json", h.run(t, "stats", "--format", "json").stdout)

	_ = os.WriteFile(filepath.Join(h.kit.CacheDir, "v1", "stats.json"), []byte("{corrupt"), 0o644)
	if r := h.run(t, "stats"); strings.Contains(r.stdout, "last run") {
		t.Fatalf("corrupt stats.json should drop the last run line:\n%s", r.stdout)
	}
}

func TestStatsFromCheckRun(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	wf := filepath.Join(root, ".github", "workflows")
	_ = os.MkdirAll(wf, 0o755)
	_ = os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\npermissions: read-all\n"), 0o644)
	args, flags, _ := cli.ParseArgs(checkcmd.Schema, nil)
	var out, errb bytes.Buffer
	env := map[string]string{"JOBSWORTH_CACHE_DIR": h.kit.CacheDir, "JOBSWORTH_API_URL": h.kit.Server.URL, "JOBSWORTH_TOKEN": "t"}
	if err := checkcmd.RunWith(args, flags, checkcmd.Env{Cwd: root, Home: t.TempDir(), Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &errb}); err != nil {
		t.Fatalf("check: %v\n%s", err, errb.String())
	}
	rs, ok := cache.ReadRunStats(h.store())
	if !ok || rs.Command != "check" || rs.APICalls != 3 || rs.Misses != 2 {
		t.Fatalf("stats.json = %+v %v", rs, ok)
	}
	if r := h.run(t, "stats"); !strings.Contains(r.stdout, "last run     hits 0 / misses 2 / stale 0 / api calls 3     (from the most recent check)") {
		t.Fatalf("stats:\n%s", r.stdout)
	}
}

func TestOldSchemaOnly(t *testing.T) {
	h := newHarness(t)
	_ = os.MkdirAll(filepath.Join(h.kit.CacheDir, "v0", "refs"), 0o755)
	for _, sub := range []string{"ls", "stats"} {
		if r := h.run(t, sub); r.code != 0 || !strings.Contains(r.stdout, "no v1 cache") || !strings.Contains(r.stdout, "found v0 (older binary)") {
			t.Fatalf("%s: %+v", sub, r)
		}
	}
	if r := h.run(t, "clear", "--yes"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	if _, err := os.Stat(filepath.Join(h.kit.CacheDir, "v0")); err != nil {
		t.Fatal("plain clear must not touch v0")
	}
	if r := h.run(t, "clear", "--yes", "--all-schemas"); r.code != 0 || !strings.Contains(r.stdout, "removed schema directory v0") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(filepath.Join(h.kit.CacheDir, "v0")); err == nil {
		t.Fatal("--all-schemas should remove v0")
	}
}

// --- clear (§2.3) -----------------------------------------------------------

// count returns refs and content files on disk.
func (h *harness) count(t *testing.T) (refs, files int) {
	t.Helper()
	l, err := cache.Scan(h.store())
	if err != nil {
		t.Fatal(err)
	}
	return len(l.Refs), len(l.Content)
}

func TestClearMatrix(t *testing.T) {
	cases := []struct {
		name            string
		tokens          []string
		advance         time.Duration
		wantRefs, wantF int
		check           func(t *testing.T, h *harness, r result)
	}{
		{name: "everything", tokens: nil, wantRefs: 0, wantF: 0, check: func(t *testing.T, h *harness, r result) {
			if !strings.Contains(r.stdout, "removed 3 refs, 3 files") || strings.Count(r.stdout, "removed actions/checkout@") != 2 {
				t.Fatalf("output:\n%s", r.stdout)
			}
			if _, err := os.Stat(filepath.Join(h.kit.CacheDir, "v1")); err != nil {
				t.Fatal("schema directory must survive clear")
			}
		}},
		{name: "refs", tokens: []string{"--refs"}, wantRefs: 0, wantF: 3},
		{name: "tags", tokens: []string{"--tags"}, wantRefs: 1, wantF: 3},
		{name: "branches", tokens: []string{"--branches"}, wantRefs: 2, wantF: 3},
		{name: "content", tokens: []string{"--content"}, wantRefs: 3, wantF: 0},
		{name: "expired nothing", tokens: []string{"--expired"}, wantRefs: 3, wantF: 3},
		{name: "expired branch", tokens: []string{"--expired"}, advance: 11 * time.Minute, wantRefs: 2, wantF: 2, check: func(t *testing.T, h *harness, r result) {
			if !strings.Contains(r.stdout, "removed actions/checkout@main") || !strings.Contains(r.stdout, "removed 1 ref, 1 file") {
				t.Fatalf("output:\n%s", r.stdout)
			}
		}},
		{name: "older-than nothing", tokens: []string{"--older-than", "30d"}, wantRefs: 3, wantF: 3},
		{name: "older-than all", tokens: []string{"--older-than", "1m"}, wantRefs: 0, wantF: 0},
		{name: "owner", tokens: []string{"actions"}, wantRefs: 1, wantF: 1},
		{name: "repo", tokens: []string{"acme/shared"}, wantRefs: 2, wantF: 2},
		{name: "repo case-insensitive", tokens: []string{"ACME/Shared"}, wantRefs: 2, wantF: 2},
		{name: "no match", tokens: []string{"nobody/home"}, wantRefs: 3, wantF: 3, check: func(t *testing.T, h *harness, r result) {
			if !strings.Contains(r.stdout, "removed 0 refs") {
				t.Fatalf("output:\n%s", r.stdout)
			}
		}},
		{name: "dry-run", tokens: []string{"--dry-run"}, wantRefs: 3, wantF: 3, check: func(t *testing.T, h *harness, r result) {
			if !strings.Contains(r.stdout, "would remove 3 refs, 3 files") {
				t.Fatalf("output:\n%s", r.stdout)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.warmThree(t)
			h.now = t0.Add(tc.advance)
			r := h.run(t, "clear", append([]string{"--yes"}, tc.tokens...)...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			refs, files := h.count(t)
			if refs != tc.wantRefs || files != tc.wantF {
				t.Fatalf("after clear: %d refs %d files, want %d %d\n%s", refs, files, tc.wantRefs, tc.wantF, r.stdout)
			}
			if tc.check != nil {
				tc.check(t, h, r)
			}
		})
	}
}

func TestClearPrompt(t *testing.T) {
	h := newHarness(t)
	h.warmThree(t)
	h.terminal, h.stdin = true, "n\n"
	if r := h.run(t, "clear"); r.code != 1 {
		t.Fatalf("declined: %+v", r)
	}
	if refs, _ := h.count(t); refs != three {
		t.Fatal("declined clear removed entries")
	}
	h.stdin = "y\n"
	if r := h.run(t, "clear"); r.code != 0 {
		t.Fatalf("accepted: %+v", r)
	}
	if refs, _ := h.count(t); refs != 0 {
		t.Fatal("accepted clear removed nothing")
	}
	if r := h.run(t, "clear", "--older-than", "soon", "--yes"); r.code != 2 {
		t.Fatalf("bad duration: %+v", r)
	}
	if r := h.run(t, "clear", "--yes"); r.code != 0 || !strings.Contains(r.stdout, "removed 0 refs, 0 files") {
		t.Fatalf("empty cache: %+v", r)
	}
}

// TestClearIsPerFile hammers the read path during a clear: every read is a
// clean hit or miss, never a partial entry (§2.3, §4.1).
func TestClearIsPerFile(t *testing.T) {
	h := newHarness(t)
	h.warmThree(t)
	store := h.store()
	sha := testkit.SHAFor("actions", "checkout", "v4")
	want, ok := cache.GetContent(store, "actions", "checkout", sha, "action.yml")
	if !ok {
		t.Fatal("warm content missing")
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if got, ok := cache.GetContent(store, "actions", "checkout", sha, "action.yml"); ok && !bytes.Equal(got, want) {
					t.Errorf("partial read: %d bytes", len(got))
				}
				if s, _, ok := cache.GetRef(store, "actions", "checkout", "v4"); ok && s != sha {
					t.Errorf("partial ref: %q", s)
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		if r := h.run(t, "clear", "--yes"); r.code != 0 {
			t.Fatal(r.stderr)
		}
		testkit.Warm(t, h.kit, "actions/checkout@v4")
	}
	close(stop)
	wg.Wait()
}

// --- verify (§2.5) ----------------------------------------------------------

func TestVerify(t *testing.T) {
	h := newHarness(t)
	h.warmThree(t)
	if r := h.run(t, "verify"); r.code != 0 || !strings.Contains(r.stdout, "checked 3 refs, 3 files") {
		t.Fatalf("clean: %+v", r)
	}
	v1 := filepath.Join(h.kit.CacheDir, "v1")
	write := func(rel, content string) {
		p := filepath.Join(v1, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sha := strings.Repeat("a", 40)
	write("refs/acme/x/tags/bad", "not-a-sha\n")                // corrupt ref
	write("content/acme/x/"+sha+"/empty.yml", "")               // corrupt: empty
	write("content/acme/x/"+sha+"/nosidecar.yml", "on: push\n") // corrupt: missing sidecar
	write("content/acme/x/"+sha+"/mismatch.yml", "on: push\n")  // corrupt: sha mismatch
	write("content/acme/x/"+sha+"/mismatch.yml.sha256", strings.Repeat("0", 64)+"\n")
	write("content/acme/x/"+sha+"/broken.yml", "jobs: [\n") // unparseable
	write("content/acme/x/"+sha+"/broken.yml.sha256", cache.SHA256Hex([]byte("jobs: [\n"))+"\n")
	write("refs/.DS_Store", "x")     // unexpected
	write("content/tmp.abc123", "x") // unexpected

	r := h.run(t, "verify")
	if r.code != 1 {
		t.Fatalf("exit %d:\n%s", r.code, r.stdout)
	}
	golden(t, "verify.txt", r.stdout)
	for _, want := range []string{"4 corrupt:", "1 unparseable:", "2 unexpected:", "(not a 40-hex SHA)", "(empty)", "(missing sha256 sidecar)", "(sha256 mismatch)", "refs/.DS_Store", "content/tmp.abc123"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	r = h.run(t, "verify", "--format", "json")
	var doc struct {
		Problems []struct{ Kind, Path string } `json:"problems"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || len(doc.Problems) != 7 {
		t.Fatalf("json: %v %d\n%s", err, len(doc.Problems), r.stdout)
	}

	r = h.run(t, "verify", "--fix")
	if r.code != 0 || !strings.Contains(r.stdout, "removed 6 entries") {
		t.Fatalf("fix: exit %d\n%s", r.code, r.stdout)
	}
	if _, err := os.Stat(filepath.Join(v1, "content", "acme", "x", sha, "broken.yml")); err != nil {
		t.Fatal("unparseable entry must be kept")
	}
	for _, rel := range []string{"refs/acme/x/tags/bad", "content/acme/x/" + sha + "/empty.yml", "content/acme/x/" + sha + "/nosidecar.yml", "content/acme/x/" + sha + "/mismatch.yml", "refs/.DS_Store", "content/tmp.abc123"} {
		if _, err := os.Stat(filepath.Join(v1, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s not removed", rel)
		}
	}
	r = h.run(t, "verify")
	if r.code != 1 || !strings.Contains(r.stdout, "1 unparseable:") || !strings.Contains(r.stdout, "0 corrupt:") {
		t.Fatalf("after fix:\n%s", r.stdout)
	}
}

// --- warm (§2.4, §6) --------------------------------------------------------

func TestWarm(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	wf := filepath.Join(root, ".github", "workflows")
	_ = os.MkdirAll(wf, 0o755)
	_ = os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/checkout@main\n  b:\n    uses: acme/shared/.github/workflows/lint.yml@v2\n    with:\n      env: x\npermissions: read-all\n"), 0o644)
	r := h.run(t, "warm", filepath.Join(wf, "ci.yml"))
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	// Cold: v4 (2 calls) + main (2) + lint.yml@v2 (2) + notify ./ in the
	// remote repo (content only) + 4 content files.
	if n := len(h.kit.Server.Requests()); n != 10 {
		t.Fatalf("cold warm made %d requests: %+v", n, h.kit.Server.Requests())
	}
	// lint.yml references actions/checkout@v4 a second time; distinct refs
	// are counted once (review N2).
	if !strings.Contains(r.stderr, "resolving 3 refs … 0 cached, 3 fetched") || !strings.Contains(r.stderr, "fetching 4 files … done") {
		t.Fatalf("progress:\n%s", r.stderr)
	}
	if !strings.Contains(r.stdout, "warm: 3 refs, 4 files,") {
		t.Fatalf("summary: %s", r.stdout)
	}

	// Warm again: everything cached, zero calls.
	h.kit.Server.ResetRequests()
	r = h.run(t, "warm", filepath.Join(wf, "ci.yml"))
	if n := len(h.kit.Server.Requests()); n != 0 {
		t.Fatalf("warm cache made %d requests", n)
	}
	if !strings.Contains(r.stderr, "3 cached, 0 fetched") || !strings.Contains(r.stdout, "(added 0)") {
		t.Fatalf("second warm:\n%s%s", r.stdout, r.stderr)
	}

	// --refresh: one ref call per ref, no content calls when SHAs are unchanged.
	h.kit.Server.ResetRequests()
	r = h.run(t, "warm", "--refresh", filepath.Join(wf, "ci.yml"))
	var refCalls, contentCalls int
	for _, rq := range h.kit.Server.Requests() {
		if strings.Contains(rq.Path, "/contents/") {
			contentCalls++
		} else {
			refCalls++
		}
	}
	if refCalls != 3 || contentCalls != 0 {
		t.Fatalf("refresh made %d ref calls and %d content calls: %+v", refCalls, contentCalls, h.kit.Server.Requests())
	}
	if !strings.Contains(r.stderr, "(3 re-resolved, same SHA)") {
		t.Fatalf("refresh progress:\n%s", r.stderr)
	}

	// --quiet prints only the summary.
	r = h.run(t, "warm", "--quiet", filepath.Join(wf, "ci.yml"))
	if r.stderr != "" || !strings.HasPrefix(r.stdout, "warm: ") {
		t.Fatalf("quiet: %+v", r)
	}
}

func TestWarmPositionalRefs(t *testing.T) {
	h := newHarness(t)
	r := h.run(t, "warm", "actions/checkout@v4", "acme/shared/.github/workflows/lint.yml@v2")
	if r.code != 0 || !strings.Contains(r.stdout, "warm: 2 refs,") {
		t.Fatalf("%+v", r)
	}
	if r := h.run(t, "ls"); !strings.Contains(r.stdout, "actions/checkout@v4") || !strings.Contains(r.stdout, "acme/shared@v2") {
		t.Fatalf("ls after positional warm:\n%s", r.stdout)
	}
	if r := h.run(t, "warm", "--parallel", "zero"); r.code != 2 {
		t.Fatalf("bad --parallel: %+v", r)
	}
}

func TestWarmAuthFailures(t *testing.T) {
	h := newHarness(t)
	r := h.run(t, "warm", "acme/forbidden@v1")
	if r.code != 1 || !strings.Contains(r.stderr, "nothing could be fetched") || !strings.Contains(r.stderr, "fetch-failed:") {
		t.Fatalf("all forbidden: %+v", r)
	}
	r = h.run(t, "warm", "acme/forbidden@v1", "actions/checkout@v4")
	if r.code != 0 || !strings.Contains(r.stderr, "fetch-failed:") {
		t.Fatalf("some forbidden: %+v", r)
	}
	r = h.run(t, "warm", "acme/nope@v1")
	if r.code != 0 || !strings.Contains(r.stderr, "fetch-not-found:") {
		t.Fatalf("404 is best effort: %+v", r)
	}
}

func TestWarmHonoursLinks(t *testing.T) {
	h := newHarness(t)
	linked, _ := filepath.Abs(filepath.Join(testdata, "repos", "acme-tools"))
	r := h.run(t, "warm", "--repo", "acme/tools="+linked, "acme/tools/.github/workflows/x.yml@v1")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if n := len(h.kit.Server.Requests()); n != 0 {
		t.Fatalf("linked repo was fetched: %d requests", n)
	}
}

// --- lock (§4.2) ------------------------------------------------------------

func TestLockContention(t *testing.T) {
	h := newHarness(t)
	h.warmThree(t)
	old := LockWait
	LockWait = 300 * time.Millisecond
	defer func() { LockWait = old }()

	// Held briefly: the second clear waits and succeeds.
	lock, err := cache.AcquireLock(h.store(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	blocked := make(chan struct{})
	var once sync.Once
	cache.LockContended = func() { once.Do(func() { close(blocked) }) }
	defer func() { cache.LockContended = nil }()
	done := make(chan result, 1)
	go func() { done <- h.run(t, "clear", "--yes", "--refs") }()
	<-blocked // clear found the lock held
	cache.Unlock(lock)
	if r := <-done; r.code != 0 || !strings.Contains(r.stdout, "removed 3 refs") {
		t.Fatalf("waiter: %+v", r)
	}

	// Held past the wait: exit 2.
	lock, err = cache.AcquireLock(h.store(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := h.run(t, "clear", "--yes")
	cache.Unlock(lock)
	if r.code != 2 || !strings.Contains(r.stderr, "locked by another process") {
		t.Fatalf("locked: %+v", r)
	}
	if _, files := h.count(t); files != 3 {
		t.Fatal("locked clear removed content")
	}
}

// TestDefaultEnvHasGHToken is spec-cache review D2: warm must fall back to
// `gh auth token` like check and init do.
func TestDefaultEnvHasGHToken(t *testing.T) {
	t.Parallel()
	if defaultEnv().GHToken == nil {
		t.Fatal("defaultEnv().GHToken is nil; warm would run unauthenticated on gh-only machines")
	}
}

func TestUnwritableCacheWarnsOnce(t *testing.T) {
	h := newHarness(t)
	file := filepath.Join(t.TempDir(), "not-a-dir")
	_ = os.WriteFile(file, nil, 0o644)
	// A root whose parent is a file cannot be created: writes fail.
	root := filepath.Join(file, "cache")
	r := h.run(t, "warm", "--cache-dir", root, "actions/checkout@v4", "actions/checkout@main")
	if r.code != 0 {
		t.Fatalf("warm must proceed without caching: exit %d %s", r.code, r.stderr)
	}
	if n := strings.Count(r.stderr, "is not writable"); n != 1 {
		t.Fatalf("expected one unwritable warning, got %d:\n%s", n, r.stderr)
	}
}

func TestHumanFormats(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{0: "0", 512: "512", 12288: "12K", 2200000: "2.1M", 52428800: "50M"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q want %q", n, got, want)
		}
	}
	for d, want := range map[time.Duration]string{45 * time.Second: "45s", 3 * time.Minute: "3m", 2*time.Hour + 10*time.Minute: "2h 10m", 2 * time.Hour: "2h", 5 * 24 * time.Hour: "5d"} {
		if got := humanAge(d); got != want {
			t.Errorf("humanAge(%v) = %q want %q", d, got, want)
		}
	}
	for s, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, "1.5d": 36 * time.Hour} {
		if got, err := parseDuration(s); err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v %v", s, got, err)
		}
	}
}
