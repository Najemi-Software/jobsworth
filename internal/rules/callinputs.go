package rules

import (
	"fmt"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// CheckCallInputs implements §7.2 for one reusable-workflow job: the
// caller's with: against the callee's on.workflow_call.inputs. calleeName is
// the callee's display name for the message label. Findings are attributed
// to the caller (File left empty for the walker to fill).
func CheckCallInputs(job workflow.Job, callee *workflow.File, calleeName string) []finding.Finding {
	if job.Uses == nil || callee == nil {
		return nil
	}
	label := fmt.Sprintf("job '%s' calling %s", job.Name, calleeName)
	var out []finding.Finding

	declared := map[string]workflow.Input{}
	for _, in := range workflow.WorkflowCallInputs(callee) {
		declared[in.Name] = in
	}

	supplied := map[string]bool{}
	for _, w := range job.With {
		supplied[w.Name] = true
		in, ok := declared[w.Name]
		if !ok {
			out = append(out, at("call-input-unknown", w.Value.Pos,
				fmt.Sprintf("%s: with.%s is supplied but the callee declares no input '%s'", label, w.Name, w.Name)))
			continue
		}
		if w.Value.IsExpr() || w.Value.Tag == "" {
			continue
		}
		if ok, want := LiteralFits(w.Value, in.Type, in.Options); !ok {
			out = append(out, at("call-input-type", w.Value.Pos,
				fmt.Sprintf("%s: with.%s is %q but the callee declares type %s (expected %s)", label, w.Name, w.Value.Value, in.Type, want)))
		}
	}

	for _, in := range workflow.WorkflowCallInputs(callee) {
		if in.Required && !in.HasDefault && !supplied[in.Name] {
			pos := job.Pos
			if job.Uses != nil {
				pos = job.Uses.Pos
			}
			out = append(out, at("call-input-missing", pos,
				fmt.Sprintf("%s: required input '%s' is not supplied", label, in.Name)))
		}
	}
	return out
}
