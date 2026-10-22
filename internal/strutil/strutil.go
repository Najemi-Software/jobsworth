// Package strutil holds small string helpers shared across packages.
package strutil

import (
	"sort"
	"strings"
)

// Levenshtein is the edit distance between a and b, case-insensitive.
func Levenshtein(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// Nearest returns up to n candidates closest to s by edit distance, ties
// broken by candidate order.
func Nearest(s string, candidates []string, n int) []string {
	type cand struct {
		s    string
		dist int
	}
	cs := make([]cand, 0, len(candidates))
	for _, c := range candidates {
		cs = append(cs, cand{c, Levenshtein(s, c)})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].dist < cs[j].dist })
	var out []string
	for i := 0; i < len(cs) && i < n; i++ {
		out = append(out, cs[i].s)
	}
	return out
}
