package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// CheckJobGraph implements §7.3: needs-unknown, needs-self, needs-cycle.
// Forward references are allowed.
func CheckJobGraph(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	exists := map[string]bool{}
	for _, j := range f.Jobs {
		exists[j.Name] = true
	}

	// edges only between existing, distinct jobs feed the cycle search;
	// unknown and self edges are reported directly.
	edges := map[string][]string{}
	for _, j := range f.Jobs {
		for _, n := range j.Needs {
			switch {
			case n.Name == j.Name:
				out = append(out, at("needs-self", n.Pos, fmt.Sprintf("job '%s' lists itself in needs", j.Name)))
			case !exists[n.Name]:
				out = append(out, at("needs-unknown", n.Pos, fmt.Sprintf("job '%s' needs unknown job '%s'", j.Name, n.Name)))
			default:
				edges[j.Name] = append(edges[j.Name], n.Name)
			}
		}
	}

	for _, cycle := range Cycles(f, edges) {
		first := workflow.JobByName(f, cycle[0])
		out = append(out, at("needs-cycle", first.Pos,
			fmt.Sprintf("needs form a cycle: %s", strings.Join(append(cycle, cycle[0]), " -> "))))
	}
	return out
}

// Cycles returns every strongly connected component of size > 1 in the needs
// graph (Tarjan), each as a job list in declaration order. Self-loops are
// not cycles here; needs-self covers them.
func Cycles(f *workflow.File, edges map[string][]string) [][]string {
	order := map[string]int{}
	for i, j := range f.Jobs {
		order[j.Name] = i
	}
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	next := 0
	var sccs [][]string

	var visit func(v string)
	visit = func(v string) {
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if _, seen := index[w]; !seen {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Slice(comp, func(a, b int) bool { return order[comp[a]] < order[comp[b]] })
				sccs = append(sccs, comp)
			}
		}
	}
	for _, j := range f.Jobs {
		if _, seen := index[j.Name]; !seen {
			visit(j.Name)
		}
	}
	sort.Slice(sccs, func(a, b int) bool { return order[sccs[a][0]] < order[sccs[b][0]] })
	return sccs
}

// TransitiveNeeds returns the set of jobs reachable from job through needs:,
// excluding unknown names.
func TransitiveNeeds(f *workflow.File, job string) map[string]bool {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		j := workflow.JobByName(f, name)
		if j == nil {
			return
		}
		for _, n := range j.Needs {
			if !seen[n.Name] {
				seen[n.Name] = true
				walk(n.Name)
			}
		}
	}
	walk(job)
	return seen
}
