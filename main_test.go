package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/graph"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/shapeq"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestCases runs every fixture under testdata/cases through the check
// pipeline and compares findings against its header (test spec §2.3).
func TestCases(t *testing.T) {
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		c := c
		name := c.Name
		if c.Title != "" {
			name += "/" + strings.ReplaceAll(c.Title, " ", "_")
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if c.Skip != "" {
				t.Skip(c.Skip)
			}
			if c.UnixOnly && runtime.GOOS == "windows" {
				t.Skip("needs the fake tools on PATH")
			}
			got := check.RunEntry(c.Path, testkit.Options(t, kit, c))
			testkit.Assert(t, c, got)
		})
	}
}

// TestCasesIsolated runs every `# offline` case with a cache of its own
// (test spec §4.1: no dependence on test ordering; test review 4 D3). An
// offline case fetches nothing, so this is cheap, and it fails when a
// case relies on a sibling having warmed the shared cache.
func TestCasesIsolated(t *testing.T) {
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		if !c.Offline || c.Skip != "" {
			continue
		}
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			if c.UnixOnly && runtime.GOOS == "windows" {
				t.Skip("needs the fake tools on PATH")
			}
			own := *kit
			own.CacheDir = t.TempDir()
			got := check.RunEntry(c.Path, testkit.Options(t, &own, c))
			testkit.Assert(t, c, got)
		})
	}
}

// TestRegistryCoverage pins that every registered rule has a fixture that
// expects it, and every fixture expectation names a registered rule (test
// spec §6.5).
func TestRegistryCoverage(t *testing.T) {
	t.Parallel()
	expected := map[string]bool{}
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		for _, e := range c.Expects {
			if !rules.IsKnown(e.Rule) {
				t.Errorf("%s expects unknown rule %q", c.Name, e.Rule)
			}
			expected[e.Rule] = true
			if rules.IsDerived(e.Rule) {
				expected["shellcheck-codes"] = true // a derived id covers its umbrella
			}
		}
	}
	// Principle 4 and §6.2–6.3: per rule, a triggering case and the nearest
	// non-triggering case, both named after the rule. file-invalid's
	// non-triggering case is any parseable file; it is still named.
	names := map[string]bool{}
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		names[c.Name] = true
	}
	hasNamed := func(snake string, ok bool) bool {
		for n := range names {
			if strings.HasPrefix(n, snake) && strings.HasSuffix(n, "_ok") == ok {
				return true
			}
		}
		return false
	}
	for id := range rules.Registry {
		if !expected[id] {
			t.Errorf("rule %s has no fixture expecting it", id)
		}
		snake := strings.ReplaceAll(id, "-", "_")
		if !hasNamed(snake, false) {
			t.Errorf("rule %s has no fixture named %s*.yml", id, snake)
		}
		if !hasNamed(snake, true) {
			t.Errorf("rule %s has no nearest non-triggering fixture %s*_ok.yml", id, snake)
		}
	}
}

// TestCasesNeverShellOut enforces test spec §2.3: a case never shells
// out. A git shim first on PATH records every invocation and fails; the
// corpus must leave it untouched (the self slug comes from the harness,
// not from the developer's clone).
func TestCasesNeverShellOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim")
	}
	shim := t.TempDir()
	log := filepath.Join(shim, "calls.log")
	script := "#!/bin/sh\necho \"$0 $*\" >> " + log + "\nexit 1\n"
	for _, tool := range []string{"git", "gh"} {
		if err := os.WriteFile(filepath.Join(shim, tool), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shim)
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		if c.Skip != "" || c.UnixOnly {
			continue
		}
		check.RunEntry(c.Path, testkit.Options(t, kit, c))
	}
	if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
		t.Fatalf("the corpus shelled out:\n%s", data)
	}
}

// TestFixtureNamingAndHeaders checks the corpus conventions (test spec
// §2.1–2.2): every case has an expectation header and a name:.
func TestFixtureNamingAndHeaders(t *testing.T) {
	t.Parallel()
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		if !c.None && len(c.Expects) == 0 {
			t.Errorf("%s: no expect: header", c.Name)
		}
		if c.None && !strings.HasSuffix(c.Name, "_ok") {
			t.Errorf("%s: expects none but is not named *_ok", c.Name)
		}
		if !c.None && strings.HasSuffix(c.Name, "_ok") {
			t.Errorf("%s: named *_ok but expects findings", c.Name)
		}
		if c.Title == "" && !strings.HasSuffix(c.Name, "name_missing") {
			t.Errorf("%s: no name:", c.Name)
		}
		if strings.ToLower(c.Name) != c.Name || strings.Contains(c.Name, "-") {
			t.Errorf("%s: file names are lower_snake_case", c.Name)
		}
		if _, err := os.Stat(filepath.Join("testdata", "cases", c.Name+".yml")); err != nil {
			t.Errorf("%s: expected .yml extension", c.Name)
		}
	}
}

