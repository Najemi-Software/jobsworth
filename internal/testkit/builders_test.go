package testkit

import (
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/workflow"
)

func TestBuildersProduceParsedStructures(t *testing.T) {
	t.Parallel()
	st := Step().ID("meta").Run(`echo "k=v" >> $GITHUB_OUTPUT`).Build()
	if st.ID != "meta" || !st.HasRun || !strings.Contains(st.Run, "k=v") || st.Index != 0 {
		t.Fatalf("step = %+v", st)
	}
	wf := Workflow().
		Input("env", "string", "", true).
		Secret("TOKEN").
		Output("version", "${{ jobs.build.outputs.v }}").
		Job("build", Job().Outputs("v", "${{ steps.meta.outputs.k }}").Steps(
			Step().ID("meta").Run(`echo "k=${{ inputs.env }}" >> $GITHUB_OUTPUT`),
			Step().Uses("actions/checkout@v4").With("ref", "${{ github.sha }}").If("inputs.env == 'prod'"),
		)).
		Job("call", Job().Needs("build").Uses("./.github/workflows/reusable.yml", [2]string{"env", "prod"})).
		Build()
	if wf.Kind != workflow.KindWorkflow || len(wf.Jobs) != 2 || len(wf.Inputs) != 1 || len(wf.Secrets) != 1 || len(wf.Outputs) != 1 {
		t.Fatalf("workflow = %+v", wf)
	}
	build := workflow.JobByName(wf, "build")
	if build == nil || len(build.Outputs) != 1 || len(workflow.StepsOf(wf, "build")) != 2 {
		t.Fatalf("build job = %+v", build)
	}
	if steps := workflow.StepsOf(wf, "build"); steps[1].Uses != "actions/checkout@v4" || len(steps[1].With) != 1 || !steps[1].With[0].Value.IsExpr() || steps[1].If.Value == "" {
		t.Fatalf("checkout step = %+v", steps[1])
	}
	if call := workflow.JobByName(wf, "call"); call == nil || call.Uses == nil || len(call.Needs) != 1 {
		t.Fatalf("call job = %+v", call)
	}
	if len(wf.Exprs) == 0 || len(wf.Uses) != 2 {
		t.Fatalf("exprs %d uses %d", len(wf.Exprs), len(wf.Uses))
	}

	act := Action().Input("name", "world", false).Output("greeting", "${{ steps.g.outputs.text }}").
		Steps(Step().ID("g").Run(`echo "text=hi ${{ inputs.name }}" >> "$GITHUB_OUTPUT"`)).Build()
	if act.Kind != workflow.KindCompositeAction || len(act.Inputs) != 1 || len(act.Outputs) != 1 || len(act.Steps) != 1 || act.Steps[0].ID != "g" {
		t.Fatalf("action = %+v", act)
	}

	// Scalars that YAML would otherwise mangle survive the round trip.
	for _, v := range []string{"true", "", "a: b", "${{ x }}", "- dash", "#hash", "it's"} {
		s := Step().With("v", v).Uses("acme/x@v1").Build()
		if got := s.With[0].Value.Value; got != v {
			t.Errorf("with %q round-tripped as %q", v, got)
		}
	}
	if Workflow().DispatchInput("level", "choice", "low", "low", "high").Build().Inputs[0].Trigger != "workflow_dispatch" {
		t.Error("dispatch input not declared under workflow_dispatch")
	}
}
