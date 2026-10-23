package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

func parse(t *testing.T, src string) *workflow.File {
	t.Helper()
	f, err := workflow.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// ids renders findings as "rule" or "rule:substring-check" for comparison.
func ids(fs []finding.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	sort.Strings(out)
	return out
}

func expectRules(t *testing.T, got []finding.Finding, want ...string) {
	t.Helper()
	sort.Strings(want)
	g := ids(got)
	if strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("rules = %v want %v\nfindings: %+v", g, want, got)
	}
	for _, f := range got {
		if f.Severity != Registry[f.Rule] {
			t.Errorf("%s emitted with severity %v, registry says %v", f.Rule, f.Severity, Registry[f.Rule])
		}
	}
}

func msgContains(t *testing.T, got []finding.Finding, rule, sub string) {
	t.Helper()
	for _, f := range got {
		if f.Rule == rule && strings.Contains(f.Message, sub) {
			return
		}
	}
	t.Fatalf("no %s finding with %q in: %+v", rule, sub, got)
}

// --- §7.1 ------------------------------------------------------------------

func TestCheckInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want []string
		sub  string
	}{
		{
			name: "undeclared",
			src: `on:
  workflow_call:
    inputs:
      env: {type: string}
jobs:
  a:
    if: inputs.env == 'x' && inputs.missing
    steps: []
`,
			want: []string{"input-undeclared"},
			sub:  "'missing'",
		},
		{
			name: "undeclared via bracket",
			src: `on:
  workflow_call:
    inputs:
      env: {type: string}
jobs:
  a:
    steps:
      - run: echo ${{ inputs['env'] }} ${{ inputs['nope'] }}
`,
			want: []string{"input-undeclared"},
		},
		{
			name: "github.event.inputs is not a reference",
			src: `on:
  workflow_call:
    inputs:
      env: {type: string}
jobs:
  a:
    steps:
      - run: echo ${{ inputs.env }} ${{ github.event.inputs.other }}
`,
			want: nil,
		},
		{
			name: "unused",
			src: `on:
  workflow_call:
    inputs:
      env: {type: string}
      verbose: {type: boolean}
jobs:
  a:
    steps:
      - run: echo ${{ inputs.env }}
`,
			want: []string{"input-unused"},
			sub:  "'verbose'",
		},
		{
			name: "unused suppressed for dispatch-only workflow",
			src: `on:
  workflow_dispatch:
    inputs:
      env: {type: string}
jobs:
  a:
    steps:
      - run: echo ${{ github.event.inputs.env }}
`,
			want: nil,
		},
		{
			name: "unused not suppressed for dispatch input when workflow_call present",
			src: `on:
  workflow_call:
  workflow_dispatch:
    inputs:
      env: {type: string}
jobs:
  a:
    steps: []
`,
			want: []string{"input-unused"},
		},
		{
			name: "unused skipped for node action",
			src: `inputs:
  token: {required: true}
runs:
  using: node20
  main: index.js
`,
			want: nil,
		},
		{
			name: "unused reported for composite action",
			src: `inputs:
  token: {required: true}
runs:
  using: composite
  steps: []
`,
			want: []string{"input-unused"},
		},
		{
			name: "default type number bad",
			src: `on:
  workflow_call:
    inputs:
      n: {type: number, default: abc}
jobs:
  a:
    if: inputs.n
    steps: []
`,
			want: []string{"input-default-type"},
			sub:  "not a number",
		},
		{
			name: "default type number ok",
			src: `on:
  workflow_call:
    inputs:
      n: {type: number, default: 3.5}
jobs:
  a:
    if: inputs.n
    steps: []
`,
			want: nil,
		},
		{
			name: "default type boolean quoted yes bad",
			src: `on:
  workflow_call:
    inputs:
      b: {type: boolean, default: "yes"}
jobs:
  a:
    if: inputs.b
    steps: []
`,
			want: []string{"input-default-type"},
		},
		{
			name: "default type boolean quoted true bad",
			src: `on:
  workflow_call:
    inputs:
      b: {type: boolean, default: "true"}
jobs:
  a:
    if: inputs.b
    steps: []
`,
			want: []string{"input-default-type"},
		},
		{
			name: "default type boolean ok",
			src: `on:
  workflow_call:
    inputs:
      b: {type: boolean, default: true}
jobs:
  a:
    if: inputs.b
    steps: []
`,
			want: nil,
		},
		{
			name: "default choice outside options",
			src: `on:
  workflow_call:
  workflow_dispatch:
    inputs:
      c: {type: choice, options: [a, b], default: z}
jobs:
  x:
    if: inputs.c
    steps: []
`,
			want: nil, // choice defaults belong to choice-default-not-option (§5.3)
		},
		{
			name: "default choice ok",
			src: `on:
  workflow_dispatch:
    inputs:
      c: {type: choice, options: [a, b], default: b}
jobs:
  x:
    if: inputs.c
    steps: []
`,
			want: nil,
		},
		{
			name: "default expression is not type checked",
			src: `on:
  workflow_call:
    inputs:
      n: {type: number, default: "${{ github.run_number }}"}
jobs:
  a:
    if: inputs.n
    steps: []
`,
			want: nil,
		},
		{
			name: "string type accepts anything",
			src: `on:
  workflow_call:
    inputs:
      s: {type: string, default: 42}
jobs:
  a:
    if: inputs.s
    steps: []
`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckInputs(parse(t, tc.src))
			expectRules(t, got, tc.want...)
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
		})
	}
}

