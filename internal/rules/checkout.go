package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/checkout"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// CheckoutContext is what the walker needs to know about a job's root
// checkout to resolve its ./ references (medium §3.1).
type CheckoutContext struct {
	// Repository is the literal with.repository of the root checkout, ""
	// when it is the file's own repo (or dynamic).
	Repository string
	// Ref is the literal with.ref, "" for the default branch.
	Ref string
	// Subdir is set when the reference goes through a subdirectory
	// checkout (`path: tools` providing `./tools/.github/actions/x`): the
	// prefix to strip before resolving (medium review 2 D7).
	Subdir string
}

// Checkouts maps a step index to the checkouts that step performs: one for
// a direct actions/checkout, one per profile entry for a wrapper, one
// Unknown entry for a callee check cannot see (checkout-inference §3).
type Checkouts map[int][]checkout.Step

// DirectCheckouts is the job's actions/checkout steps alone: what a caller
// without a walk (unit tests, explain on a file with no trace) can see.
func DirectCheckouts(f *workflow.File, job string) Checkouts {
	out := Checkouts{}
	for _, st := range workflow.StepsOf(f, job) {
		if st.Uses != "" && checkout.IsCheckoutAction(st.Uses) {
			out[st.Index] = []checkout.Step{checkout.Classify(st)}
		}
	}
	return out
}

// CheckCheckout implements medium §3.2 over one job. self is the file's own
// owner/repo ("" when unknown); root is the local repo root for existence
// checks ("" for fetched files); cos are the job's checkouts (nil: direct
// checkouts only). The returned context is nil when the job has no root
// checkout.
func CheckCheckout(f *workflow.File, job, self, root string, cos Checkouts) ([]finding.Finding, *CheckoutContext) {
	out, ctx, _ := CheckCheckoutSubdirs(f, job, self, root, cos)
	return out, ctx
}

// UnknownAfter is the first step from which the job's checkout state is
// unknown (checkout-inference §3.6), or -1.
func UnknownAfter(f *workflow.File, job string, cos Checkouts) (int, string) {
	for _, st := range workflow.StepsOf(f, job) {
		for _, cs := range cos[st.Index] {
			if cs.Unknown {
				return st.Index, cs.Reason
			}
		}
	}
	return -1, ""
}

