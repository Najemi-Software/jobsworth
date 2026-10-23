package rules

import (
	"fmt"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// JobOutputs describes what one job declares, as seen by §7.4. Known is false
// when the job is a uses: job whose callee could not be loaded, in which case
// nothing about its outputs is reported.
type JobOutputs struct {
	Known bool
	Names []string
}

// CheckJobOutputs implements §7.4. calleeOutputs supplies the declared
// outputs of each uses: job (keyed by job name); a job absent from it is
// treated as unknown. Normal jobs are read from the file directly.
func CheckJobOutputs(f *workflow.File, calleeOutputs map[string]JobOutputs) []finding.Finding {
	var out []finding.Finding

	declared := map[string]JobOutputs{}
	for _, j := range f.Jobs {
		if j.Uses != nil {
			declared[j.Name] = calleeOutputs[j.Name]
			continue
		}
		jo := JobOutputs{Known: true}
		for _, o := range j.Outputs {
			jo.Names = append(jo.Names, o.Name)
		}
		declared[j.Name] = jo
	}
	has := func(job, output string) bool {
		for _, n := range declared[job].Names {
			if n == output {
				return true
			}
		}
		return false
	}

	// used[job][output]: consumed by another job via needs, or lifted into
	// on.workflow_call.outputs.
	used := map[string]map[string]bool{}
	mark := func(job, output string) {
		if used[job] == nil {
			used[job] = map[string]bool{}
		}
		used[job][output] = true
	}

	closures := map[string]map[string]bool{}
	for _, e := range f.Exprs {
		for _, r := range workflow.NeedsOutputRefs(e.Text) {
			mark(r.Job, r.Output)
			d, exists := declared[r.Job]
			if !exists {
				out = append(out, at("needs-output-job-unknown", e.Pos,
					fmt.Sprintf("needs.%s.outputs.%s references job '%s', which does not exist", r.Job, r.Output, r.Job)))
				continue
			}
			if e.Job != "" {
				cl, ok := closures[e.Job]
				if !ok {
					cl = TransitiveNeeds(f, e.Job)
					closures[e.Job] = cl
				}
				if !cl[r.Job] {
					out = append(out, at("needs-output-not-needed", e.Pos,
						fmt.Sprintf("job '%s' references needs.%s.outputs.%s, but '%s' is not in its needs", e.Job, r.Job, r.Output, r.Job)))
					continue
				}
			}
			if d.Known && !has(r.Job, r.Output) {
				out = append(out, at("needs-output-undeclared", e.Pos,
					fmt.Sprintf("job '%s' references needs.%s.outputs.%s, but job '%s' declares no output '%s'", e.Job, r.Job, r.Output, r.Job, r.Output)))
			}
		}
	}

	for _, o := range f.Outputs {
		for _, r := range workflow.JobsOutputRefs(o.Value) {
			mark(r.Job, r.Output)
			d, exists := declared[r.Job]
			if !exists {
				out = append(out, at("call-output-job-unknown", o.Pos,
					fmt.Sprintf("workflow output '%s' references jobs.%s.outputs.%s, but job '%s' does not exist", o.Name, r.Job, r.Output, r.Job)))
				continue
			}
			if d.Known && !has(r.Job, r.Output) {
				out = append(out, at("call-output-undeclared", o.Pos,
					fmt.Sprintf("workflow output '%s' references jobs.%s.outputs.%s, but job '%s' declares no output '%s'", o.Name, r.Job, r.Output, r.Job, r.Output)))
			}
		}
	}

	for _, j := range f.Jobs {
		if j.Uses != nil {
			continue
		}
		for _, o := range j.Outputs {
			if !used[j.Name][o.Name] {
				out = append(out, at("output-unused", o.Pos,
					fmt.Sprintf("job '%s' declares output '%s' but nothing references it", j.Name, o.Name)))
			}
		}
	}
	return out
}