// --- §7.2 ------------------------------------------------------------------

const callee = `on:
  workflow_call:
    inputs:
      env: {type: string, required: true}
      count: {type: number}
      strict: {type: boolean, default: false}
      level: {type: choice, options: [low, high]}
      with-default: {type: string, required: true, default: x}
  workflow_dispatch:
    inputs:
      dispatch-only: {type: string}
jobs:
  a:
    if: inputs.env && inputs.count && inputs.strict && inputs.level && inputs.with-default
    steps: []
`

func TestCheckCallInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		with string
		want []string
		sub  string
	}{
		{name: "all good", with: "env: prod\ncount: 3\nstrict: true\nlevel: high\n"},
		{name: "unknown input", with: "env: prod\nbogus: 1\n", want: []string{"call-input-unknown"}, sub: "with.bogus"},
		{name: "dispatch input is unknown to workflow_call", with: "env: prod\ndispatch-only: 1\n", want: []string{"call-input-unknown"}},
		{name: "missing required", with: "count: 1\n", want: []string{"call-input-missing"}, sub: "'env'"},
		{name: "required with default is not missing", with: "env: x\n"},
		{name: "number given string", with: "env: x\ncount: three\n", want: []string{"call-input-type"}, sub: "with.count"},
		{name: "number given quoted number", with: "env: x\ncount: \"3\"\n", want: []string{"call-input-type"}},
		{name: "boolean given yes", with: "env: x\nstrict: yes\n", want: []string{"call-input-type"}},
		{name: "boolean given quoted true", with: "env: x\nstrict: \"true\"\n", want: []string{"call-input-type"}},
		{name: "choice outside options", with: "env: x\nlevel: medium\n", want: []string{"call-input-type"}},
		{name: "expression value is not type checked", with: "env: x\ncount: ${{ github.run_number }}\nstrict: ${{ inputs.s }}\n"},
		{name: "several findings", with: "count: x\nbogus: 1\n", want: []string{"call-input-missing", "call-input-type", "call-input-unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "on: push\njobs:\n  call:\n    uses: acme/shared/.github/workflows/x.yml@v1\n    with:\n"
			for _, line := range strings.Split(strings.TrimSpace(tc.with), "\n") {
				if line != "" {
					src += "      " + line + "\n"
				}
			}
			caller := parse(t, src)
			got := CheckCallInputs(caller.Jobs[0], parse(t, callee), "acme/shared/.github/workflows/x.yml@v1")
			expectRules(t, got, tc.want...)
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
			for _, f := range got {
				if !strings.HasPrefix(f.Message, "job 'call' calling acme/shared/.github/workflows/x.yml@v1") {
					t.Errorf("label missing: %q", f.Message)
				}
			}
		})
	}
	t.Run("nil callee runs nothing", func(t *testing.T) {
		t.Parallel()
		caller := parse(t, "on: push\njobs:\n  call:\n    uses: a/b/.github/workflows/x.yml@v1\n    with:\n      bogus: 1\n")
		expectRules(t, CheckCallInputs(caller.Jobs[0], nil, "x"))
	})
}

// --- §7.3 ------------------------------------------------------------------

func TestCheckJobGraph(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		jobs string
		want []string
		sub  string
	}{
		{name: "linear", jobs: "a: {}\nb: {needs: a}\nc: {needs: [a, b]}\n"},
		{name: "forward reference allowed", jobs: "a: {needs: b}\nb: {}\n"},
		{name: "unknown", jobs: "a: {needs: nope}\n", want: []string{"needs-unknown"}, sub: "'nope'"},
		{name: "self", jobs: "a: {needs: [a]}\n", want: []string{"needs-self"}},
		{name: "two-cycle", jobs: "a: {needs: b}\nb: {needs: a}\n", want: []string{"needs-cycle"}, sub: "a -> b -> a"},
		{name: "three-cycle reported once", jobs: "a: {needs: c}\nb: {needs: a}\nc: {needs: b}\nd: {needs: a}\n", want: []string{"needs-cycle"}},
		{name: "two separate cycles", jobs: "a: {needs: b}\nb: {needs: a}\nc: {needs: d}\nd: {needs: c}\n", want: []string{"needs-cycle", "needs-cycle"}},
		{name: "self plus unknown", jobs: "a: {needs: [a, zz]}\n", want: []string{"needs-self", "needs-unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "on: push\njobs:\n"
			for _, line := range strings.Split(strings.TrimSpace(tc.jobs), "\n") {
				src += "  " + line + "\n"
			}
			got := CheckJobGraph(parse(t, src))
			expectRules(t, got, tc.want...)
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
		})
	}
}