// TestGraphGoldens renders every `# graph` case as tree and json against
// testdata/golden/graph/<name>.{txt,json} (spec-graph §8). Each case gets
// a fresh cache so cache status is deterministic.
func TestGraphGoldens(t *testing.T) {
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		if !c.Graph {
			continue
		}
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			opts := testkit.Options(t, kit, c)
			opts.Fetcher = fetch.New(kit.Server.URL, "test-token", cache.New(t.TempDir()))
			opts.LoadOnly = true
			if opts.SelfSlug == "" {
				opts.SelfSlug = "example/app" // never read from git: goldens must not depend on the checkout
			}
			g := check.NewGraph()
			opts.Graph = g
			check.RunEntry(c.Path, opts)
			stable := func(s string) string { return strings.ReplaceAll(s, kit.Testdata, "<TESTDATA>") }
			var tree, js bytes.Buffer
			graph.Tree(&tree, g, graph.TreeOptions{Cwd: filepath.Dir(c.Path), Home: kit.Home, Depth: -1})
			if err := graph.JSON(&js, g, opts.Fetcher.Stats.APICalls.Load()); err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "golden", "graph", c.Name+".txt"), stable(tree.String()))
			golden(t, filepath.Join("testdata", "golden", "graph", c.Name+".json"), stable(js.String()))
		})
	}
}

func golden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// TestGraphAgreesWithCheck is spec-graph §6/§8: for every case, the files
// check validated and the files graph shows as loaded are the same set
// with the same sources. Cases on deliberately unstable remotes (flaky,
// rate limited, timing out) are excluded: their outcome depends on
// request order.
func TestGraphAgreesWithCheck(t *testing.T) {
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			if c.Skip != "" {
				t.Skip(c.Skip)
			}
			data, _ := os.ReadFile(c.Path)
			for _, unstable := range []string{"acme/flaky", "acme/limited", "acme/secondary", "acme/slow"} {
				if bytes.Contains(data, []byte(unstable)) {
					t.Skip("unstable remote " + unstable)
				}
			}
			store := cache.New(t.TempDir())
			tr := check.NewTrace()
			opts := testkit.Options(t, kit, c)
			opts.Fetcher = fetch.New(kit.Server.URL, "test-token", store)
			opts.Fetcher.Offline = c.Offline
			opts.Trace = tr
			check.RunEntry(c.Path, opts)

			g := check.NewGraph()
			gopts := testkit.Options(t, kit, c)
			gopts.Fetcher = fetch.New(kit.Server.URL, "test-token", store)
			gopts.Fetcher.Offline = c.Offline
			gopts.LoadOnly = true
			gopts.Graph = g
			check.RunEntry(c.Path, gopts)

			checked := map[string]string{}
			for name, ft := range tr.Files {
				checked[name] = ft.How
			}
			graphed := map[string]string{}
			for _, n := range g.Nodes {
				switch n.Source {
				case "entry":
					graphed[n.Name] = "local"
				case "local", "linked", "fetched":
					graphed[n.Name] = n.Source
				}
			}
			for name, how := range checked {
				if graphed[name] != how {
					t.Errorf("check validated %s (%s) but graph has %q", name, how, graphed[name])
				}
			}
			for name, src := range graphed {
				if _, ok := checked[name]; !ok {
					t.Errorf("graph loaded %s (%s) but check never validated it", name, src)
				}
			}
		})
	}
}

// TestGraphShaCacheStatus is spec-graph review D2: a SHA-pinned fetched
// node reports the content cache's answer, so it is a miss on a cold
// store and a hit on the next run.
func TestGraphShaCacheStatus(t *testing.T) {
	kit := testkit.New(t, "testdata")
	c, err := testkit.ParseCase("testdata/cases/graph_sha_and_branch.yml")
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(t.TempDir())
	for run, want := range []string{"miss", "hit"} {
		opts := testkit.Options(t, kit, c)
		opts.Fetcher = fetch.New(kit.Server.URL, "test-token", store)
		opts.LoadOnly = true
		g := check.NewGraph()
		opts.Graph = g
		check.RunEntry(c.Path, opts)
		fetched := 0
		for _, n := range g.Nodes {
			if n.Source != "fetched" {
				continue
			}
			fetched++
			if n.Cache != want {
				t.Errorf("run %d: %s (%s) cache %q, want %q", run+1, n.Display, n.RefKind, n.Cache, want)
			}
		}
		if fetched != 3 {
			t.Errorf("run %d: %d fetched nodes, want 3", run+1, fetched)
		}
	}
}

