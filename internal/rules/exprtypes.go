package rules

import (
	"fmt"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// CheckExprTypes implements the expression column of medium §5.2 for a
// reusable-workflow call: a with: value whose inferred shape is a primitive
// that cannot fit the callee's declared type is call-input-type. Unknown
// shapes are skipped.
func CheckExprTypes(job workflow.Job, callee *workflow.File, calleeName string, shapes map[string]*shape.Shape) []finding.Finding {
	var out []finding.Finding
	declared := map[string]workflow.Input{}
	for _, in := range workflow.WorkflowCallInputs(callee) {
		declared[in.Name] = in
	}
	for _, w := range job.With {
		if !w.Value.IsExpr() {
			continue
		}
		in, ok := declared[w.Name]
		if !ok {
			continue
		}
		s := shapes[w.Name]
		if s == nil || s.Kind == shape.Any {
			continue
		}
		if msg := kindFits(s, in.Type); msg != "" {
			out = append(out, at("call-input-type", w.Value.Pos, fmt.Sprintf("job '%s' calling %s: with.%s is an expression of type %s but the callee declares type %s (%s)", job.Name, calleeName, w.Name, s.Kind, in.Type, msg)))
		}
	}
	return out
}

// CheckStepExprTypes is the step-level twin of CheckExprTypes for a uses:
// step whose action declares typed inputs.
func CheckStepExprTypes(sc *Scope, st workflow.Step, callee *workflow.File, calleeName string, shapes map[string]*shape.Shape) []finding.Finding {
	var out []finding.Finding
	if callee == nil {
		return nil
	}
	declared := map[string]workflow.Input{}
	for _, in := range callee.Inputs {
		declared[in.Name] = in
	}
	for _, w := range st.With {
		if !w.Value.IsExpr() {
			continue
		}
		in, ok := declared[w.Name]
		if !ok || in.Type == "" {
			continue
		}
		s := shapes[w.Name]
		// Action inputs are strings at runtime; a string-shaped expression
		// (github.run_number, an output) may well hold a number, so only a
		// definitely-typed value (boolean, number, JSON) can conflict.
		if s == nil || s.Kind == shape.Any || s.Kind == shape.String {
			continue
		}
		if msg := kindFits(s, in.Type); msg != "" {
			label := fmt.Sprintf("step %s using %s", StepLabel(st), calleeName)
			if sc.Job != "" {
				label = fmt.Sprintf("job '%s' %s", sc.Job, label)
			}
			out = append(out, at("step-input-type", w.Value.Pos, fmt.Sprintf("%s: with.%s is an expression of type %s but the input declares type %s (%s)", label, w.Name, s.Kind, in.Type, msg)))
		}
	}
	return out
}

// kindFits applies the §5.2 table to an inferred shape; "" means ok or
// unknown.
func kindFits(s *shape.Shape, typ string) string {
	isJSON := s.Kind == shape.Object || s.Kind == shape.Array || s.Kind == shape.UnknownObject || s.Kind == shape.UnknownArray
	switch typ {
	case "number":
		if s.Kind == shape.String || s.Kind == shape.Bool || isJSON {
			return "expected a number"
		}
	case "boolean":
		if s.Kind == shape.String || s.Kind == shape.Number || isJSON {
			return "expected true or false"
		}
	case "choice":
		if s.Kind == shape.Number || s.Kind == shape.Bool || isJSON {
			return "expected one of the options"
		}
	case "environment":
		if isJSON {
			return "expected an environment name"
		}
	}
	return ""
}
