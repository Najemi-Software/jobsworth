package explain

import (
	"fmt"
	"strings"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// siteContext is the context block for the advanced-spec families
// (triggers, security, conditions, expressions, scripts, reachability,
// performance, and structure rules on jobs and steps): where the finding
// sits, the expression or block it is about, and the facts the family
// reads (triggers, permissions, runs-on, needs).
func siteContext(kind string, f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: kind, Data: map[string]any{}}
	fl := ft.File
	if e, ok := exprAt(ft, f.Line); ok {
		c.Lines = append(c.Lines, fmt.Sprintf("expression     %s   at :%d  (%s)", truncateText(e.Text, 70), f.Line, site(ft, e)))
		c.Data["expression"], c.Data["site"] = e.Text, site(ft, e)
	}
	if job, st, ok := stepAtLine(ft, f.Line); ok {
		label := "step #" + fmt.Sprint(st.Index+1)
		if st.ID != "" {
			label = "step '" + st.ID + "'"
		}
		where := label
		if job != "" {
			where = "job '" + job + "' / " + label
		}
		what := "run:"
		if st.Uses != "" {
			what = "uses: " + st.Uses
		}
		c.Lines = append(c.Lines, fmt.Sprintf("step           %-36s at :%d   %s", where, st.Pos.Line, what))
		c.Data["step"] = map[string]any{"job": job, "index": st.Index, "id": st.ID, "line": st.Pos.Line, "uses": st.Uses}
	} else if j := jobAtLine(fl, f.Line); j != nil {
		c.Lines = append(c.Lines, fmt.Sprintf("job            %-36s at :%d", "'"+j.Name+"'", j.Pos.Line))
		c.Data["job"] = j.Name
		if j.RunsOn != nil {
			var labels []string
			for _, l := range j.RunsOn.Labels {
				labels = append(labels, l.Value)
			}
			c.Lines = append(c.Lines, "runs-on        "+listOrNone(labels))
			c.Data["runs_on"] = labels
		}
		if len(j.Needs) > 0 {
			var needs []string
			for _, n := range j.Needs {
				needs = append(needs, n.Name)
			}
			c.Lines = append(c.Lines, "needs          "+strings.Join(needs, ", "))
			c.Data["needs"] = needs
		}
		if j.If.Value != "" {
			c.Lines = append(c.Lines, "if             "+truncateText(j.If.Value, 70))
		}
	}
	if fl.Kind == workflow.KindWorkflow {
		c.Lines = append(c.Lines, "triggers       "+listOrNone(fl.Events))
		c.Data["triggers"] = fl.Events
		if fl.Permissions != nil {
			c.Lines = append(c.Lines, "permissions    "+permissionsText(fl.Permissions)+" (workflow)")
		}
	}
	c.Conclusion = f.Message
	return c
}

func permissionsText(p *workflow.Permissions) string {
	switch p.Form {
	case "map":
		var parts []string
		for _, s := range p.Scopes {
			parts = append(parts, s.Name+": "+s.Level)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case "empty":
		return "{}"
	}
	return p.Form
}

// stepAtLine finds the step whose span contains the line.
func stepAtLine(ft *check.FileTrace, line int) (string, workflow.Step, bool) {
	scopes := []string{""}
	if ft.File.Kind == workflow.KindWorkflow {
		scopes = scopes[:0]
		for _, j := range ft.File.Jobs {
			scopes = append(scopes, j.Name)
		}
	}
	for _, job := range scopes {
		steps := workflow.StepsOf(ft.File, job)
		for i, st := range steps {
			end := 1 << 30
			if i+1 < len(steps) {
				end = steps[i+1].Pos.Line
			}
			if line >= st.Pos.Line && line < end {
				return job, st, true
			}
		}
	}
	return "", workflow.Step{}, false
}

// jobAtLine finds the job whose span contains the line.
func jobAtLine(f *workflow.File, line int) *workflow.Job {
	var best *workflow.Job
	for i := range f.Jobs {
		if f.Jobs[i].Pos.Line <= line && (best == nil || f.Jobs[i].Pos.Line > best.Pos.Line) {
			best = &f.Jobs[i]
		}
	}
	return best
}
