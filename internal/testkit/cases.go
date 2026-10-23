package testkit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Kit is the shared per-test-run environment (test spec §3.2): a fake GitHub
// server, a temp cache dir, and a temp HOME.
type Kit struct {
	Server   *Server
	CacheDir string
	Home     string
	// Testdata is the absolute testdata/ root.
	Testdata string
}

// New sets up the kit. It must be called from a non-parallel test; subtests
// may then run in parallel sharing it.
func New(t *testing.T, testdata string) *Kit {
	t.Helper()
	abs, err := filepath.Abs(testdata)
	if err != nil {
		t.Fatal(err)
	}
	k := &Kit{
		Server:   FakeGitHub(t, filepath.Join(abs, "remote")),
		CacheDir: t.TempDir(),
		Home:     t.TempDir(),
		Testdata: abs,
	}
	t.Setenv("JOBSWORTH_CACHE_DIR", k.CacheDir)
	t.Setenv("HOME", k.Home)
	FakeTools(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(k.Home, ".cache"))
	t.Setenv("JOBSWORTH_API_URL", k.Server.URL)
	t.Setenv("JOBSWORTH_TOKEN", "test-token")
	return k
}

// Fetcher returns a client against the fake server sharing the kit's cache.
func Fetcher(k *Kit, offline bool) *fetch.Client {
	c := fetch.New(k.Server.URL, "test-token", cache.New(k.CacheDir))
	c.Offline = offline
	c.Sleep = func(time.Duration) {}
	return c
}

// Expect is one expected finding from a case header.
type Expect struct {
	Severity  finding.Severity
	Rule      string
	Substring string
	Count     int
}

// Case is one fixture under testdata/cases (test spec §2).
type Case struct {
	// Links maps owner/repo to a path relative to testdata.
	Links map[string]string
	// Name is the file name without extension; Title is its `name:`.
	Name  string
	Title string
	Path  string
	// ConfigYAML is the concatenated `config:` directives.
	ConfigYAML string
	// Skip is the reason to skip, or "".
	Skip string
	// Self is the entry repo's own owner/repo for checkout tracking.
	Self string
	// Expects is the expected multiset; None is an explicit `expect: none`.
	Expects []Expect
	// Shapes are the `# shape:` directives (spec-shape §9).
	Shapes []ShapeExpect
	// Warm lists external references ("owner/repo[/path]@ref") fetched
	// into the kit's cache before the case runs, so an offline case can
	// state what it expects to find there (test review 4 D3).
	Warm []string
	// UnixOnly marks a case that needs the fake tools on PATH (shell
	// scripts), skipped on Windows.
	UnixOnly bool
	None     bool
	Offline  bool
	// Graph marks a case whose rendered graph is compared against
	// testdata/golden/graph/<name>.{txt,json} (spec-graph §8).
	Graph bool
	// Enrich runs the case with the --enrich lookups against the fake
	// server (advanced §11).
	Enrich bool
}

// ShapeExpect is one `# shape: [--at <point>] '<expr>' => <render>` line;
// Mismatch is set for the `=> ✗ <substring>` form.
type ShapeExpect struct {
	At       string
	Expr     string
	Render   string
	Mismatch bool
}

var (
	reDirective = regexp.MustCompile(`^#\s*([a-z]+):?\s*(.*)$`)
	reExpect    = regexp.MustCompile(`^(error|warning|note)\s+([a-zA-Z0-9-]+)(?:\s+x(\d+))?(?:\s+"(.*)")?\s*$`)
	reName      = regexp.MustCompile(`^name:\s*(.*)$`)
)

// LoadCases reads every .yml/.yaml under dir (recursively) and parses its
// expectation header.
func LoadCases(t *testing.T, dir string) []Case {
	t.Helper()
	var cases []Case
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")) {
			return nil
		}
		c, err := ParseCase(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		cases = append(cases, c)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases
}

// ParseCase reads the leading comment block of one fixture.
func ParseCase(path string) (Case, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Case{}, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return Case{}, err
	}
	defer f.Close()

	c := Case{Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Path: abs, Links: map[string]string{}}
	var cfg []string
	inHeader := true
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := reName.FindStringSubmatch(line); m != nil && c.Title == "" {
			c.Title = strings.Trim(m[1], `"'`)
		}
		if !inHeader {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			if line != "" {
				inHeader = false
			}
			continue
		}
		m := reDirective.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, rest := m[1], strings.TrimSpace(m[2])
		switch key {
		case "expect":
			if rest == "none" {
				c.None = true
				continue
			}
			em := reExpect.FindStringSubmatch(rest)
			if em == nil {
				return Case{}, fmt.Errorf("bad expect directive: %q", line)
			}
			sev, _ := finding.ParseSeverity(em[1])
			e := Expect{Severity: sev, Rule: em[2], Count: 1, Substring: em[4]}
			if em[3] != "" {
				e.Count, _ = strconv.Atoi(em[3])
			}
			c.Expects = append(c.Expects, e)
		case "link":
			eq := strings.IndexByte(rest, '=')
			if eq < 0 {
				return Case{}, fmt.Errorf("bad link directive: %q", line)
			}
			c.Links[strings.TrimSpace(rest[:eq])] = strings.TrimSpace(rest[eq+1:])
		case "config":
			cfg = append(cfg, rest)
		case "offline":
			c.Offline = true
		case "warm":
			c.Warm = append(c.Warm, strings.Fields(rest)...)
		case "self":
			c.Self = rest
		case "graph":
			c.Graph = true
		case "unix":
			c.UnixOnly = true
		case "enrich":
			c.Enrich = true
		case "shape":
			se, err := parseShapeDirective(rest)
			if err != nil {
				return Case{}, fmt.Errorf("bad shape directive: %q: %v", line, err)
			}
			c.Shapes = append(c.Shapes, se)
		case "skip":
			c.Skip = rest
			if c.Skip == "" {
				c.Skip = "skipped"
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Case{}, err
	}
	c.ConfigYAML = strings.Join(cfg, "\n")
	if c.None && len(c.Expects) > 0 {
		return Case{}, fmt.Errorf("expect: none alongside expect lines")
	}
	return c, nil
}

