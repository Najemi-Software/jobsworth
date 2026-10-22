// Package checkout classifies checkout steps and their targets (medium §3):
// which actions count as a checkout, where a `path:` lands, and whether a
// sparse-checkout list covers a local action path.
package checkout

import (
	"path"
	"strings"

	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// IsCheckoutAction reports whether a uses: value is actions/checkout at any
// ref: the one checkout known a priori (checkout-inference §2). Wrappers
// are recognised from their content (Profile).
func IsCheckoutAction(raw string) bool {
	ref, err := uses.Parse(raw)
	if err != nil || ref.Kind != uses.External {
		return false
	}
	base := strings.ToLower(ref.Owner + "/" + ref.Repo)
	if ref.Path != "" {
		base += "/" + strings.ToLower(ref.Path)
	}
	return base == "actions/checkout"
}

// Target is where a checkout lands.
type Target int

const (
	// Root: the workspace root.
	Root Target = iota
	// Subdir: a literal relative path.
	Subdir
	// Dynamic: path: contains an expression.
	Dynamic
)

// ClassifyPath interprets a with.path value (absent = "").
func ClassifyPath(p string) (Target, string) {
	s := strings.TrimSpace(p)
	if strings.Contains(s, "${{") {
		return Dynamic, s
	}
	clean := path.Clean(strings.ReplaceAll(s, "\\", "/"))
	if s == "" || clean == "." || clean == "/" {
		return Root, ""
	}
	return Subdir, strings.TrimPrefix(clean, "./")
}

// Sparse is a parsed sparse-checkout list.
type Sparse struct {
	// Entries are the literal lines; Dynamic is set when any contains an
	// expression or (in non-cone mode) a glob character.
	Entries []string
	Dynamic bool
	// Cone is true unless sparse-checkout-cone-mode: false.
	Cone bool
}

// ParseSparseCheckout reads the sparse-checkout value: a block scalar, a
// space/newline separated scalar, or a sequence rendered by the model as
// one scalar per line. cone is the sparse-checkout-cone-mode input.
func ParseSparseCheckout(value string, isExpr bool, cone string) Sparse {
	sp := Sparse{Cone: strings.TrimSpace(strings.ToLower(cone)) != "false"}
	if isExpr {
		sp.Dynamic = true
		return sp
	}
	for _, l := range strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == '\r' }) {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.Contains(l, "${{") {
			sp.Dynamic = true
		}
		if !sp.Cone && strings.ContainsAny(l, "*?[") {
			sp.Dynamic = true
		}
		sp.Entries = append(sp.Entries, l)
	}
	return sp
}

// SparseCovers reports whether the list covers actionPath (medium §3.2).
// Cone semantics: an entry covers itself and everything beneath; "." or ""
// covers all. In non-cone mode only exact and prefix matches count.
func SparseCovers(sp Sparse, actionPath string) bool {
	if sp.Dynamic || len(sp.Entries) == 0 {
		return true
	}
	target := strings.Trim(path.Clean(strings.ReplaceAll(actionPath, "\\", "/")), "/")
	for _, e := range sp.Entries {
		entry := strings.Trim(path.Clean(strings.TrimPrefix(e, "/")), "/")
		if entry == "" || entry == "." {
			return true
		}
		if target == entry || strings.HasPrefix(target, entry+"/") {
			return true
		}
		if !sp.Cone && strings.HasPrefix(target, entry) {
			return true
		}
	}
	return false
}

// Step is one checkout as seen by the rules: a direct actions/checkout
// step, or the effective checkout a wrapper performs at a step's position
// (checkout-inference §2 "Effective checkout").
type Step struct {
	Sparse *Sparse
	// Wiring is how the wrapper supplied each input (explain); nil for a
	// direct checkout.
	Wiring     map[string]Wire
	Path       string
	Repository string // literal with.repository, "" when absent or dynamic
	Ref        string // literal with.ref
	// RefExpr is the ref value as written when it is an expression.
	RefExpr string
	// PersistCredentials and Token are the literal values ("" when absent
	// or dynamic).
	PersistCredentials string
	Token              string
	// Uses is the caller step's uses: text.
	Uses string
	// Reason says why the shape is Unknown.
	Reason string
	// If is the caller step's if: text, so a consumer under the same
	// condition can be matched to it (review 1 D1).
	If string
	// Via is the wrapper chain down to actions/checkout (nil for a direct
	// checkout); ViaStep the 1-based step inside the innermost wrapper.
	Via     []string
	Index   int
	Target  Target
	ViaStep int
	RepoDyn bool
	RefDyn  bool
	// Conditional: the checkout runs under an if: (§3.3).
	Conditional bool
	// TrailingClean: nothing inside a wrapper reads the tree after the
	// checkout (always true for a direct checkout); the performance rules
	// look at the caller's later steps only when it is set (review 1 D2).
	TrailingClean bool
	// Unknown: a checkout of a shape check cannot see (§3.6); see Reason.
	// The job's checkout state is unknown from this step on.
	Unknown bool
}

// IsWrapper reports whether the step went through a wrapper.
func (s Step) IsWrapper() bool { return len(s.Via) > 0 }

// Classify reads a direct checkout step's with: block.
func Classify(st workflow.Step) Step {
	with := map[string]workflow.Scalar{}
	for _, w := range st.With {
		with[w.Name] = w.Value
	}
	cs := classify(st.Index, with)
	cs.Uses = st.Uses
	cs.Conditional = st.If.Value != "" && !alwaysTrue(st.If.Value)
	cs.If = st.If.Value
	cs.TrailingClean = true
	return cs
}

// classify interprets a (possibly synthesised) with: map.
func classify(index int, with map[string]workflow.Scalar) Step {
	cs := Step{Index: index}
	cs.Target, cs.Path = ClassifyPath(with["path"].Value)
	if r, ok := with["repository"]; ok {
		if r.IsExpr() {
			cs.RepoDyn = true
		} else {
			cs.Repository = strings.TrimSpace(r.Value)
		}
	}
	if r, ok := with["ref"]; ok {
		if r.IsExpr() {
			cs.RefDyn = true
			cs.RefExpr = r.Value
		} else {
			cs.Ref = strings.TrimSpace(r.Value)
		}
	}
	if v, ok := with["persist-credentials"]; ok && !v.IsExpr() {
		cs.PersistCredentials = strings.TrimSpace(v.Value)
	}
	if v, ok := with["token"]; ok && !v.IsExpr() {
		cs.Token = strings.TrimSpace(v.Value)
	}
	if s, ok := with["sparse-checkout"]; ok {
		sp := ParseSparseCheckout(s.Value, s.IsExpr(), with["sparse-checkout-cone-mode"].Value)
		cs.Sparse = &sp
	}
	return cs
}