// --- §7.4 ------------------------------------------------------------------

func TestCheckJobOutputs(t *testing.T) {
	t.Parallel()
	known := func(names ...string) JobOutputs { return JobOutputs{Known: true, Names: names} }
	cases := []struct {
		name    string
		src     string
		callees map[string]JobOutputs
		want    []string
		sub     string
	}{
		{
			name: "clean",
			src: `on: push
jobs:
  build:
    outputs:
      version: ${{ steps.v.outputs.version }}
    steps: []
  deploy:
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
`,
		},
		{
			name: "job unknown",
			src: `on: push
jobs:
  deploy:
    steps:
      - run: echo ${{ needs.nope.outputs.version }}
`,
			want: []string{"needs-output-job-unknown"},
			sub:  "'nope'",
		},
		{
			name: "not needed",
			src: `on: push
jobs:
  build:
    outputs:
      version: x
    steps: []
  deploy:
    steps:
      - run: echo ${{ needs.build.outputs.version }}
`,
			want: []string{"needs-output-not-needed"},
			sub:  "not in its needs",
		},
		{
			name: "transitive need is fine",
			src: `on: push
jobs:
  build:
    outputs:
      version: x
    steps: []
  test:
    needs: build
    steps: []
  deploy:
    needs: test
    steps:
      - run: echo ${{ needs.build.outputs.version }}
`,
		},
		{
			name: "undeclared",
			src: `on: push
jobs:
  build:
    outputs:
      version: x
    steps: []
  deploy:
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.sha }} ${{ needs.build.outputs.version }}
`,
			want: []string{"needs-output-undeclared"},
			sub:  "no output 'sha'",
		},
		{
			name: "undeclared skipped when callee unknown",
			src: `on: push
jobs:
  build:
    uses: a/b/.github/workflows/x.yml@v1
  deploy:
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.sha }}
`,
			callees: map[string]JobOutputs{"build": {Known: false}},
		},
		{
			name: "undeclared on uses job with known callee",
			src: `on: push
jobs:
  build:
    uses: a/b/.github/workflows/x.yml@v1
  deploy:
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.sha }} ${{ needs.build.outputs.version }}
`,
			callees: map[string]JobOutputs{"build": known("version")},
			want:    []string{"needs-output-undeclared"},
		},
		{
			name: "call output job unknown",
			src: `on:
  workflow_call:
    outputs:
      v:
        value: ${{ jobs.nope.outputs.version }}
jobs:
  build:
    steps: []
`,
			want: []string{"call-output-job-unknown"},
		},
		{
			name: "call output undeclared",
			src: `on:
  workflow_call:
    outputs:
      v:
        value: ${{ jobs.build.outputs.version }}
jobs:
  build:
    outputs:
      sha: x
    steps: []
`,
			want: []string{"call-output-undeclared", "output-unused"},
		},
		{
			name: "lifted into workflow_call output counts as used",
			src: `on:
  workflow_call:
    outputs:
      v:
        value: ${{ jobs.build.outputs.version }}
jobs:
  build:
    outputs:
      version: x
    steps: []
`,
		},
		{
			name: "unused",
			src: `on: push
jobs:
  build:
    outputs:
      version: x
      sha: y
    steps: []
  deploy:
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
`,
			want: []string{"output-unused"},
			sub:  "'sha'",
		},
		{
			name: "needs.result is not an output",
			src: `on: push
jobs:
  build:
    steps: []
  deploy:
    needs: build
    if: needs.build.result == 'success'
    steps: []
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckJobOutputs(parse(t, tc.src), tc.callees)
			expectRules(t, got, tc.want...)
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
		})
	}
}

func TestTransitiveNeeds(t *testing.T) {
	t.Parallel()
	f := parse(t, "on: push\njobs:\n  a: {}\n  b: {needs: a}\n  c: {needs: b}\n  d: {needs: [c, x]}\n")
	got := TransitiveNeeds(f, "d")
	for _, name := range []string{"a", "b", "c", "x"} {
		if !got[name] {
			t.Errorf("missing %s in %v", name, got)
		}
	}
	if got["d"] || len(got) != 4 {
		t.Errorf("closure = %v", got)
	}
}
