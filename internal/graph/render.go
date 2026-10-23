// Package graph renders a check.Graph (spec-graph §4): the tree, json, dot
// and mermaid formats, and the upward tree of --callers (§5).
package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/check"
)

// TreeOptions tunes the tree renderer.
type TreeOptions struct {
	// Cwd makes local paths relative; Home abbreviates linked roots to ~.
	Cwd, Home string
	// Color enables ANSI colour (stdout is a terminal).
	Color bool
	// ExternalOnly collapses local and linked nodes (§4.1).
	ExternalOnly bool
	// NoDedupe prints a node's subtree at every occurrence.
	NoDedupe bool
	// Depth is the --depth value, or -1; it only affects the entry's marker.
	Depth int
}

const (
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	reset  = "\x1b[0m"
)

func paint(on bool, code, s string) string {
	if !on || s == "" {
		return s
	}
	return code + s + reset
}

// Tree writes the default format (§4.1).
func Tree(w io.Writer, g *check.Graph, o TreeOptions) {
	root := check.NodeByID(g, g.Entry)
	if root == nil {
		fmt.Fprintln(w, "(empty graph)")
		return
	}
	fmt.Fprintf(w, "%s  %s\n", check.RelOrAbs(root.Display, o.Cwd), paint(o.Color, dim, "("+root.Kind+", entry)"))
	expanded := map[string]bool{root.ID: true}
	renderChildren(w, g, root, "", o, expanded, map[string]bool{root.ID: true})
	fmt.Fprintln(w)
	fmt.Fprintln(w, Summary(g))
}

// child is one line to print under a node: its edge (with a possibly
// hoisted site) and the node it reaches.
type child struct {
	site string
	edge check.Edge
	node *check.Node
}

// childrenOf lists what to print under n, hoisting through local and
// linked nodes under --external-only. onPath guards cycles while hoisting.
func childrenOf(g *check.Graph, n *check.Node, o TreeOptions, prefix string, onPath map[string]bool) []child {
	var out []child
	for _, e := range check.Children(g, n.ID) {
		to := check.NodeByID(g, e.To)
		site := e.Site
		if prefix != "" {
			site = prefix + " / " + site
		}
		if o.ExternalOnly && (to.Source == "local" || to.Source == "linked") {
			if onPath[to.ID] {
				continue
			}
			onPath[to.ID] = true
			out = append(out, childrenOf(g, to, o, site, onPath)...)
			delete(onPath, to.ID)
			continue
		}
		out = append(out, child{site: site, edge: e, node: to})
	}
	return out
}

func renderChildren(w io.Writer, g *check.Graph, n *check.Node, indent string, o TreeOptions, expanded, onPath map[string]bool) {
	kids := childrenOf(g, n, o, "", map[string]bool{n.ID: true})
	if n.Cut {
		if n.CutEdges > 0 && !(n.ID == g.Entry && o.Depth == 0) {
			fmt.Fprintf(w, "%s└─ %s\n", indent, paint(o.Color, dim, fmt.Sprintf("… (%d more)", n.CutEdges)))
		}
		return
	}
	width := 0
	for _, k := range kids {
		if len(k.site) > width {
			width = len(k.site)
		}
	}
	for i, k := range kids {
		last := i == len(kids)-1
		branch, next := "├─ ", "│  "
		if last {
			branch, next = "└─ ", "   "
		}
		already := expanded[k.node.ID] || onPath[k.node.ID]
		line := fmt.Sprintf("%s%s%-*s → %s", indent, branch, width, k.site, k.node.Display)
		switch {
		case already && (!o.NoDedupe || onPath[k.node.ID]):
			fmt.Fprintf(w, "%s  %s\n", line, paint(o.Color, dim, "↑"))
			continue
		default:
			line += "  " + describe(k.node, k.edge, o)
			if len(k.edge.With) > 0 {
				line += "  " + paint(o.Color, dim, "with: "+strings.Join(k.edge.With, ", "))
			}
			fmt.Fprintln(w, line)
		}
		expanded[k.node.ID] = true
		onPath[k.node.ID] = true
		renderChildren(w, g, k.node, indent+next, o, expanded, onPath)
		delete(onPath, k.node.ID)
	}
}

// describe is the parenthesised source summary after a node (§4.1).
func describe(n *check.Node, e check.Edge, o TreeOptions) string {
	var s string
	switch n.Source {
	case "entry":
		s = n.Kind + ", entry"
	case "local":
		s = n.Kind + ", local"
	case "linked":
		s = n.Kind + ", linked " + abbreviateHome(n.LinkPath, o.Home)
	case "fetched":
		if e.ViaCheckout != "" {
			s = fmt.Sprintf("%s, fetched %s@%s → %s, via checkout in %s", n.Kind, n.Repo, n.Ref, short(n.SHA), jobOf(e.Site))
		} else {
			s = n.Kind + ", fetched, " + refDesc(n)
		}
		if n.Cache != "" {
			s += ", cache " + n.Cache
		}
	case "skipped":
		if n.Kind == "docker-action" {
			s = "docker-action, skipped"
		} else {
			s = "skipped by config"
		}
		return paint(o.Color, yellow, "("+s+")")
	case "unresolved":
		s = "unresolved"
		if n.Error != "" && n.Error != "not fetched (--no-fetch)" {
			s += ": " + n.Error
		}
		return paint(o.Color, yellow, "("+s+")")
	case "failed":
		return paint(o.Color, red, "(failed: "+n.Error+")")
	}
	return paint(o.Color, dim, "("+s+")")
}

