package shapeq

import (
	"os"
	"strings"

	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// chainsOf is every rooted chain of an expression, inner chains
// (fromJSON arguments) included; nil when it does not parse.
func chainsOf(text string) []expr.Chain {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "${{") && strings.HasSuffix(t, "}}") {
		t = strings.TrimSpace(t[3 : len(t)-2])
	}
	node, err := expr.Parse(t)
	if err != nil {
		return nil
	}
	var out []expr.Chain
	var walk func(c expr.Chain)
	walk = func(c expr.Chain) {
		out = append(out, c)
		if c.Inner != nil {
			walk(*c.Inner)
		}
	}
	for _, c := range expr.Chains(node) {
		walk(c)
	}
	return out
}

// FollowFilter decides which of the entry's references the walk must
// load to answer one expression (spec-shape §7, "fetch only what the
// expression touches"): the step an `steps.ID` names, every reference of
// a job a `needs.X` names, and the needed jobs' references for `matrix`
// (its include/exclude may come from their outputs). Other roots
// (inputs, github, env, literals) need no callee. nil means follow
// everything: the file does not parse here, the point is ambiguous, or
// the expression does not parse.
func FollowFilter(entry, at, text string) func(workflow.Uses) bool {
	data, err := os.ReadFile(entry)
	if err != nil {
		return nil
	}
	f, err := workflow.Parse(data)
	if err != nil || f == nil {
		return nil
	}
	roots := rootsOf(text)
	if roots == nil {
		return nil
	}
	job := ""
	if f.Kind == workflow.KindWorkflow {
		job = at
		if i := strings.IndexByte(job, '/'); i >= 0 {
			job = job[:i]
		}
		if job == "" && len(f.Jobs) == 1 {
			job = f.Jobs[0].Name
		}
	}
	allOf := map[string]bool{} // jobs whose every reference is needed
	steps := map[string]bool{} // step ids in the point job
	needsOfPoint := func() {
		if j := workflow.JobByName(f, job); j != nil {
			for _, n := range j.Needs {
				allOf[n.Name] = true
			}
		}
	}
	for _, c := range chainsOf(text) {
		switch strings.ToLower(c.Root) {
		case "needs":
			if len(c.Segs) > 0 && c.Segs[0].Name != "" {
				allOf[c.Segs[0].Name] = true
			} else {
				needsOfPoint()
			}
		case "steps":
			if job == "" && f.Kind == workflow.KindWorkflow {
				return nil // the point is unknown; be safe
			}
			if len(c.Segs) > 0 && c.Segs[0].Name != "" && !c.Segs[0].Dynamic {
				steps[c.Segs[0].Name] = true
			} else {
				allOf[job] = true
			}
		case "matrix", "strategy":
			needsOfPoint()
		}
	}
	stepIDs := map[workflow.Pos]bool{}
	for id := range steps {
		for _, st := range workflow.StepsOf(f, job) {
			if st.ID == id && st.Uses != "" {
				stepIDs[st.UsesPos] = true
			}
		}
	}
	return func(u workflow.Uses) bool {
		return allOf[u.Job] || stepIDs[u.Pos]
	}
}
