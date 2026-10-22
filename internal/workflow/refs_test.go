package workflow

import (
	"reflect"
	"testing"
)

func TestInputRefs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expr string
		want []string
	}{
		{"inputs.env", []string{"env"}},
		{"inputs.env == 'prod' && inputs.verbose", []string{"env", "verbose"}},
		{"inputs['my-input']", []string{"my-input"}},
		{`inputs["x"]`, []string{"x"}},
		{"github.event.inputs.env", nil},
		{"myinputs.env", nil},
		{"format('{0}', inputs.a)", []string{"a"}},
		{"!inputs.flag", []string{"flag"}},
		{"steps.x.outputs.y", nil},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			if got := InputRefs(tc.expr); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("InputRefs(%q) = %v want %v", tc.expr, got, tc.want)
			}
		})
	}
}

func TestOutputRefs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expr      string
		wantNeeds []OutputRef
		wantJobs  []OutputRef
	}{
		{"needs.build.outputs.version", []OutputRef{{"build", "version"}}, nil},
		{"needs['build'].outputs.version", []OutputRef{{"build", "version"}}, nil},
		{"needs.build.outputs['version']", []OutputRef{{"build", "version"}}, nil},
		{"needs.build.result == 'success'", nil, nil},
		{"jobs.build.outputs.version", nil, []OutputRef{{"build", "version"}}},
		{"x.needs.build.outputs.version", nil, nil},
		{"needs.a.outputs.x || needs.b.outputs.y", []OutputRef{{"a", "x"}, {"b", "y"}}, nil},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			if got := NeedsOutputRefs(tc.expr); !reflect.DeepEqual(got, tc.wantNeeds) {
				t.Errorf("NeedsOutputRefs = %v want %v", got, tc.wantNeeds)
			}
			if got := JobsOutputRefs(tc.expr); !reflect.DeepEqual(got, tc.wantJobs) {
				t.Errorf("JobsOutputRefs = %v want %v", got, tc.wantJobs)
			}
		})
	}
}

// TestExtractExprsStrings is medium review 6 D13: }} inside a string
// literal does not end the expression.
func TestExtractExprsStrings(t *testing.T) {
	spans := ExtractExprs(`a ${{ format('{{x}}', 1) }} b ${{ format('it''s }}', 2) }} ${{ github.sha }}`, Pos{Line: 1, Column: 1})
	var got []string
	for _, s := range spans {
		got = append(got, s.Text)
	}
	want := []string{"format('{{x}}', 1)", "format('it''s }}', 2)", "github.sha"}
	if len(got) != len(want) {
		t.Fatalf("got %q want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d = %q, want %q", i, got[i], want[i])
		}
	}
}
