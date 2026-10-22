package shapeq_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/shapeq"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

// trace runs check over testdata/shape/<rel> with the testdata/shape
// repo as root and returns its trace.
func trace(t *testing.T, kit *testkit.Kit, rel string) *check.FileTrace {
	t.Helper()
	root := filepath.Join(kit.Testdata, "shape")
	path := filepath.Join(root, filepath.FromSlash(rel))
	tr := check.NewTrace()
	check.RunEntry(path, check.Options{Fetcher: testkit.Fetcher(kit, false), RootOverride: root, Trace: tr})
	ft := tr.Get(path)
	if ft == nil {
		t.Fatalf("no trace for %s", rel)
	}
	return ft
}

func TestResolvePoint(t *testing.T) {
	kit := testkit.New(t, "../../testdata")
	ft := trace(t, kit, ".github/workflows/release.yml")
	for _, tc := range []struct {
		at, expr string
		job      string
		step     int
		wantErr  string
	}{
		{"", "github.sha", "", -1, ""},
		{"", "steps.x.outputs.y", "", -1, "pass --at"},
		{"set-vars", "steps.x", "set-vars", -1, ""},
		{"set-vars/build-clusters-matrix", "env.X", "set-vars", 1, ""},
		{"set-vars/#3", "env.X", "set-vars", 2, ""},
		{"set-vars/#9", "env.X", "", 0, "no step #9"},
		{"set-vars/nope", "env.X", "", 0, "no step with id 'nope'"},
		{"nope", "env.X", "", 0, "no job named 'nope'"},
	} {
		p, err := shapeq.ResolvePoint(ft, tc.at, tc.expr)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("--at %q: want error %q, got %v", tc.at, tc.wantErr, err)
			}
			continue
		}
		if err != nil || p.Job != tc.job || p.Step != tc.step {
			t.Errorf("--at %q: got %+v %v, want job %q step %d", tc.at, p, err, tc.job, tc.step)
		}
	}
	// Single-job file: the job is implied.
	single := trace(t, kit, ".github/workflows/reuse.yml")
	if p, err := shapeq.ResolvePoint(single, "", "steps.a.outputs.b"); err != nil || p.Job != "rollout" {
		t.Errorf("single job: %+v %v", p, err)
	}
	// Action: --at <step>, and a job/step form is an error.
	act := trace(t, kit, ".github/actions/meta/action.yml")
	if p, err := shapeq.ResolvePoint(act, "s", "steps.s.outputs.sha"); err != nil || p.Job != "" || p.Step != 0 {
		t.Errorf("action step: %+v %v", p, err)
	}
	if _, err := shapeq.ResolvePoint(act, "job/s", "x"); err == nil || !strings.Contains(err.Error(), "actions have no jobs") {
		t.Errorf("action with job: %v", err)
	}
}

// TestProvenanceKinds pins that each source kind yields exactly one
// entry with the right line (spec-shape §9).
func TestProvenanceKinds(t *testing.T) {
	kit := testkit.New(t, "../../testdata")
	ft := trace(t, kit, ".github/workflows/release.yml")
	kinds := func(a shapeq.Answer) map[string]int {
		out := map[string]int{}
		for _, c := range a.Chains {
			if c.Shape == nil {
				continue
			}
			for _, p := range c.Shape.Prov {
				out[p.Kind]++
			}
		}
		return out
	}
	lineOf := func(a shapeq.Answer, kind string) int {
		for _, c := range a.Chains {
			for _, p := range c.Shape.Prov {
				if p.Kind == kind {
					return p.Line
				}
			}
		}
		return -1
	}
	a, err := shapeq.Query(ft, shapeq.Point{Job: "set-vars", Step: -1}, "fromJSON(steps.build-clusters-matrix.outputs.canary)")
	if err != nil || a.Error != nil {
		t.Fatalf("query: %v %v", err, a.Error)
	}
	k := kinds(a)
	for _, want := range []string{"github_output_write", "bash_var", "file_read", "jq", "fromjson", "builtin"} {
		if k[want] != 1 {
			t.Errorf("%s: %d entries, want 1 (%v)", want, k[want], k)
		}
	}
	if lineOf(a, "github_output_write") != 17 || lineOf(a, "bash_var") != 16 {
		t.Errorf("lines: write %d var %d", lineOf(a, "github_output_write"), lineOf(a, "bash_var"))
	}
	a, _ = shapeq.Query(ft, shapeq.Point{Job: "deploy", Step: -1}, "needs.set-vars.outputs.canary")
	if k := kinds(a); k["lift"] != 1 || lineOf(a, "lift") != 9 {
		t.Errorf("lift: %v line %d", k, lineOf(a, "lift"))
	}
	a, _ = shapeq.Query(ft, shapeq.Point{Job: "deploy", Step: -1}, "matrix.os")
	if k := kinds(a); k["matrix"] != 1 {
		t.Errorf("matrix: %v", k)
	}
	a, _ = shapeq.Query(ft, shapeq.Point{Job: "set-vars", Step: 2}, "env.VERSION")
	if k := kinds(a); k["github_env_write"] != 1 || lineOf(a, "github_env_write") != 18 {
		t.Errorf("env write: %v line %d", k, lineOf(a, "github_env_write"))
	}
	a, _ = shapeq.Query(ft, shapeq.Point{Job: "set-vars", Step: 2}, "env.REGION")
	if k := kinds(a); k["env_block"] != 1 || lineOf(a, "env_block") != 4 {
		t.Errorf("env block: %v line %d", k, lineOf(a, "env_block"))
	}
	a, _ = shapeq.Query(ft, shapeq.Point{}, "github.event_name")
	if k := kinds(a); k["static_schema"] != 1 || k["access"] != 1 {
		t.Errorf("static: %v", k)
	}
	a, _ = shapeq.Query(ft, shapeq.Point{Job: "deploy", Step: -1}, "steps.meta.outputs.sha")
	if k := kinds(a); k["lift"] != 1 || lineOf(a, "lift") != 29 {
		t.Errorf("step lift: %v line %d", k, lineOf(a, "lift"))
	}
	a, _ = shapeq.Query(ft, shapeq.Point{}, "inputs[github.ref]")
	if k := kinds(a); k["opaque"] < 1 {
		t.Errorf("dynamic index should be opaque: %v", k)
	}
	reuse := trace(t, kit, ".github/workflows/reuse.yml")
	a, _ = shapeq.Query(reuse, shapeq.Point{}, "fromJSON(inputs.cfg).replicas")
	if k := kinds(a); k["declared_input"] != 1 || k["input_default"] != 1 || k["no_callers"] != 1 || k["access"] != 1 || lineOf(a, "declared_input") != 5 {
		t.Errorf("inputs: %v", k)
	}
	// The caller's with: shows up when the caller was walked first.
	rel := trace(t, kit, ".github/workflows/release.yml")
	_ = rel
	_ = os.Getenv
}
