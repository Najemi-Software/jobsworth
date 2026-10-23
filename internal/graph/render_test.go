package graph_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/graph"
)

// sample builds: entry n0 -> local n1 -> fetched n2; n0 -> n2 again;
// n0 -> failed n3; n1 -> local n4 (cut).
func sample() *check.Graph {
	g := check.NewGraph()
	g.Entry = "n0"
	g.Nodes = []*check.Node{
		{ID: "n0", Kind: "workflow", Source: "entry", Display: "/repo/.github/workflows/ci.yml", Path: "/repo/.github/workflows/ci.yml"},
		{ID: "n1", Kind: "composite-action", Source: "local", Display: "./.github/actions/setup", Path: "/repo/.github/actions/setup/action.yml"},
		{ID: "n2", Kind: "node-action", Source: "fetched", Display: "actions/checkout@v4", Repo: "actions/checkout", Ref: "v4", RefKind: "tag", SHA: "d23441a48e516b6c34aea4fa41551a30e30af803", Cache: "hit", Path: "action.yml"},
		{ID: "n3", Source: "failed", Display: "acme/private@main", Error: "403 forbidden"},
		{ID: "n4", Kind: "composite-action", Source: "local", Display: "./.github/actions/cache", Path: "/repo/.github/actions/cache/action.yml", Cut: true, CutEdges: 2},
	}
	g.Edges = []check.Edge{
		{From: "n0", To: "n1", Site: "job build / step setup", Line: 8, Uses: "./.github/actions/setup"},
		{From: "n1", To: "n2", Site: "step checkout", Line: 4, Uses: "actions/checkout@v4"},
		{From: "n1", To: "n4", Site: "step cache", Line: 6, Uses: "./.github/actions/cache"},
		{From: "n0", To: "n2", Site: "job test / step checkout", Line: 14, Uses: "actions/checkout@v4"},
		{From: "n0", To: "n3", Site: "job report", Line: 20, Uses: "acme/private@main"},
	}
	return g
}

func render(g *check.Graph, o graph.TreeOptions) string {
	var b bytes.Buffer
	o.Cwd = "/repo"
	if o.Depth == 0 {
		o.Depth = -1
	}
	graph.Tree(&b, g, o)
	return b.String()
}

func TestTreeDedupeAndCut(t *testing.T) {
	got := render(sample(), graph.TreeOptions{})
	want := `.github/workflows/ci.yml  (workflow, entry)
├─ job build / step setup   → ./.github/actions/setup  (composite-action, local)
│  ├─ step checkout → actions/checkout@v4  (node-action, fetched, tag → d23441a, cache hit)
│  └─ step cache    → ./.github/actions/cache  (composite-action, local)
│     └─ … (2 more)
├─ job test / step checkout → actions/checkout@v4  ↑
└─ job report               → acme/private@main  (failed: 403 forbidden)

5 nodes: 1 entry, 2 local, 1 fetched (1 cache hit, 0 misses, 0 stale), 1 failed
`
	if got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
}

func TestTreeNoDedupe(t *testing.T) {
	got := render(sample(), graph.TreeOptions{NoDedupe: true})
	if strings.Count(got, "(node-action, fetched, tag → d23441a, cache hit)") != 2 {
		t.Errorf("--no-dedupe should expand the fetched node twice:\n%s", got)
	}
	if strings.Contains(got, "↑") {
		t.Errorf("no dedupe marker expected:\n%s", got)
	}
}

func TestTreeExternalOnly(t *testing.T) {
	got := render(sample(), graph.TreeOptions{ExternalOnly: true})
	want := `.github/workflows/ci.yml  (workflow, entry)
├─ job build / step setup / step checkout → actions/checkout@v4  (node-action, fetched, tag → d23441a, cache hit)
├─ job test / step checkout               → actions/checkout@v4  ↑
└─ job report                             → acme/private@main  (failed: 403 forbidden)

5 nodes: 1 entry, 2 local, 1 fetched (1 cache hit, 0 misses, 0 stale), 1 failed
`
	if got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
}

func TestTreeColour(t *testing.T) {
	got := render(sample(), graph.TreeOptions{Color: true})
	if !strings.Contains(got, "\x1b[31m(failed: 403 forbidden)\x1b[0m") {
		t.Errorf("failed should be red:\n%q", got)
	}
	if !strings.Contains(got, "\x1b[2m↑\x1b[0m") {
		t.Errorf("dedupe marker should be dim:\n%q", got)
	}
}

func TestDot(t *testing.T) {
	g := sample()
	g.Nodes[3].Error = `quote " and \ backslash`
	var b bytes.Buffer
	graph.Dot(&b, g, "/repo")
	got := b.String()
	for _, want := range []string{
		"digraph jobsworth {",
		`n0 [label=".github/workflows/ci.yml\nentry", shape=box, color=black];`,
		`n2 [label="actions/checkout@v4\nfetched d23441a", shape=ellipse, color=blue];`,
		`n3 [label="acme/private@main\nfailed", shape=ellipse, color=red];`,
		`n0 -> n1 [label="job build / step setup"];`,
		"}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dot missing %q:\n%s", want, got)
		}
	}
	for _, id := range []string{"n0", "n1", "n2", "n3", "n4"} {
		if strings.Count(got, "  "+id+" [") != 1 {
			t.Errorf("node %s should be declared exactly once:\n%s", id, got)
		}
	}
}

func TestMermaid(t *testing.T) {
	g := sample()
	g.Edges[0].Site = `job "quoted" / step setup`
	var b bytes.Buffer
	graph.Mermaid(&b, g, "/repo")
	got := b.String()
	for _, want := range []string{
		"flowchart LR\n",
		`n0[".github/workflows/ci.yml<br/>entry"]`,
		`n2(["actions/checkout@v4<br/>fetched d23441a"])`,
		`n0 -->|"job #quot;quoted#quot; / step setup"| n1`,
		"classDef failed",
		"class n3 failed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("mermaid missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"quoted"`) {
		t.Errorf("quotes must be escaped in labels:\n%s", got)
	}
}

func TestJSONSchema(t *testing.T) {
	d := graph.Build(sample(), 3)
	if d.Schema != 1 || d.Entry != "n0" || len(d.Nodes) != 5 || len(d.Edges) != 5 {
		t.Fatalf("document: %+v", d)
	}
	if d.Summary.Nodes != 5 || d.Summary.BySource["local"] != 2 || d.Summary.Cache["hit"] != 1 || d.Summary.APICalls != 3 {
		t.Errorf("summary: %+v", d.Summary)
	}
}

func TestDepthZeroEntryOnly(t *testing.T) {
	g := check.NewGraph()
	g.Entry = "n0"
	g.Nodes = []*check.Node{{ID: "n0", Kind: "workflow", Source: "entry", Display: "/repo/x.yml", Path: "/repo/x.yml", Cut: true, CutEdges: 3}}
	var b bytes.Buffer
	graph.Tree(&b, g, graph.TreeOptions{Cwd: "/repo", Depth: 0})
	got := b.String()
	want := "x.yml  (workflow, entry)\n\n1 node: 1 entry\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
