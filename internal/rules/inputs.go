package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// CheckInputs implements §7.1: input-undeclared, input-unused and
// input-default-type over one file's own declarations and expressions.
func CheckInputs(f *workflow.File) []finding.Finding {
	var out []finding.Finding

	declared := map[string]workflow.Input{}
	for _, in := range f.Inputs {
		if _, dup := declared[in.Name]; !dup {
			declared[in.Name] = in
		}
	}

	// A workflow with neither workflow_call nor workflow_dispatch has no
	// inputs context at all (advanced §2.3 input-no-trigger).
	noTrigger := f.Kind == workflow.KindWorkflow && !f.HasWorkflowCall && !f.HasWorkflowDispatch
	referenced := map[string]bool{}
	for _, e := range f.Exprs {
		for _, name := range workflow.InputRefs(e.Text) {
			referenced[name] = true
			if _, ok := declared[name]; !ok {
				if noTrigger {
					out = append(out, at("input-no-trigger", e.Pos,
						fmt.Sprintf("inputs.%s is referenced but the workflow has neither workflow_call nor workflow_dispatch; the inputs context is always empty", name)))
					continue
				}
				out = append(out, at("input-undeclared", e.Pos,
					fmt.Sprintf("inputs.%s is referenced but input '%s' is not declared", name, name)))
			}
		}
	}

	// input-unused: skipped for non-composite actions (their implementation
	// is never inspected) and for dispatch-only workflows (§7.1 note).
	unusedApplies := f.Kind != workflow.KindOtherAction
	dispatchOnly := f.Kind == workflow.KindWorkflow && f.HasWorkflowDispatch && !f.HasWorkflowCall
	for _, in := range f.Inputs {
		if unusedApplies && !referenced[in.Name] && !(dispatchOnly && in.Trigger == "workflow_dispatch") {
			out = append(out, at("input-unused", in.Pos,
				fmt.Sprintf("input '%s' is declared but never referenced", in.Name)))
		}
		if msg := defaultTypeProblem(in); msg != "" {
			out = append(out, at("input-default-type", in.Default.Pos, msg))
		}
	}
	return out
}

// defaultTypeProblem returns a message when a declared default is not
// representable in the declared type, else "".
func defaultTypeProblem(in workflow.Input) string {
	if !in.HasDefault || in.Default.IsExpr() || in.Type == "choice" {
		return "" // choice defaults are choice-default-not-option (§5.3)
	}
	ok, want := LiteralFits(in.Default, in.Type, in.Options)
	if ok {
		return ""
	}
	return fmt.Sprintf("input '%s' has type %s but its default %q is not %s", in.Name, in.Type, in.Default.Value, want)
}

// LiteralFits reports whether a YAML scalar literal is representable in the
// GitHub input type. The second result describes what was expected, for
// messages. Unknown or empty types accept anything.
func LiteralFits(s workflow.Scalar, typ string, options []string) (bool, string) {
	switch typ {
	case "number":
		if s.Tag == "!!int" || s.Tag == "!!float" {
			return true, ""
		}
		if _, err := strconv.ParseFloat(strings.TrimSpace(s.Value), 64); err == nil && s.Tag != "!!str" {
			return true, ""
		}
		return false, "a number"
	case "boolean":
		if s.Tag == "!!bool" && (s.Value == "true" || s.Value == "false") {
			return true, ""
		}
		return false, "true or false"
	case "choice":
		for _, o := range options {
			if o == s.Value {
				return true, ""
			}
		}
		return false, "one of the declared options (" + strings.Join(options, ", ") + ")"
	}
	return true, ""
}
