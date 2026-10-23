package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

var stepIDRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// CheckSteps implements medium §0.2 (composite-shell-missing, bash-syntax)
// and §1.1–1.3, §1.5 over one scope.
func CheckSteps(sc *Scope) []finding.Finding {
	var out []finding.Finding
	isAction := sc.Job == ""

	// §1.1 ids.
	seen := map[string]workflow.Step{}
	for _, st := range sc.Steps {
		if st.ID == "" {
			continue
		}
		if !stepIDRe.MatchString(st.ID) {
			out = append(out, at("step-id-invalid", st.IDPos, fmt.Sprintf("step id %q must match [A-Za-z_][A-Za-z0-9_-]*", st.ID)))
		}
		if prev, dup := seen[st.ID]; dup {
			out = append(out, at("step-id-duplicate", st.IDPos, fmt.Sprintf("step id %q is already used by step %s", st.ID, StepLabel(prev))))
			continue
		}
		seen[st.ID] = st
	}

	// §0.2 shells and parse errors.
	for _, st := range sc.Steps {
		if !st.HasRun {
			continue
		}
		if isAction && sc.Shell[st.Index] == "" {
			out = append(out, at("composite-shell-missing", st.RunPos, fmt.Sprintf("composite step %s has run: but no shell:; every composite run step must set one", StepLabel(st))))
		}
		if a := sc.Bash[st.Index]; a != nil && a.ParseErr != nil {
			out = append(out, at("bash-syntax", st.RunPos, fmt.Sprintf("step %s: script does not parse as %s: %s", StepLabel(st), sc.Shell[st.Index], a.ParseErr.Error())))
		}
	}

	// §1.1 unknown and forward references, §1.2 consumer side, §1.5.
	referencedKeys := map[string]map[string]bool{}
	referencedIDs := map[string]bool{}
	for _, r := range sc.Refs {
		referencedIDs[r.ID] = true
		st, ok := sc.StepByID(r.ID)
		if !ok {
			ids := sc.IDs()
			declared := "none"
			if len(ids) > 0 {
				declared = strings.Join(ids, ", ")
			}
			out = append(out, at("step-id-unknown", r.Expr.Pos, fmt.Sprintf("steps.%s is referenced but no step has id %q (declared ids: %s)", r.ID, r.ID, declared)))
			continue
		}
		if r.Field == "outputs" {
			if referencedKeys[r.ID] == nil {
				referencedKeys[r.ID] = map[string]bool{}
			}
			referencedKeys[r.ID][r.Output] = true
		}
		if r.Expr.Step >= 0 && !r.Expr.InOutputs && st.Index >= r.Expr.Step {
			which := "later"
			if st.Index == r.Expr.Step {
				which = "itself"
			}
			out = append(out, at("step-forward-ref", r.Expr.Pos, fmt.Sprintf("steps.%s is referenced by step %s but declared %s; the value is always empty", r.ID, StepLabel(sc.Steps[r.Expr.Step]), which)))
			continue
		}
		if r.Field != "outputs" {
			continue
		}
		rule := func(id string) string {
			if isAction && r.Expr.InOutputs {
				return "action-output-unlifted"
			}
			return id
		}
		switch {
		case st.HasRun && st.Uses == "":
			a := sc.Bash[st.Index]
			if a == nil || a.ParseErr != nil {
				continue // non-bash or unparsed: skipped
			}
			ts := a.Target(bash.Output)
			w, written := ts.Writes[r.Output]
			if !written {
				if ts.Open {
					continue
				}
				seen := fmt.Sprintf("writes: %s", keysOrNone(ts.Keys()))
				if len(ts.Keys()) == 0 {
					seen = fmt.Sprintf("no $GITHUB_OUTPUT writes in the %d statement%s analysed", len(a.Statements), map[bool]string{true: "", false: "s"}[len(a.Statements) == 1])
				}
				out = append(out, at(rule("step-output-unwritten"), r.Expr.Pos, fmt.Sprintf("steps.%s.outputs.%s is referenced but step %s never writes %q to $GITHUB_OUTPUT (%s)", r.ID, r.Output, StepLabel(st), r.Output, seen)))
				continue
			}
			if !w.Definite {
				out = append(out, at("step-output-conditional", r.Expr.Pos, fmt.Sprintf("steps.%s.outputs.%s is only written on some paths of step %s; the reference may be empty", r.ID, r.Output, StepLabel(st))))
			}
		case st.Uses != "":
			callee := sc.Callees[st.Index]
			if callee == nil || callee.Kind != workflow.KindCompositeAction {
				// JS and Docker actions set outputs at runtime (core.setOutput);
				// their declared list is a lower bound (medium review D2).
				continue
			}
			declared := false
			var names []string
			for _, o := range callee.Outputs {
				names = append(names, o.Name)
				if o.Name == r.Output {
					declared = true
				}
			}
			if !declared {
				out = append(out, at(rule("step-output-action-undeclared"), r.Expr.Pos, fmt.Sprintf("steps.%s.outputs.%s is referenced but %s declares no output %q (declared: %s)", r.ID, r.Output, sc.CalleeNames[st.Index], r.Output, keysOrNone(names))))
			}
		default:
			out = append(out, at("step-output-not-run", r.Expr.Pos, fmt.Sprintf("steps.%s.outputs.%s is referenced but step %s has neither run: nor uses:", r.ID, r.Output, StepLabel(st))))
		}
	}

	// §1.3 producer side.
	for _, st := range sc.Steps {
		if st.ID == "" {
			continue
		}
		if !referencedIDs[st.ID] {
			out = append(out, at("step-id-unreferenced", st.IDPos, fmt.Sprintf("step id %q is never referenced", st.ID)))
		}
		if a := sc.Bash[st.Index]; a != nil && a.ParseErr == nil && st.HasRun && st.Uses == "" {
			ts := a.Target(bash.Output)
			if ts.Open {
				continue
			}
			for _, k := range ts.Keys() {
				if !referencedKeys[st.ID][k] {
					out = append(out, at("step-output-unused", st.RunPos, fmt.Sprintf("step %s writes output %q (script line %d) but nothing references steps.%s.outputs.%s", StepLabel(st), k, ts.Writes[k].Line, st.ID, k)))
				}
			}
		}
	}
	return out
}

