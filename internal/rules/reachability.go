package rules

import (
	"fmt"
	"strings"

	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family reachability (advanced §8): consequences of §4 and the job graph.

// alwaysFalseIf reports whether a bare if: value is provably false.
func alwaysFalseIf(s workflow.Scalar) bool {
	if s.Value == "" {
		return false
	}
	t := strings.TrimSpace(s.Value)
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "${{"), "}}"))
	node, err := expr.Parse(t)
	if err != nil {
		return false
	}
	sat, decided := satisfiable(node, nil)
	return decided && !sat
}

// consumesOutputs reports whether job b references needs.a.outputs
// anywhere (if:, with:, env:, steps).
func consumesOutputs(f *workflow.File, b, a string) bool {
	needle := "needs." + a + ".outputs"
	for _, e := range f.Exprs {
		if e.Job == b && strings.Contains(e.Text, needle) {
			return true
		}
	}
	return false
}

// provablyTrueIf reports if: values that always hold.
func provablyTrueIf(s workflow.Scalar) bool {
	t := strings.TrimSpace(s.Value)
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "${{"), "}}"))
	return t == "" || t == "true" || t == "always()" || t == "success()"
}

// CheckReachability implements advanced §8 over one file.
func CheckReachability(f *workflow.File, scopes map[string]*Scope) []finding.Finding {
	var out []finding.Finding
	for _, sc := range scopes {
		for _, st := range sc.Steps {
			if st.ID == "" || !alwaysFalseIf(st.If) || !sc.Referenced(st.ID, "") {
				continue
			}
			out = append(out, at("step-unreachable", st.Pos, fmt.Sprintf("step '%s' never runs (its if: is always false) but its outputs are consumed; the consumers get empty values", st.ID)))
		}
	}
	if f.Kind != workflow.KindWorkflow {
		return out
	}
	for _, b := range f.Jobs {
		guarded := strings.Contains(b.If.Value, "always()") || strings.Contains(b.If.Value, "cancelled()")
		for _, n := range b.Needs {
			a := workflow.JobByName(f, n.Name)
			if a == nil || a.If.Value == "" || provablyTrueIf(a.If) || neverRuns(f, a) {
				continue
			}
			if guarded || strings.Contains(b.If.Value, "needs."+n.Name+".result") {
				continue
			}
			if consumesOutputs(f, b.Name, n.Name) {
				continue // B cannot usefully run without A's outputs; skipping is the intent (medium review 8 N14)
			}
			out = append(out, at("job-needs-skipped", n.Pos, fmt.Sprintf("job '%s' needs '%s', whose if: can be false; when '%s' is skipped this job is skipped silently. Add always() or !cancelled() with a needs.%s.result check if it should run anyway", b.Name, n.Name, n.Name, n.Name)))
		}
	}
	// output-behind-condition: a job output from a conditional step,
	// consumed without a result or outcome check.
	for _, a := range f.Jobs {
		sc := scopes[a.Name]
		if sc == nil {
			continue
		}
		for _, o := range a.Outputs {
			var condStep string
			for _, r := range workflow.StepRefs(o.Value) {
				if st, ok := sc.StepByID(r.ID); ok && st.If.Value != "" && !provablyTrueIf(st.If) {
					condStep = r.ID
				}
			}
			if condStep == "" {
				continue
			}
			for _, e := range f.Exprs {
				if e.Job == a.Name || !strings.Contains(e.Text, "needs."+a.Name+".outputs."+o.Name) {
					continue
				}
				consumer := workflow.JobByName(f, e.Job)
				checked := strings.Contains(e.Text, "needs."+a.Name+".result")
				if consumer != nil && strings.Contains(consumer.If.Value, "needs."+a.Name+".result") {
					checked = true
				}
				if e.Step >= 0 {
					steps := workflow.StepsOf(f, e.Job)
					if e.Step < len(steps) && strings.Contains(steps[e.Step].If.Value, "needs."+a.Name+".result") {
						checked = true
					}
				}
				if !checked {
					out = append(out, at("output-behind-condition", e.Pos, fmt.Sprintf("needs.%s.outputs.%s comes from step '%s', which has an if:; the value may be empty and nothing checks needs.%s.result", a.Name, o.Name, condStep, a.Name)))
					break
				}
			}
		}
	}
	return out
}
