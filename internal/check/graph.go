package check

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Graph is the walk itself turned into output (spec-graph §3): one node per
// resolved identity, one edge per uses: occurrence. It is filled by the
// walk when set in Options.Graph, in breadth-first order, so node ids are
// n<index in BFS order>.
type Graph struct {
	mu    sync.Mutex
	Entry string
	Nodes []*Node
	Edges []Edge
	byKey map[string]*Node
}

// NewGraph returns an empty graph to pass in Options.
func NewGraph() *Graph { return &Graph{byKey: map[string]*Node{}} }

// Node is one file (or one unfollowed reference) in the graph (§3.1).
type Node struct {
	ID string `json:"id"`
	// Kind: workflow, composite-action, node-action, docker-action, unknown;
	// "" for skipped and unresolved references.
	Kind string `json:"kind,omitempty"`
	// Source: entry, local, linked, fetched, skipped, unresolved, failed.
	Source string `json:"source"`
	// Display is what the user wrote, normalised; the entry's path for the
	// entry node.
	Display string `json:"display"`
	// Path is the absolute path (local, linked) or the path within the
	// repository (fetched).
	Path string `json:"path,omitempty"`
	// Repo is owner/name: the remote for fetched, the link's slug for
	// linked, the entry repo's own slug for local ("" when unknown).
	Repo    string `json:"repo,omitempty"`
	Ref     string `json:"ref,omitempty"`
	RefKind string `json:"ref_kind,omitempty"`
	SHA     string `json:"sha,omitempty"`
	// Cache: how the cache answered (fetched only). A resolved ref reports
	// hit, miss, stale or bypassed; a literal SHA skips ref resolution and
	// reports the content cache's hit, miss or bypassed (never stale).
	Cache    string   `json:"cache,omitempty"`
	LinkPath string   `json:"link_path,omitempty"`
	Error    string   `json:"error,omitempty"`
	Inputs   []string `json:"inputs,omitempty"`
	Outputs  []string `json:"outputs,omitempty"`
	// Cut is set when --depth stopped the walk below this node; CutEdges
	// is how many references were not followed.
	Cut      bool `json:"cut,omitempty"`
	CutEdges int  `json:"cut_edges,omitempty"`
	// Name is the name check attributes findings to (the absolute path, or
	// owner/repo[/path]@ref), for joining with check output and for the
	// agreement test.
	Name string `json:"-"`
}

// Edge is one uses: occurrence (§3.2).
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Site: "job <name>", "job <name> / step <label>", or "step <label>"
	// inside a composite action.
	Site string `json:"site"`
	Line int    `json:"line"`
	Uses string `json:"uses"`
	// With lists the with: keys supplied at the call site (only when the
	// walk was asked to record them).
	With []string `json:"with,omitempty"`
	// ViaCheckout is owner/name[@ref] when a ./ reference resolved against
	// another repository's root checkout (medium §3).
	ViaCheckout string `json:"via_checkout,omitempty"`
}