func keysOrNone(keys []string) string {
	if len(keys) == 0 {
		return "none"
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// CheckStepInputs implements §1.4 for one uses: step whose callee is known
// (nil callee: only the structural rules run).
func CheckStepInputs(sc *Scope, st workflow.Step, callee *workflow.File, calleeName string) []finding.Finding {
	var out []finding.Finding
	if st.HasRun && st.Uses != "" {
		out = append(out, at("step-run-and-uses", st.UsesPos, fmt.Sprintf("step %s has both run: and uses:", StepLabel(st))))
		return out
	}
	if st.HasRun && st.HasWith {
		out = append(out, at("step-with-on-run", st.WithPos, fmt.Sprintf("step %s is a run: step; its with: block is ignored", StepLabel(st))))
		return out
	}
	if st.Uses == "" || callee == nil {
		return out
	}
	label := fmt.Sprintf("step %s using %s", StepLabel(st), calleeName)
	if sc.Job != "" {
		label = fmt.Sprintf("job '%s' %s", sc.Job, label)
	}
	declared := map[string]workflow.Input{}
	for _, in := range callee.Inputs {
		declared[in.Name] = in
	}
	supplied := map[string]bool{}
	for _, w := range st.With {
		supplied[w.Name] = true
		in, ok := declared[w.Name]
		if !ok {
			out = append(out, at("step-input-unknown", w.Value.Pos, fmt.Sprintf("%s: with.%s is supplied but the action declares no input %q", label, w.Name, w.Name)))
			continue
		}
		if in.Type == "" || w.Value.IsExpr() || w.Value.Tag == "" {
			continue
		}
		if ok, want := LiteralFits(w.Value, in.Type, in.Options); !ok {
			rule := "step-input-type"
			if in.Type == "choice" {
				rule = "step-input-choice"
			}
			out = append(out, at(rule, w.Value.Pos, fmt.Sprintf("%s: with.%s is %q but the input declares type %s (expected %s)", label, w.Name, w.Value.Value, in.Type, want)))
		}
	}
	for _, in := range callee.Inputs {
		if in.Required && !in.HasDefault && !supplied[in.Name] {
			out = append(out, at("step-input-missing", st.UsesPos, fmt.Sprintf("%s: required input %q is not supplied", label, in.Name)))
		}
	}
	return out
}
