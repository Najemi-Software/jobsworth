package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/expr"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family structure (advanced §3): runner labels, jobs and steps that are
// valid YAML but useless, and file metadata.

// RunnerLabels are the GitHub-hosted labels (advanced §3.1), refreshed
// per release; Retired maps a label to the date GitHub removed it.
var RunnerLabels = []string{
	"ubuntu-latest", "ubuntu-24.04", "ubuntu-22.04", "ubuntu-24.04-arm", "ubuntu-22.04-arm",
	"windows-latest", "windows-2025", "windows-2022", "windows-11-arm",
	"macos-latest", "macos-15", "macos-14", "macos-13",
	"macos-latest-large", "macos-15-large", "macos-14-large", "macos-13-large",
	"macos-latest-xlarge", "macos-15-xlarge", "macos-14-xlarge", "macos-13-xlarge",
}

var RetiredLabels = map[string]string{
	"ubuntu-20.04": "2025-04-15",
	"ubuntu-18.04": "2022-12-01",
	"windows-2019": "2025-06-30",
	"windows-2016": "2022-06-30",
	"macos-12":     "2024-12-03",
	"macos-11":     "2024-06-28",
	"macos-10.15":  "2022-08-30",
}

// StructureInputs is what CheckStructure knows about the repo and config.
type StructureInputs struct {
	RunnerLabels     []string
	SelfHostedLabels []string
	// Workflows lists the repo's workflows as (name, path) for duplicate
	// names; Path is this file's path. Nil when the repo is not local.
	Workflows []NamedWorkflow
	Path      string
}

// NamedWorkflow is a workflow's name: and its absolute path.
type NamedWorkflow struct {
	Name, Path string
}

// CheckStructure implements advanced §3.1–§3.3 (the parts that need no
// step scopes) over one file.
func CheckStructure(f *workflow.File, in StructureInputs) []finding.Finding {
	var out []finding.Finding
	switch f.Kind {
	case workflow.KindWorkflow:
		out = append(out, checkWorkflowMeta(f, in)...)
		for _, j := range f.Jobs {
			out = append(out, checkJob(f, j, in)...)
		}
		out = append(out, checkOrphanOutputs(f)...)
		out = append(out, checkUnreachable(f)...)
	case workflow.KindCompositeAction, workflow.KindOtherAction:
		if f.Name.Value == "" {
			out = append(out, at("action-name-missing", workflow.Pos{Line: 1, Column: 1}, "action has no name:; it is required for the marketplace and is the action's documentation surface"))
		}
		if f.Description.Value == "" {
			out = append(out, at("action-description-missing", workflow.Pos{Line: 1, Column: 1}, "action has no description:; it is required for the marketplace and is the action's documentation surface"))
		}
		out = append(out, checkStepsShape(f, "", f.Steps)...)
	}
	for _, in := range f.Inputs {
		if !in.HasDescription && (f.Kind != workflow.KindWorkflow || in.Trigger == "workflow_call") {
			out = append(out, at("input-description-missing", in.Pos, fmt.Sprintf("input '%s' has no description:", in.Name)))
		}
	}
	return out
}

func checkWorkflowMeta(f *workflow.File, in StructureInputs) []finding.Finding {
	var out []finding.Finding
	if f.Name.Value == "" {
		out = append(out, at("workflow-name-missing", workflow.Pos{Line: 1, Column: 1}, "workflow has no name:; the Actions tab shows the file path instead"))
	} else {
		if f.Name.IsExpr() {
			out = append(out, at("name-expression", f.Name.Pos, "workflow name: contains ${{ }}, which is not evaluated there; the Actions tab shows it verbatim (run-name: does evaluate expressions)"))
		}
		var earlier []string
		for _, w := range in.Workflows {
			if w.Name == f.Name.Value && w.Path != in.Path && w.Path < in.Path {
				earlier = append(earlier, w.Path)
			}
		}
		if len(earlier) > 0 {
			sort.Strings(earlier)
			out = append(out, at("workflow-name-duplicate", f.Name.Pos, fmt.Sprintf("workflow name %q is also used by %s; the Actions tab cannot tell them apart", f.Name.Value, shortPath(earlier[0]))))
		}
	}
	// jobs.<id>.name does evaluate expressions (github, needs, strategy,
	// matrix, vars, inputs); only the workflow-level name: is verbatim
	// (medium review 7 D14).
	return out
}

func shortPath(p string) string {
	if i := strings.Index(p, ".github/"); i >= 0 {
		return p[i:]
	}
	return p
}