// Children returns the edges out of a node in file order.
func Children(g *Graph, id string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

// NodeByID looks a node up by id.
func NodeByID(g *Graph, id string) *Node {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// add returns the node for key, creating it from mk on first sight.
func (g *Graph) add(key string, mk func() *Node) *Node {
	g.mu.Lock()
	defer g.mu.Unlock()
	if n, ok := g.byKey[key]; ok {
		return n
	}
	n := mk()
	n.ID = "n" + strconv.Itoa(len(g.Nodes))
	g.Nodes = append(g.Nodes, n)
	g.byKey[key] = n
	if len(g.Nodes) == 1 {
		g.Entry = n.ID
	}
	return n
}

// recordEntry adds the root node.
func (g *Graph) recordEntry(l *loader, root *loaded) {
	g.add(identity(root.src), func() *Node {
		n := nodeFor(l, root, root.src.path)
		n.Source = "entry"
		return n
	})
}

// record adds the edges out of cur and the nodes they reach. cut means the
// depth limit stopped the walk here.
func (g *Graph) record(l *loader, cur *loaded, results []loadResult, overrides map[int]*rules.CheckoutContext, cut bool, withKeys bool) {
	parent := g.add(identity(cur.src), func() *Node { return nodeFor(l, cur, cur.src.path) })
	if cut {
		parent.Cut = true
		parent.CutEdges = len(cur.file.Uses)
		return
	}
	for i, u := range cur.file.Uses {
		r := results[i]
		var child *Node
		if r.callee != nil {
			callee := r.callee
			child = g.add(identity(callee.src), func() *Node { return nodeFor(l, callee, u.Raw) })
		} else {
			how, errMsg := unfollowed(r)
			child = g.add("ref:"+how+":"+u.Raw, func() *Node {
				n := &Node{Source: how, Display: u.Raw, Error: errMsg}
				if how == "docker" {
					n.Source, n.Kind = "skipped", "docker-action"
				}
				return n
			})
		}
		e := Edge{From: parent.ID, To: child.ID, Site: site(cur.file, u), Line: u.Pos.Line, Uses: u.Raw}
		if withKeys {
			e.With = withNames(cur.file, u)
		}
		if ctx := overrides[i]; ctx != nil && ctx.Repository != "" && isLocalRef(u.Raw) {
			e.ViaCheckout = ctx.Repository
			if ctx.Ref != "" {
				e.ViaCheckout += "@" + ctx.Ref
			}
		}
		g.mu.Lock()
		g.Edges = append(g.Edges, e)
		g.mu.Unlock()
	}
}

func isLocalRef(raw string) bool {
	ref, err := uses.Parse(raw)
	return err == nil && ref.Kind == uses.Local
}

// unfollowed classifies a reference the walk did not load.
func unfollowed(r loadResult) (how, errMsg string) {
	switch r.how {
	case "docker", "skipped", "dynamic", "unresolved":
		how = r.how
		if how == "dynamic" {
			how = "unresolved"
		}
		return how, r.err
	}
	switch {
	case len(r.callerFindings) > 0:
		errMsg = r.callerFindings[0].Message
	case len(r.calleeFindings) > 0:
		errMsg = r.calleeFindings[0].Message
	default:
		errMsg = "could not be loaded"
	}
	return "failed", errMsg
}

// nodeFor describes a loaded file. display is what the user wrote.
func nodeFor(l *loader, ld *loaded, shown string) *Node {
	n := &Node{Display: shown, Name: display(ld.src), Kind: kindName(ld.file)}
	for _, in := range ld.file.Inputs {
		n.Inputs = append(n.Inputs, in.Name)
	}
	for _, o := range ld.file.Outputs {
		n.Outputs = append(n.Outputs, o.Name)
	}
	src := ld.src
	if !src.local {
		n.Source = "fetched"
		n.Path = src.rpath
		n.Repo = src.owner + "/" + src.repo
		n.Ref = src.ref
		n.RefKind = src.refKind
		n.SHA = src.sha
		n.Cache = src.cacheStatus
		return n
	}
	n.Path = src.path
	n.Source = "local"
	for _, lk := range repolink.All(l.links) {
		if lk.Path != src.root {
			continue
		}
		if src.root == l.entryRoot {
			// The entry repository's own (implicit) link: local, with the
			// link's slug as the repo (review D1).
			n.Repo = lk.Display
			return n
		}
		n.Source = "linked"
		n.LinkPath = lk.Path
		n.Repo = lk.Display
		return n
	}
	n.Repo = selfSlug(l, src)
	return n
}

// kindName is the §3.1 kind vocabulary.
func kindName(f *workflow.File) string {
	switch f.Kind {
	case workflow.KindWorkflow:
		return "workflow"
	case workflow.KindCompositeAction:
		return "composite-action"
	case workflow.KindOtherAction:
		if strings.HasPrefix(f.Using, "docker") {
			return "docker-action"
		}
		return "node-action"
	}
	return "unknown"
}

// site names the call site of a uses: (§3.2).
func site(f *workflow.File, u workflow.Uses) string {
	if u.JobLevel {
		return "job " + u.Job
	}
	label := "?"
	if idx, ok := stepIndexOfUses(f, u); ok {
		label = stepLabel(workflow.StepsOf(f, u.Job)[idx])
	}
	if f.Kind == workflow.KindWorkflow {
		return "job " + u.Job + " / step " + label
	}
	return "step " + label
}

// stepLabel is the step's id, else its name, else its 1-based index.
func stepLabel(st workflow.Step) string {
	switch {
	case st.ID != "":
		return st.ID
	case st.Name.Value != "":
		return st.Name.Value
	}
	return "#" + strconv.Itoa(st.Index+1)
}

// withNames lists the with: keys at a call site, sorted.
func withNames(f *workflow.File, u workflow.Uses) []string {
	var ws []workflow.With
	if u.JobLevel {
		if j := workflow.JobByName(f, u.Job); j != nil {
			ws = j.With
		}
	} else if idx, ok := stepIndexOfUses(f, u); ok {
		ws = workflow.StepsOf(f, u.Job)[idx].With
	}
	var out []string
	for _, w := range ws {
		out = append(out, w.Name)
	}
	sort.Strings(out)
	return out
}

// RelOrAbs is the display form of a local path: relative to base when
// under it, else absolute.
func RelOrAbs(p, base string) string {
	if base == "" {
		return p
	}
	rel, err := filepath.Rel(base, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.ToSlash(rel)
}