// parseShapeDirective reads `[--at point] '<expr>' => <render>`.
func parseShapeDirective(rest string) (ShapeExpect, error) {
	var se ShapeExpect
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "--at ") {
		rest = strings.TrimSpace(rest[5:])
		i := strings.IndexByte(rest, ' ')
		if i < 0 {
			return se, fmt.Errorf("missing expression after --at")
		}
		se.At, rest = rest[:i], strings.TrimSpace(rest[i+1:])
	}
	if !strings.HasPrefix(rest, "'") {
		return se, fmt.Errorf("expression must be single-quoted")
	}
	end := strings.Index(rest[1:], "' => ")
	if end < 0 {
		return se, fmt.Errorf("expected '<expr>' => <render>")
	}
	se.Expr = rest[1 : 1+end]
	want := strings.TrimSpace(rest[1+end+len("' => "):])
	if strings.HasPrefix(want, "✗") {
		se.Mismatch = true
		want = strings.TrimSpace(strings.TrimPrefix(want, "✗"))
	}
	se.Render = want
	return se, nil
}

// Options builds the check options for a case: the fake fetcher, the case's
// links, its inline config, and testdata/callees as the repo root.
func Options(t *testing.T, k *Kit, c Case) check.Options {
	t.Helper()
	if len(c.Warm) > 0 {
		Warm(t, k, c.Warm...)
	}
	links := repolink.New()
	for slug, rel := range c.Links {
		p := filepath.Join(k.Testdata, filepath.FromSlash(rel))
		lk, err := repolink.ParseExplicit(slug+"="+p, k.Testdata, k.Home)
		if err != nil {
			t.Fatalf("link %s: %v", slug, err)
		}
		if err := repolink.AddExplicit(links, lk); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	if c.ConfigYAML != "" {
		var err error
		cfg, err = config.Parse([]byte(c.ConfigYAML), rules.IsKnown)
		if err != nil {
			t.Fatalf("config: %v", err)
		}
	}
	HarnessConfig(&cfg, c.ConfigYAML)
	self := c.Self
	if self == "" {
		// A case never shells out (test review 1 D1, medium review 5 N13):
		// without a slug the walk would ask git for the origin remote.
		self = "example/app"
	}
	return check.Options{
		Links:        links,
		Fetcher:      Fetcher(k, c.Offline),
		Config:       cfg,
		RootOverride: filepath.Join(k.Testdata, "callees"),
		SelfSlug:     self,
		Enrich:       c.Enrich,
		Store:        cache.New(k.CacheDir),
	}
}

// HarnessConfig applies the corpus-wide defaults: the fake remote's owners
// and najemi-software (the tool's own action, tag-pinned by init's
// generated workflow) are trusted here only, never in the shipped
// defaults; and permissions-missing (a repository-default heuristic) is
// off unless the case configures it, as a repo whose default token is
// read-only would see it.
func HarnessConfig(cfg *config.Config, caseConfig string) {
	cfg.SecurityTrustedOwners = append(cfg.SecurityTrustedOwners, "acme", "dorny", "najemi-software", "hashicorp")
	if !strings.Contains(caseConfig, "permissions-missing") {
		if cfg.Rules == nil {
			cfg.Rules = map[string]config.Override{}
		}
		cfg.Rules["permissions-missing"] = config.Override{Off: true}
	}
}

// Assert compares the result against the case's expectations as an exact
// multiset (test spec §2.1). Expectations are assigned to findings by
// backtracking, so overlapping substrings never steal each other's match.
func Assert(t *testing.T, c Case, got check.Result) {
	t.Helper()
	// Every emitted finding must carry a registered id (spec-rules §3.1).
	for _, f := range append(check.AllFindings(got), check.AllSuppressed(got)...) {
		if !rules.IsKnown(f.Rule) {
			t.Errorf("finding with unregistered rule id %q: %s", f.Rule, f.Message)
		}
	}
	actual := check.AllFindings(got)
	var expects []Expect
	for _, e := range c.Expects {
		for n := 0; n < e.Count; n++ {
			expects = append(expects, e)
		}
	}
	fits := func(e Expect, a finding.Finding) bool {
		return a.Severity == e.Severity && a.Rule == e.Rule && strings.Contains(a.Message, e.Substring)
	}
	used := make([]bool, len(actual))
	var assign func(i int) bool
	assign = func(i int) bool {
		if i == len(expects) {
			return true
		}
		for j, a := range actual {
			if used[j] || !fits(expects[i], a) {
				continue
			}
			used[j] = true
			if assign(i + 1) {
				return true
			}
			used[j] = false
		}
		return false
	}
	if assign(0) && len(expects) == len(actual) {
		return
	}
	// Report the mismatch greedily so the message names something useful.
	used = make([]bool, len(actual))
	for _, e := range expects {
		found := false
		for j, a := range actual {
			if !used[j] && fits(e, a) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s %s %q, not found", e.Severity, e.Rule, e.Substring)
		}
	}
	for j, a := range actual {
		if !used[j] {
			t.Errorf("unexpected %s %s at %s:%d: %s", a.Severity, a.Rule, filepath.Base(a.File), a.Line, a.Message)
		}
	}
}