// checkJob is runs-on-*, job-empty-steps, job-no-steps, job-timeout-missing
// and the step shape rules over one job.
func checkJob(f *workflow.File, j workflow.Job, in StructureInputs) []finding.Finding {
	var out []finding.Finding
	if j.Uses != nil {
		return nil
	}
	switch {
	case !j.HasSteps:
		out = append(out, at("job-no-steps", j.Pos, fmt.Sprintf("job '%s' has neither steps: nor uses:; GitHub rejects the workflow", j.Name)))
	case j.StepsEmpty:
		out = append(out, at("job-empty-steps", j.Pos, fmt.Sprintf("job '%s' has steps: [] ; it runs, occupies a runner, and does nothing", j.Name)))
	}
	if j.RunsOn == nil {
		out = append(out, at("runs-on-missing", j.Pos, fmt.Sprintf("job '%s' has no runs-on:; GitHub rejects the workflow", j.Name)))
	} else {
		out = append(out, checkRunsOn(f, j, in)...)
	}
	if !j.TimeoutMinutes {
		out = append(out, at("job-timeout-missing", j.Pos, fmt.Sprintf("job '%s' has no timeout-minutes:; the default is 360", j.Name)))
	}
	out = append(out, checkStepsShape(f, j.Name, j.Steps)...)
	return out
}

func checkRunsOn(f *workflow.File, j workflow.Job, in StructureInputs) []finding.Finding {
	var out []finding.Finding
	r := j.RunsOn
	if r.HasGroup && !r.HasLabels {
		out = append(out, at("runs-on-group-and-labels", r.Pos, fmt.Sprintf("job '%s' runs-on names a group with no labels:; fine, but often a leftover", j.Name)))
	}
	selfHosted := false
	for _, l := range r.Labels {
		if l.Value == "self-hosted" || contains(in.SelfHostedLabels, l.Value) {
			selfHosted = true
		}
	}
	if selfHosted || r.HasGroup {
		return out
	}
	for _, l := range r.Labels {
		if l.IsExpr() {
			// runs-on-matrix: an expression backed by a matrix axis.
			if axis, ok := matrixAxisRef(l.Value); ok && j.Matrix != nil {
				for _, v := range j.Matrix.Axes[axis] {
					if v.IsExpr() || v.Tag == "" {
						continue
					}
					if msg := labelProblem(v.Value, in); msg != "" {
						out = append(out, at("runs-on-matrix", l.Pos, fmt.Sprintf("job '%s' runs-on takes matrix.%s, whose value %q %s", j.Name, axis, v.Value, msg)))
					}
				}
			}
			continue
		}
		if date, retired := RetiredLabels[l.Value]; retired {
			out = append(out, at("runs-on-retired", l.Pos, fmt.Sprintf("runner label %q was retired by GitHub on %s; the job will never schedule", l.Value, date)))
			continue
		}
		if msg := labelProblem(l.Value, in); msg != "" {
			out = append(out, at("runs-on-unknown", l.Pos, fmt.Sprintf("runner label %q %s; the job never schedules (add it to structure.runner_labels if it is a self-hosted label)", l.Value, msg)))
		}
	}
	return out
}

// labelProblem reports why a literal label would never schedule.
func labelProblem(label string, in StructureInputs) string {
	if date, retired := RetiredLabels[label]; retired {
		return "was retired by GitHub on " + date
	}
	if contains(RunnerLabels, label) || contains(in.RunnerLabels, label) || contains(in.SelfHostedLabels, label) {
		return ""
	}
	for _, prefix := range []string{"ubuntu-", "windows-", "macos-"} {
		if strings.HasPrefix(label, prefix) {
			return "" // larger-runner convention
		}
	}
	return "is not a GitHub-hosted runner label"
}

func matrixAxisRef(expr string) (string, bool) {
	t := strings.TrimSpace(expr)
	t = strings.TrimSuffix(strings.TrimPrefix(t, "${{"), "}}")
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "matrix.") && !strings.ContainsAny(t[7:], ". []()") {
		return t[7:], true
	}
	return "", false
}

// checkStepsShape is step-null and step-no-action.
func checkStepsShape(f *workflow.File, job string, steps []workflow.Step) []finding.Finding {
	var out []finding.Finding
	for _, st := range steps {
		label := fmt.Sprintf("step #%d", st.Index+1)
		if job != "" {
			label = "job '" + job + "' " + label
		}
		switch {
		case st.IsNull:
			out = append(out, at("step-null", st.Pos, label+" is null (an empty list item); GitHub rejects the workflow"))
		case !st.HasRun && st.Uses == "":
			out = append(out, at("step-no-action", st.Pos, label+" has neither run: nor uses:; GitHub rejects the workflow"))
		}
	}
	return out
}