func refDesc(n *check.Node) string {
	switch n.RefKind {
	case "sha":
		return "sha " + short(n.SHA)
	case "default-branch":
		return "default → " + short(n.SHA)
	}
	return n.RefKind + " → " + short(n.SHA)
}

func jobOf(site string) string {
	if i := strings.Index(site, " / "); i >= 0 {
		return site[:i]
	}
	return site
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func abbreviateHome(p, home string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + filepath.ToSlash(p[len(home):])
	}
	return p
}

// Counts tallies nodes by source and fetched nodes by cache status.
type Counts struct {
	Nodes    int            `json:"nodes"`
	BySource map[string]int `json:"by_source"`
	Cache    map[string]int `json:"cache"`
}

// Count tallies a graph.
func Count(g *check.Graph) Counts {
	c := Counts{Nodes: len(g.Nodes), BySource: map[string]int{}, Cache: map[string]int{"hit": 0, "miss": 0, "stale": 0}}
	for _, n := range g.Nodes {
		c.BySource[n.Source]++
		if n.Source == "fetched" && n.Cache != "" {
			c.Cache[n.Cache]++
		}
	}
	return c
}

var sourceOrder = []string{"entry", "local", "linked", "fetched", "skipped", "unresolved", "failed"}

// Summary is the tree's last line (§4.1): nodes by source, in a fixed order.
func Summary(g *check.Graph) string {
	c := Count(g)
	var parts []string
	for _, s := range sourceOrder {
		n := c.BySource[s]
		if n == 0 {
			continue
		}
		part := fmt.Sprintf("%d %s", n, s)
		if s == "fetched" {
			// The same keys json always carries: hit, miss, stale; bypassed
			// only when it happened (review N5).
			extra := fmt.Sprintf("%s, %s, %d stale", plural(c.Cache["hit"], "cache hit"), plural(c.Cache["miss"], "miss", "misses"), c.Cache["stale"])
			if c.Cache["bypassed"] > 0 {
				extra += fmt.Sprintf(", %d bypassed", c.Cache["bypassed"])
			}
			part += " (" + extra + ")"
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("%s: %s", plural(c.Nodes, "node"), strings.Join(parts, ", "))
}

func plural(n int, unit string, pl ...string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	if len(pl) > 0 {
		return fmt.Sprintf("%d %s", n, pl[0])
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// Document is the json schema (§4.2).
type Document struct {
	Schema  int                    `json:"schema"`
	Entry   string                 `json:"entry"`
	Nodes   map[string]*check.Node `json:"nodes"`
	Edges   []check.Edge           `json:"edges"`
	Summary DocSummary             `json:"summary"`
}

// DocSummary is the json summary block.
type DocSummary struct {
	Nodes    int            `json:"nodes"`
	BySource map[string]int `json:"by_source"`
	Cache    map[string]int `json:"cache"`
	APICalls int64          `json:"api_calls"`
}

// Build assembles the json document.
func Build(g *check.Graph, apiCalls int64) Document {
	c := Count(g)
	d := Document{Schema: 1, Entry: g.Entry, Nodes: map[string]*check.Node{}, Edges: g.Edges,
		Summary: DocSummary{Nodes: c.Nodes, BySource: c.BySource, Cache: c.Cache, APICalls: apiCalls}}
	if d.Edges == nil {
		d.Edges = []check.Edge{}
	}
	for _, n := range g.Nodes {
		d.Nodes[n.ID] = n
	}
	return d
}

// JSON writes the json format.
func JSON(w io.Writer, g *check.Graph, apiCalls int64) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(Build(g, apiCalls))
}

// Dot writes Graphviz (§4.3).
func Dot(w io.Writer, g *check.Graph, cwd string) {
	fmt.Fprintln(w, "digraph jobsworth {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [fontname=\"Helvetica\"];")
	for _, n := range g.Nodes {
		shape := "ellipse"
		if n.Kind == "workflow" || n.Source == "entry" {
			shape = "box"
		}
		fmt.Fprintf(w, "  %s [label=%q, shape=%s, color=%s];\n", n.ID, nodeLabel(n, cwd), shape, dotColor(n))
	}
	for _, e := range g.Edges {
		fmt.Fprintf(w, "  %s -> %s [label=%q];\n", e.From, e.To, e.Site)
	}
	fmt.Fprintln(w, "}")
}

// Mermaid writes a flowchart (§4.4).
func Mermaid(w io.Writer, g *check.Graph, cwd string) {
	fmt.Fprintln(w, "flowchart LR")
	for _, n := range g.Nodes {
		label := mermaidQuote(nodeLabel(n, cwd))
		if n.Kind == "workflow" || n.Source == "entry" {
			fmt.Fprintf(w, "  %s[\"%s\"]\n", n.ID, label)
		} else {
			fmt.Fprintf(w, "  %s([\"%s\"])\n", n.ID, label)
		}
	}
	for _, e := range g.Edges {
		fmt.Fprintf(w, "  %s -->|\"%s\"| %s\n", e.From, mermaidQuote(e.Site), e.To)
	}
	for _, s := range sourceOrder {
		var ids []string
		for _, n := range g.Nodes {
			if n.Source == s {
				ids = append(ids, n.ID)
			}
		}
		if len(ids) > 0 && mermaidStyle(s) != "" {
			fmt.Fprintf(w, "  classDef %s %s\n  class %s %s\n", s, mermaidStyle(s), strings.Join(ids, ","), s)
		}
	}
}

// nodeLabel is "display\nsource [sha]" for dot and mermaid.
func nodeLabel(n *check.Node, cwd string) string {
	disp := n.Display
	if n.Source == "entry" {
		disp = check.RelOrAbs(disp, cwd)
	}
	second := n.Source
	if n.SHA != "" {
		second += " " + short(n.SHA)
	}
	return disp + "\n" + second
}

func dotColor(n *check.Node) string {
	switch n.Source {
	case "failed":
		return "red"
	case "skipped", "unresolved":
		return "grey"
	case "fetched":
		return "blue"
	case "linked":
		return "green"
	}
	return "black"
}

func mermaidStyle(source string) string {
	switch source {
	case "failed":
		return "stroke:#c00,color:#c00"
	case "skipped", "unresolved":
		return "stroke:#999,color:#999"
	case "fetched":
		return "stroke:#36c"
	case "linked":
		return "stroke:#390"
	}
	return ""
}

func mermaidQuote(s string) string {
	s = strings.ReplaceAll(s, "\"", "#quot;")
	return strings.ReplaceAll(s, "\n", "<br/>")
}

// Callers renders the upward tree of --callers (§5). The graph's entry is
// the target; edges point from caller to callee.
func Callers(w io.Writer, g *check.Graph, o TreeOptions) {
	target := check.NodeByID(g, g.Entry)
	if target == nil {
		fmt.Fprintln(w, "(empty graph)")
		return
	}
	fmt.Fprintf(w, "%s  %s\n", check.RelOrAbs(target.Path, o.Cwd), paint(o.Color, dim, "("+target.Kind+")"))
	seen := map[string]bool{target.ID: true}
	var walkUp func(n *check.Node, indent string)
	walkUp = func(n *check.Node, indent string) {
		for _, e := range callersOf(g, n.ID) {
			from := check.NodeByID(g, e.From)
			line := fmt.Sprintf("%s◄─ %s in %s", indent, e.Site, callerPath(from, o))
			if from.Repo != n.Repo || indent == "" {
				tag := from.Repo
				if from.Source == "linked" {
					if tag != "" {
						tag += ", "
					}
					tag += "linked"
				}
				if tag != "" {
					line += "  " + paint(o.Color, dim, "("+tag+")")
				}
			}
			if seen[from.ID] {
				fmt.Fprintf(w, "%s  %s\n", line, paint(o.Color, dim, "↑"))
				continue
			}
			fmt.Fprintln(w, line)
			seen[from.ID] = true
			walkUp(from, indent+"   ")
		}
	}
	walkUp(target, "")
	fmt.Fprintln(w)
	fmt.Fprintln(w, CallersSummary(g))
}

// callerPath is the caller's path: relative to cwd when under it, else ~.
func callerPath(n *check.Node, o TreeOptions) string {
	p := check.RelOrAbs(n.Path, o.Cwd)
	if filepath.IsAbs(p) {
		p = abbreviateHome(p, o.Home)
	}
	return p
}

// callersOf lists edges into id, in recorded order.
func callersOf(g *check.Graph, id string) []check.Edge {
	var out []check.Edge
	for _, e := range g.Edges {
		if e.To == id {
			out = append(out, e)
		}
	}
	return out
}

// CallersSummary is "N callers in R repos (D direct, T transitive)".
func CallersSummary(g *check.Graph) string {
	direct := map[string]bool{}
	for _, e := range g.Edges {
		if e.To == g.Entry {
			direct[e.From] = true
		}
	}
	callers := len(g.Nodes) - 1
	if callers <= 0 {
		return "0 callers"
	}
	repos := map[string]bool{}
	for _, n := range g.Nodes {
		if n.ID != g.Entry {
			repos[n.Repo+"|"+n.LinkPath] = true
		}
	}
	return fmt.Sprintf("%s in %s (%d direct, %d transitive)", plural(callers, "caller"), plural(len(repos), "repo"), len(direct), callers-len(direct))
}
