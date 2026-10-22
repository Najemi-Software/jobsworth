package check

import (
	"os"
	"path/filepath"

	"github.com/najemi-software/jobsworth/internal/checkout"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// checkoutsFor computes a scope's effective checkouts (checkout-inference
// §3.1): direct actions/checkout steps, wrapper profiles applied to their
// call sites, and Unknown entries for callees check cannot see (§3.6).
// results are the phase-1 (external) follow results for the file.
func (l *loader) checkoutsFor(cur *loaded, job string, results []loadResult) rules.Checkouts {
	cos := rules.Checkouts{}
	steps := workflow.StepsOf(cur.file, job)
	for i, u := range cur.file.Uses {
		if u.Job != job || u.JobLevel {
			continue
		}
		idx, ok := stepIndexOfUses(cur.file, u)
		if !ok || idx >= len(steps) {
			continue
		}
		st := steps[idx]
		if checkout.IsCheckoutAction(u.Raw) {
			cos[idx] = []checkout.Step{checkout.Classify(st)}
			continue
		}
		ref, err := uses.Parse(u.Raw)
		if err != nil {
			continue
		}
		var f *workflow.File
		var id string
		var src source
		switch ref.Kind {
		case uses.Docker:
			continue
		case uses.Dynamic:
			cos[idx] = []checkout.Step{unknownCheckout(st, "dynamic reference")}
			continue
		case uses.Local:
			// A local wrapper is profiled from the file at its own path; the
			// reference itself is followed in phase 2 (§6 "local action").
			f, id = l.localActionFile(cur, ref)
			src = cur.src
		default:
			r := results[i]
			if r.callee == nil {
				if r.how == "docker" || r.how == "skipped" {
					continue // trusted by config: an ordinary step
				}
				cos[idx] = []checkout.Step{unknownCheckout(st, notLoadedReason(r))}
				continue
			}
			f, id, src = r.callee.file, identity(r.callee.src), r.callee.src
		}
		if f == nil || f.Kind != workflow.KindCompositeAction {
			continue
		}
		entries := l.profileOf(id, f, src)
		if len(entries) == 0 {
			continue
		}
		cos[idx] = checkout.EffectiveAll(entries, st)
	}
	return cos
}

func unknownCheckout(st workflow.Step, reason string) checkout.Step {
	return checkout.Step{Index: st.Index, Uses: st.Uses, Unknown: true, Reason: reason}
}

// notLoadedReason names why a reference has no callee.
func notLoadedReason(r loadResult) string {
	switch {
	case r.err != "":
		return r.err
	case len(r.callerFindings) > 0:
		return r.callerFindings[0].Message
	case r.how != "":
		return r.how
	}
	return "not loaded"
}

// profileOf memoises a wrapper's profile by identity (§3.5). Nested
// wrappers are loaded through the walker's own follow, so the cache and
// links apply.
func (l *loader) profileOf(id string, f *workflow.File, src source) []checkout.Entry {
	if v, ok := l.parsed.profiles.Load(id); ok {
		return v.([]checkout.Entry)
	}
	resolve := func(raw string) (*workflow.File, string, bool) {
		r := l.follow(src, workflow.Uses{Raw: raw})
		if r.callee == nil {
			return nil, "", false
		}
		return r.callee.file, identity(r.callee.src), true
	}
	clean := func(st workflow.Step, callee *workflow.File) bool {
		return rules.CleanStep(st, callee, l.cfg.PerformanceFSClean)
	}
	entries := checkout.ProfileWith(f, id, resolve, clean)
	v, _ := l.parsed.profiles.LoadOrStore(id, entries)
	return v.([]checkout.Entry)
}

// localActionFile reads a local action's file for profiling without
// resolving it through the job's checkout state (a local wrapper checks
// out this repository, or is already reported as unreachable).
func (l *loader) localActionFile(cur *loaded, ref uses.Ref) (*workflow.File, string) {
	if !cur.src.local || cur.src.root == "" {
		return nil, ""
	}
	for _, name := range []string{"action.yml", "action.yaml"} {
		p := filepath.Join(cur.src.root, filepath.FromSlash(ref.Path), name)
		if v, ok := l.parsed.localFiles.Load(p); ok {
			return v.(*workflow.File), "local:" + p
		}
		data, err := l.read(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, ""
		}
		f, err := workflow.Parse(data)
		if err != nil {
			f = nil
		}
		l.parsed.localFiles.Store(p, f)
		return f, "local:" + p
	}
	return nil, ""
}