// checkOrphanOutputs is job-orphan-outputs (§3.2).
func checkOrphanOutputs(f *workflow.File) []finding.Finding {
	if !f.HasWorkflowCall {
		return nil
	}
	neededBy := map[string]bool{}
	for _, j := range f.Jobs {
		for n := range TransitiveNeeds(f, j.Name) {
			neededBy[n] = true
		}
	}
	var out []finding.Finding
	for _, j := range f.Jobs {
		if len(j.Outputs) == 0 || j.Matrix != nil || neededBy[j.Name] {
			continue
		}
		lifted := false
		for _, o := range f.Outputs {
			if strings.Contains(o.Value, "jobs."+j.Name+".outputs") {
				lifted = true
			}
		}
		if lifted {
			continue
		}
		// Every output unused already carries output-unused; the job-level
		// finding adds nothing then.
		allUnused := true
		for _, o := range j.Outputs {
			for _, e := range f.Exprs {
				if strings.Contains(e.Text, "needs."+j.Name+".outputs."+o.Name) || strings.Contains(e.Text, "jobs."+j.Name+".outputs."+o.Name) {
					allUnused = false
				}
			}
		}
		if allUnused {
			continue
		}
		out = append(out, at("job-orphan-outputs", j.Pos, fmt.Sprintf("job '%s' declares outputs: but no job needs it and no on.workflow_call.outputs lifts them; delete the block or lift it", j.Name)))
	}
	return out
}

// LiteralFalse reports an if: that is literally false.
func LiteralFalse(s workflow.Scalar) bool {
	t := strings.TrimSpace(s.Value)
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "${{"), "}}"))
	return strings.EqualFold(t, "false")
}

// neverRuns is a job whose if: is literally false or provably false
// (advanced §3.2, §4.2): unsatisfiable, or a status contradiction.
func neverRuns(f *workflow.File, j *workflow.Job) bool {
	if LiteralFalse(j.If) || alwaysFalseIf(j.If) {
		return true
	}
	t := strings.TrimSpace(j.If.Value)
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "${{"), "}}"))
	if t == "" {
		return false
	}
	node, err := expr.Parse(t)
	if err != nil {
		return false
	}
	return statusContradiction(f, node, workflow.Expr{Job: j.Name, Step: -1, Text: t}) != ""
}

func keepsRunning(s workflow.Scalar) bool {
	return strings.Contains(s.Value, "always()") || strings.Contains(s.Value, "cancelled()")
}

// checkUnreachable is job-unreachable: a needs chain through an if: false
// job with nothing on the way that runs after a skip.
func checkUnreachable(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	for _, j := range f.Jobs {
		if keepsRunning(j.If) || neverRuns(f, &j) {
			continue
		}
		if via, ok := deadNeed(f, j.Name, map[string]bool{}); ok {
			out = append(out, at("job-unreachable", j.Pos, fmt.Sprintf("job '%s' needs '%s', whose if: is always false, and nothing on the way runs after a skip; the job never runs", j.Name, via)))
		}
	}
	return out
}

func deadNeed(f *workflow.File, job string, seen map[string]bool) (string, bool) {
	j := workflow.JobByName(f, job)
	if j == nil || seen[job] {
		return "", false
	}
	seen[job] = true
	for _, n := range j.Needs {
		nj := workflow.JobByName(f, n.Name)
		if nj == nil {
			continue
		}
		if neverRuns(f, nj) {
			return n.Name, true
		}
		if keepsRunning(nj.If) {
			continue
		}
		if via, ok := deadNeed(f, n.Name, seen); ok {
			return via, true
		}
	}
	return "", false
}

// CheckStepContinue is step-continue-on-error-outputs over one scope.
func CheckStepContinue(sc *Scope) []finding.Finding {
	var out []finding.Finding
	for _, st := range sc.Steps {
		if st.ID == "" || st.ContinueOnError == nil || st.ContinueOnError.Value != "true" {
			continue
		}
		for _, r := range sc.Refs {
			if r.ID != st.ID || r.Field != "outputs" {
				continue
			}
			guarded := false
			if r.Expr.Step >= 0 && r.Expr.Step < len(sc.Steps) {
				cond := sc.Steps[r.Expr.Step].If.Value
				guarded = strings.Contains(cond, "steps."+st.ID+".outcome") || strings.Contains(cond, "steps."+st.ID+".conclusion")
			}
			if strings.Contains(r.Expr.Text, "steps."+st.ID+".outcome") || strings.Contains(r.Expr.Text, "steps."+st.ID+".conclusion") {
				guarded = true
			}
			if !guarded {
				out = append(out, at("step-continue-on-error-outputs", r.Expr.Pos, fmt.Sprintf("steps.%s.outputs.%s is consumed, but step '%s' has continue-on-error: true and the consumer does not check its outcome; on failure the value is empty", st.ID, r.Output, st.ID)))
				break
			}
		}
	}
	return out
}
