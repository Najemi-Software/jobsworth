package workflow

import (
	"reflect"
	"strings"
	"testing"
)

const sampleWorkflow = `name: sample
on:
  workflow_call:
    inputs:
      env:
        type: string
        required: true
      verbose:
        type: boolean
        default: false
    outputs:
      version:
        value: ${{ jobs.build.outputs.version }}
  workflow_dispatch:
    inputs:
      level:
        type: choice
        options: [low, high]
        default: low
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: ${{ steps.v.outputs.version }}
    steps:
      - uses: actions/checkout@v4
        with:
          uses: not-a-uses
      - id: v
        if: inputs.verbose
        run: echo "version=1" >> $GITHUB_OUTPUT
  deploy:
    needs: [build]
    if: ${{ inputs.env == 'prod' }}
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/deploy
        with:
          version: ${{ needs.build.outputs.version }}
  lint:
    needs: build
    uses: acme/shared/.github/workflows/lint.yml@v2
    with:
      strict: true
      level: ${{ inputs.level }}
`

func TestParseWorkflow(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte(sampleWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindWorkflow {
		t.Fatalf("kind = %v", f.Kind)
	}
	if !f.HasWorkflowCall || !f.HasWorkflowDispatch {
		t.Fatalf("triggers: call=%v dispatch=%v", f.HasWorkflowCall, f.HasWorkflowDispatch)
	}

	// Inputs: union of workflow_call and workflow_dispatch, with details.
	if got := InputNames(f); !reflect.DeepEqual(got, []string{"env", "verbose", "level"}) {
		t.Fatalf("inputs = %v", got)
	}
	env := f.Inputs[0]
	if env.Trigger != "workflow_call" || env.Type != "string" || !env.Required || env.HasDefault {
		t.Errorf("env = %+v", env)
	}
	verbose := f.Inputs[1]
	if !verbose.HasDefault || verbose.Default.Value != "false" || verbose.Default.Tag != "!!bool" {
		t.Errorf("verbose default = %+v", verbose.Default)
	}
	level := f.Inputs[2]
	if level.Trigger != "workflow_dispatch" || level.Type != "choice" || !reflect.DeepEqual(level.Options, []string{"low", "high"}) {
		t.Errorf("level = %+v", level)
	}
	if env.Pos.Line != 5 || env.Pos.Column != 7 {
		t.Errorf("env pos = %+v", env.Pos)
	}

	// File-level outputs.
	if len(f.Outputs) != 1 || f.Outputs[0].Name != "version" || f.Outputs[0].Value != "${{ jobs.build.outputs.version }}" {
		t.Errorf("outputs = %+v", f.Outputs)
	}

	// Jobs.
	if len(f.Jobs) != 3 {
		t.Fatalf("jobs = %d", len(f.Jobs))
	}
	build, deploy, lint := f.Jobs[0], f.Jobs[1], f.Jobs[2]
	if build.Name != "build" || len(build.Outputs) != 1 || build.Outputs[0].Name != "version" {
		t.Errorf("build = %+v", build)
	}
	if deploy.Name != "deploy" || len(deploy.Needs) != 1 || deploy.Needs[0].Name != "build" || deploy.Uses != nil {
		t.Errorf("deploy = %+v", deploy)
	}
	if lint.Uses == nil || lint.Uses.Raw != "acme/shared/.github/workflows/lint.yml@v2" || !lint.Uses.JobLevel || lint.Uses.Job != "lint" {
		t.Errorf("lint.Uses = %+v", lint.Uses)
	}
	if len(lint.Needs) != 1 || lint.Needs[0].Name != "build" {
		t.Errorf("lint.Needs = %+v", lint.Needs)
	}
	if len(lint.With) != 2 || lint.With[0].Name != "strict" || lint.With[0].Value.Tag != "!!bool" || !lint.With[1].Value.IsExpr() {
		t.Errorf("lint.With = %+v", lint.With)
	}

	// Uses: every occurrence, document order; `with.uses` is not one.
	var uses []string
	for _, u := range f.Uses {
		uses = append(uses, u.Raw)
	}
	want := []string{"actions/checkout@v4", "./.github/actions/deploy", "acme/shared/.github/workflows/lint.yml@v2"}
	if !reflect.DeepEqual(uses, want) {
		t.Fatalf("uses = %v want %v", uses, want)
	}
	if f.Uses[0].Job != "build" || f.Uses[0].JobLevel || f.Uses[2].Job != "lint" || !f.Uses[2].JobLevel {
		t.Errorf("uses attribution: %+v", f.Uses)
	}

	// Expressions: ${{ }} bodies and bare if: values, with job context.
	type ex struct{ text, job string }
	var got []ex
	for _, e := range f.Exprs {
		got = append(got, ex{e.Text, e.Job})
	}
	wantEx := []ex{
		{"jobs.build.outputs.version", ""},
		{"steps.v.outputs.version", "build"},
		{"inputs.verbose", "build"},
		{"inputs.env == 'prod'", "deploy"},
		{"needs.build.outputs.version", "deploy"},
		{"inputs.level", "lint"},
	}
	if !reflect.DeepEqual(got, wantEx) {
		t.Fatalf("exprs = %v\nwant %v", got, wantEx)
	}
}

func TestParseCompositeAction(t *testing.T) {
	t.Parallel()
	src := `name: deploy
inputs:
  version:
    required: true
  dry-run:
    default: "false"
outputs:
  url:
    value: ${{ steps.d.outputs.url }}
runs:
  using: composite
  steps:
    - uses: ./.github/actions/helper
    - id: d
      shell: bash
      run: echo "url=x" >> $GITHUB_OUTPUT
      env:
        V: ${{ inputs.version }}
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != KindCompositeAction {
		t.Fatalf("kind = %v", f.Kind)
	}
	if got := InputNames(f); !reflect.DeepEqual(got, []string{"version", "dry-run"}) {
		t.Errorf("inputs = %v", got)
	}
	if !f.Inputs[0].Required || f.Inputs[0].Trigger != "" || !f.Inputs[1].HasDefault || f.Inputs[1].Default.Tag != "!!str" {
		t.Errorf("inputs = %+v", f.Inputs)
	}
	if len(f.Outputs) != 1 || f.Outputs[0].Name != "url" {
		t.Errorf("outputs = %+v", f.Outputs)
	}
	if len(f.Uses) != 1 || f.Uses[0].Raw != "./.github/actions/helper" || f.Uses[0].Job != "" || f.Uses[0].JobLevel {
		t.Errorf("uses = %+v", f.Uses)
	}
	if len(f.Exprs) != 2 || f.Exprs[1].Text != "inputs.version" {
		t.Errorf("exprs = %+v", f.Exprs)
	}
}

func TestParseUsesOnlyUnderSteps(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte(`on: push
jobs:
  a:
    strategy:
      matrix:
        include:
          - uses: not/a-step@v1
    steps:
      - uses: real/step@v1
    with:
      uses: nope
  b:
    uses: ./.github/workflows/x.yml
`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range f.Uses {
		got = append(got, u.Raw)
	}
	if !reflect.DeepEqual(got, []string{"real/step@v1", "./.github/workflows/x.yml"}) {
		t.Fatalf("uses = %v", got)
	}
}

func TestParseTriggerShapes(t *testing.T) {
	t.Parallel()
	for src, want := range map[string][2]bool{
		"on: workflow_call\njobs: {}\n":                           {true, false},
		"on: [push, workflow_dispatch]\njobs: {}\n":               {false, true},
		"on:\n  workflow_call:\n  workflow_dispatch:\njobs: {}\n": {true, true},
		"on: push\njobs: {}\n":                                    {false, false},
	} {
		f, err := Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if f.HasWorkflowCall != want[0] || f.HasWorkflowDispatch != want[1] {
			t.Errorf("%q: call=%v dispatch=%v", src, f.HasWorkflowCall, f.HasWorkflowDispatch)
		}
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte("jobs: [\n")); err == nil || !strings.Contains(err.Error(), "yaml") {
		t.Fatalf("err = %v", err)
	}
	f, err := Parse([]byte("foo: bar\n"))
	if err != nil || f.Kind != KindUnknown {
		t.Fatalf("unknown shape: %v %v", f.Kind, err)
	}
	f, err = Parse([]byte(""))
	if err != nil || f.Kind != KindUnknown {
		t.Fatalf("empty: %v %v", f.Kind, err)
	}
}

func TestExtractExprs(t *testing.T) {
	t.Parallel()
	got := ExtractExprs("a ${{ x }} b ${{y}} ${{ unterminated", Pos{Line: 3, Column: 7})
	want := []ExprSpan{{"x", Pos{3, 7}}, {"y", Pos{3, 7}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := ExtractExprs("plain", Pos{}); got != nil {
		t.Fatalf("plain: %+v", got)
	}
}
