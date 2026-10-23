package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/repolink"
)

// CallersOptions configures the inverse scan (spec-graph §5).
type CallersOptions struct {
	// Target is the absolute path of the file whose callers are wanted.
	Target string
	// Root is the entry repository's root; every linked repository is
	// scanned too.
	Root   string
	Links  *repolink.Links
	Config config.Config
	// RepoSlug names the entry repository ("" when unknown).
	RepoSlug string
	// ExtraRoots are further local repositories to scan (the current
	// directory's, when the target lives in a linked checkout).
	ExtraRoots []string
}

// localEdge is one local-to-local uses: seen while scanning.
type localEdge struct {
	from, to   string
	site       string
	line       int
	uses       string
	sourceSlug string
}

// FindCallers walks every workflow and action file in the entry repo and
// the linked repos as an entry, without fetching, and keeps every path
// that ends at the target. The result graph has the target as entry and
// edges pointing from caller to callee.
func FindCallers(o CallersOptions) (*check.Graph, error) {
	if _, err := os.Stat(o.Target); err != nil {
		return nil, err
	}
	type repoInfo struct {
		slug   string
		linked bool
	}
	roots := map[string]repoInfo{o.Root: {slug: o.RepoSlug}}
	for _, lk := range repolink.All(o.Links) {
		if _, seen := roots[lk.Path]; !seen {
			roots[lk.Path] = repoInfo{slug: lk.Display, linked: true}
		}
	}
	for _, r := range o.ExtraRoots {
		if _, seen := roots[r]; !seen && r != "" {
			slug := ""
			if lk, ok := repolink.Detect(r, repolink.CWD); ok {
				slug = lk.Display
			}
			roots[r] = repoInfo{slug: slug}
		}
	}
	rootOf := func(p string) (string, repoInfo) {
		best := ""
		for r := range roots {
			if (p == r || hasPrefixDir(p, r)) && len(r) > len(best) {
				best = r
			}
		}
		return best, roots[best]
	}

	var files []string
	for r := range roots {
		for _, g := range []string{".github/workflows/*.{yml,yaml}", ".github/actions/**/action.{yml,yaml}"} {
			m, _ := doublestar.FilepathGlob(filepath.Join(r, g), doublestar.WithFilesOnly())
			files = append(files, m...)
		}
	}
	files = append(files, o.Target)
	sort.Strings(files)

	nodes := map[string]*check.Node{} // by path
	var edges []localEdge
	seenEdge := map[string]bool{}
	memo := check.NewParseMemo() // shared callees parse once across the scan (review N4)
	for i, f := range files {
		if i > 0 && files[i-1] == f {
			continue
		}
		root, _ := rootOf(f)
		g := check.NewGraph()
		check.RunEntry(f, check.Options{Links: o.Links, Config: o.Config, RootOverride: root, LoadOnly: true, NoFetch: true, Graph: g, ParseMemo: memo})
		for _, n := range g.Nodes {
			if n.Path == "" || n.Source == "fetched" || n.Source == "failed" || n.Source == "skipped" || n.Source == "unresolved" {
				continue
			}
			if _, ok := nodes[n.Path]; !ok {
				c := *n
				c.Source = "local"
				r, info := rootOf(n.Path)
				c.Repo = info.slug
				if info.linked {
					c.Source, c.LinkPath = "linked", r
				}
				nodes[n.Path] = &c
			}
		}
		for _, e := range g.Edges {
			from, to := check.NodeByID(g, e.From), check.NodeByID(g, e.To)
			if from.Path == "" || to.Path == "" || nodes[from.Path] == nil || nodes[to.Path] == nil {
				continue
			}
			key := fmt.Sprintf("%s|%s|%s|%d", from.Path, to.Path, e.Site, e.Line)
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			edges = append(edges, localEdge{from: from.Path, to: to.Path, site: e.Site, line: e.Line, uses: e.Uses})
		}
	}

	target := nodes[o.Target]
	if target == nil {
		return nil, fmt.Errorf("%s is not a workflow or action file", o.Target)
	}
	out := check.NewGraph()
	ids := map[string]string{}
	add := func(path string) string {
		if id, ok := ids[path]; ok {
			return id
		}
		n := *nodes[path]
		n.Display = n.Path
		out.Nodes = append(out.Nodes, &n)
		n.ID = fmt.Sprintf("n%d", len(out.Nodes)-1)
		ids[path] = n.ID
		return n.ID
	}
	add(o.Target)
	out.Nodes[0].Source = "entry"
	out.Entry = out.Nodes[0].ID
	// Breadth-first upward: direct callers first, in file order.
	queue := []string{o.Target}
	visited := map[string]bool{o.Target: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		var in []localEdge
		for _, e := range edges {
			if e.to == cur {
				in = append(in, e)
			}
		}
		sort.SliceStable(in, func(i, j int) bool {
			if in[i].from != in[j].from {
				return in[i].from < in[j].from
			}
			return in[i].line < in[j].line
		})
		for _, e := range in {
			fromID := add(e.from)
			out.Edges = append(out.Edges, check.Edge{From: fromID, To: ids[cur], Site: e.site, Line: e.line, Uses: e.uses})
			if !visited[e.from] {
				visited[e.from] = true
				queue = append(queue, e.from)
			}
		}
	}
	return out, nil
}

func hasPrefixDir(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !(len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator))
}