// CheckCheckoutSubdirs is CheckCheckout returning also, per step index, the
// context of a subdirectory checkout that provides that step's local
// reference (spec medium §3.1 subdirCheckouts). Steps after an unknown
// checkout get an empty context: they resolve against the file's own
// repository and report nothing (§3.6).
func CheckCheckoutSubdirs(f *workflow.File, job, self, root string, cos Checkouts) ([]finding.Finding, *CheckoutContext, map[int]*CheckoutContext) {
	if job == "" {
		return nil, nil, nil
	}
	if cos == nil {
		cos = DirectCheckouts(f, job)
	}
	var out []finding.Finding
	var rootCheckout *checkout.Step
	// condRoots are conditional root checkouts after the current root: a
	// consumer may run exactly when one of them did, so each is a
	// candidate root for it (checkout-inference review 1 D1).
	var condRoots []checkout.Step
	subdirOnly := false
	unknown := false
	subdirs := map[string]checkout.Step{} // literal path: -> checkout
	provided := map[int]*CheckoutContext{}
	foreign := func(repo string) bool { return repo != "" && self != "" && !strings.EqualFold(repo, self) }
	for _, st := range workflow.StepsOf(f, job) {
		if entries := cos[st.Index]; len(entries) > 0 {
			refDyn := false
			for _, cs := range entries {
				if cs.Unknown {
					unknown = true
					continue
				}
				switch cs.Target {
				case checkout.Root:
					if cs.Conditional && rootCheckout != nil {
						condRoots = append(condRoots, cs) // a conditional checkout never displaces a known root (§3.3)
						break
					}
					c := cs
					rootCheckout = &c
					condRoots = nil
				case checkout.Dynamic:
					if rootCheckout == nil {
						c := cs
						rootCheckout = &c
					}
				case checkout.Subdir:
					if rootCheckout == nil {
						subdirOnly = true
					}
					subdirs[strings.Trim(cs.Path, "/")] = cs
				}
				refDyn = refDyn || cs.RefDyn
			}
			if refDyn {
				out = append(out, at("checkout-ref-unresolvable", st.UsesPos, fmt.Sprintf("checkout step %s uses an expression for ref:; nothing downstream can be verified against it", StepLabel(st))))
			}
			continue
		}
		if st.Uses == "" {
			continue
		}
		ref, err := uses.Parse(st.Uses)
		if err != nil || ref.Kind != uses.Local {
			continue
		}
		if unknown {
			provided[st.Index] = &CheckoutContext{} // own repository, nothing reported (§3.6)
			continue
		}
		if key, sub, ok := subdirProviding(subdirs, ref.Path); ok {
			// The checkout into `path: key` provides this reference: it is
			// loaded from that checkout's repository at the stripped path.
			stripped := strings.TrimPrefix(strings.TrimPrefix(ref.Path, key), "/")
			if sub.Sparse != nil && !checkout.SparseCovers(*sub.Sparse, stripped) {
				out = append(out, at("local-action-sparse-miss", st.UsesPos, fmt.Sprintf("step %s uses %s but the checkout into %s has a sparse-checkout list (%s) that does not cover it", StepLabel(st), st.Uses, key, strings.Join(sub.Sparse.Entries, ", "))))
			}
			c := &CheckoutContext{Ref: sub.Ref, Subdir: key}
			if foreign(sub.Repository) {
				c.Repository = sub.Repository
			}
			provided[st.Index] = c
			continue
		}
		if rootCheckout == nil {
			if subdirOnly {
				out = append(out, at("local-action-after-subdir-only", st.UsesPos, fmt.Sprintf("step %s uses local action %s but the only checkouts in job '%s' use path:; the workspace root is empty", StepLabel(st), st.Uses, job)))
			} else {
				out = append(out, at("local-action-no-checkout", st.UsesPos, fmt.Sprintf("step %s uses local action %s but job '%s' has no checkout before it", StepLabel(st), st.Uses, job)))
			}
			continue
		}
		// Candidate roots: the current root and every later conditional one.
		// The reference is fine when any candidate accepts it; otherwise the
		// candidate whose if: the consumer shares explains the miss, else
		// the unconditional root.
		candidates := append([]checkout.Step{*rootCheckout}, condRoots...)
		blame := *rootCheckout
		for _, c := range condRoots {
			if SameCondition(c.If, st.If.Value) {
				blame = c
				provided[st.Index] = contextOf(c, foreign) // via_checkout follows the matching conditional root
			}
		}
		accepted := false
		for _, c := range candidates {
			if !wrongRepo(c, self, root, ref.Path) && !sparseMiss(c, ref.Path) {
				accepted = true
				break
			}
		}
		if accepted {
			continue
		}
		if wrongRepo(blame, self, root, ref.Path) {
			out = append(out, at("local-action-wrong-repo", st.UsesPos, fmt.Sprintf("step %s uses %s, which will be loaded from %s (the root checkout), not from this repository", StepLabel(st), st.Uses, blame.Repository)))
		}
		if sparseMiss(blame, ref.Path) {
			out = append(out, at("local-action-sparse-miss", st.UsesPos, fmt.Sprintf("step %s uses %s but the root checkout's sparse-checkout list (%s) does not cover it", StepLabel(st), st.Uses, strings.Join(blame.Sparse.Entries, ", "))))
		}
	}
	if rootCheckout == nil {
		return out, nil, provided
	}
	ctx := &CheckoutContext{Ref: rootCheckout.Ref}
	if foreign(rootCheckout.Repository) {
		ctx.Repository = rootCheckout.Repository
	}
	return out, ctx, provided
}

// wrongRepo reports a root checkout of another repository that does not
// hold the action path (checked on disk when the repo is local).
func wrongRepo(c checkout.Step, self, root, refPath string) bool {
	if c.Repository == "" || self == "" || strings.EqualFold(c.Repository, self) {
		return false
	}
	if root != "" {
		p := filepath.Join(root, filepath.FromSlash(refPath))
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return false
		}
	}
	return true
}

// sparseMiss reports a sparse list that does not cover the action path.
func sparseMiss(c checkout.Step, refPath string) bool {
	return c.Sparse != nil && !checkout.SparseCovers(*c.Sparse, refPath)
}

// SameCondition compares two if: texts ignoring ${{ }} and spacing.
func SameCondition(a, b string) bool {
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "${{"), "}}"))
		return strings.Join(strings.Fields(s), " ")
	}
	return a != "" && norm(a) == norm(b)
}

// contextOf is the walker's context for a root checkout.
func contextOf(c checkout.Step, foreign func(string) bool) *CheckoutContext {
	ctx := &CheckoutContext{Ref: c.Ref}
	if foreign(c.Repository) {
		ctx.Repository = c.Repository
	}
	return ctx
}

// subdirProviding finds the subdirectory checkout whose path: is a prefix
// of a local reference path (longest wins).
func subdirProviding(subdirs map[string]checkout.Step, refPath string) (string, checkout.Step, bool) {
	best := ""
	for key := range subdirs {
		if key == "" {
			continue
		}
		if (refPath == key || strings.HasPrefix(refPath, key+"/")) && len(key) > len(best) {
			best = key
		}
	}
	if best == "" {
		return "", checkout.Step{}, false
	}
	return best, subdirs[best], true
}