// TestShapeDirectives evaluates every `# shape:` directive against its
// rendered shape or mismatch (spec-shape §9).
func TestShapeDirectives(t *testing.T) {
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		if len(c.Shapes) == 0 {
			continue
		}
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			tr := check.NewTrace()
			opts := testkit.Options(t, kit, c)
			opts.Trace = tr
			check.RunEntry(c.Path, opts)
			ft := tr.Get(c.Path)
			if ft == nil {
				t.Fatal("no trace for the entry")
			}
			for _, se := range c.Shapes {
				got, err := evalDirective(ft, se)
				if se.Mismatch {
					if err == nil || !strings.Contains(err.Error(), se.Render) {
						t.Errorf("%s %q: want mismatch containing %q, got %v (shape %s)", se.At, se.Expr, se.Render, err, got)
					}
					continue
				}
				if err != nil {
					t.Errorf("%s %q: unexpected error: %v", se.At, se.Expr, err)
					continue
				}
				if got != se.Render {
					t.Errorf("%s %q: got %s, want %s", se.At, se.Expr, got, se.Render)
				}
			}
		})
	}
}

// evalDirective runs one directive: the rendered shape, or an error
// carrying the mismatch or point message.
func evalDirective(ft *check.FileTrace, se testkit.ShapeExpect) (string, error) {
	p, err := shapeq.ResolvePoint(ft, se.At, se.Expr)
	if err != nil {
		return "", err
	}
	a, err := shapeq.Query(ft, p, se.Expr)
	if err != nil {
		return "", err
	}
	if a.Error != nil {
		return shape.Render(a.Shape, 4), errors.New(a.Error.Message)
	}
	render := shape.Render(a.Shape, 4)
	if render == "Any" {
		// The reason is what the user reads (shape review 1 N6): a
		// directive may pin it as `Any (opaque: <reason>)`.
		if reason := shapeq.OpaqueReason(a); reason != "" && strings.HasPrefix(se.Render, "Any (opaque:") {
			render += " (opaque: " + reason + ")"
		}
	}
	return render, nil
}

// TestShapeAgreesWithCheck is spec-shape §9: for every expression in the
// corpus, `shape` at that expression's own point reports a mismatch
// exactly when check reported shape-mismatch there, with the same
// message.
func TestShapeAgreesWithCheck(t *testing.T) {
	kit := testkit.New(t, "testdata")
	for _, c := range testkit.LoadCases(t, "testdata/cases") {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			if c.Skip != "" {
				t.Skip(c.Skip)
			}
			tr := check.NewTrace()
			opts := testkit.Options(t, kit, c)
			opts.Trace = tr
			res := check.RunEntry(c.Path, opts)
			ft := tr.Get(c.Path)
			if ft == nil || ft.ShapeInputs == nil {
				return
			}
			reported := map[string][]string{} // "line:col" -> messages
			for _, f := range check.AllFindings(res) {
				if f.Rule == "shape-mismatch" && f.File == c.Path {
					k := fmt.Sprintf("%d:%d", f.Line, f.Column)
					reported[k] = append(reported[k], f.Message)
				}
			}
			seen := map[string]bool{}
			for _, x := range ft.File.Exprs {
				k := fmt.Sprintf("%d:%d", x.Pos.Line, x.Pos.Column)
				a, err := shapeq.Query(ft, shapeq.Point{Job: x.Job, Step: x.Step}, x.Text)
				if err != nil {
					continue
				}
				msgs := reported[k]
				switch {
				case a.Mismatch != nil:
					found := false
					for _, m := range msgs {
						if m == a.Mismatch.Message {
							found = true
						}
					}
					if !found {
						t.Errorf("%s at %s: shape reports %q but check reported %v", x.Text, k, a.Mismatch.Message, msgs)
					}
					seen[k] = true
				case len(msgs) > 0 && !seen[k]:
					// Another expression on the same position may carry it.
					other := false
					for _, y := range ft.File.Exprs {
						if y.Pos == x.Pos && y.Text != x.Text {
							other = true
						}
					}
					if !other {
						t.Errorf("%s at %s: check reported %v but shape finds no mismatch", x.Text, k, msgs)
					}
				}
			}
		})
	}
}
