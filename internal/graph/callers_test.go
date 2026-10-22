package graph_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/graph"
	"github.com/najemi-software/jobsworth/internal/repolink"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repos builds an app repo whose release.yml and pr.yml call lint.yml,
// and a linked tools repo whose ci.yml calls app's lint.yml through the
// link, with nightly.yml calling ci.yml.
func repos(t *testing.T) (app, tools string, links *repolink.Links) {
	t.Helper()
	base := t.TempDir()
	app = filepath.Join(base, "app")
	tools = filepath.Join(base, "tools")
	write(t, filepath.Join(app, ".github/workflows/lint.yml"), "on: workflow_call\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo lint\nname: T1\n")
	write(t, filepath.Join(app, ".github/workflows/release.yml"), "on: push\njobs:\n  lint:\n    uses: ./.github/workflows/lint.yml\nname: T2\npermissions: read-all\n")
	write(t, filepath.Join(app, ".github/workflows/pr.yml"), "on: pull_request\njobs:\n  lint:\n    uses: ./.github/workflows/lint.yml\nname: T3\npermissions: read-all\n")
	write(t, filepath.Join(app, ".github/workflows/unrelated.yml"), "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/greet\nname: T4\npermissions: read-all\n")
	write(t, filepath.Join(app, ".github/actions/greet/action.yml"), "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hi\nname: N\ndescription: D\n")
	write(t, filepath.Join(tools, ".github/workflows/ci.yml"), "on: push\njobs:\n  checks:\n    uses: acme/app/.github/workflows/lint.yml@main\nname: T5\npermissions: read-all\n")
	write(t, filepath.Join(tools, ".github/workflows/nightly.yml"), "on: schedule\njobs:\n  ci:\n    uses: ./.github/workflows/ci.yml\nname: T6\npermissions: read-all\n")
	links = repolink.New()
	for slug, p := range map[string]string{"acme/app": app, "acme/tools": tools} {
		lk, err := repolink.ParseExplicit(slug+"="+p, base, base)
		if err != nil {
			t.Fatal(err)
		}
		if err := repolink.AddExplicit(links, lk); err != nil {
			t.Fatal(err)
		}
	}
	return app, tools, links
}

func TestCallersDirectTransitiveLinked(t *testing.T) {
	app, tools, links := repos(t)
	// The app repo is the entry repo: it is "linked" to itself by slug so
	// the tools repo's external reference resolves to it.
	g, err := graph.FindCallers(graph.CallersOptions{Target: filepath.Join(app, ".github/workflows/lint.yml"), Root: app, Links: links, RepoSlug: "acme/app"})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	graph.Callers(&b, g, graph.TreeOptions{Cwd: app, Home: filepath.Dir(tools)})
	got := b.String()
	want := `.github/workflows/lint.yml  (workflow)
◄─ job lint in .github/workflows/pr.yml  (acme/app)
◄─ job lint in .github/workflows/release.yml  (acme/app)
◄─ job checks in ~/tools/.github/workflows/ci.yml  (acme/tools, linked)
   ◄─ job ci in ~/tools/.github/workflows/nightly.yml

4 callers in 2 repos (3 direct, 1 transitive)
`
	if got != want {
		t.Errorf("callers:\n%s\nwant:\n%s", got, want)
	}
	if g.Entry != "n0" || len(g.Edges) != 4 {
		t.Errorf("graph: entry %s, %d edges", g.Entry, len(g.Edges))
	}
	for _, e := range g.Edges {
		if e.From == g.Entry {
			t.Errorf("edges must point into the target: %+v", e)
		}
	}
}

func TestCallersNone(t *testing.T) {
	app, _, links := repos(t)
	g, err := graph.FindCallers(graph.CallersOptions{Target: filepath.Join(app, ".github/workflows/unrelated.yml"), Root: app, Links: links})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	graph.Callers(&b, g, graph.TreeOptions{Cwd: app})
	if got := b.String(); !strings.HasSuffix(got, "\n0 callers\n") || !strings.HasPrefix(got, ".github/workflows/unrelated.yml  (workflow)\n") {
		t.Errorf("got:\n%s", got)
	}
}

func TestCallersOfAction(t *testing.T) {
	app, _, links := repos(t)
	g, err := graph.FindCallers(graph.CallersOptions{Target: filepath.Join(app, ".github/actions/greet/action.yml"), Root: app, Links: links})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	graph.Callers(&b, g, graph.TreeOptions{Cwd: app})
	got := b.String()
	if !strings.Contains(got, "◄─ job a / step #1 in .github/workflows/unrelated.yml") || !strings.HasSuffix(got, "1 caller in 1 repo (1 direct, 0 transitive)\n") {
		t.Errorf("got:\n%s", got)
	}
}

func TestCallersMissingTarget(t *testing.T) {
	app, _, links := repos(t)
	if _, err := graph.FindCallers(graph.CallersOptions{Target: filepath.Join(app, "nope.yml"), Root: app, Links: links}); err == nil {
		t.Fatal("expected an error for a missing target")
	}
}
