package shapeq

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/workflow"
)

// TestFollowFilter pins which references of the shape fixture workflow a
// query loads (spec-shape §7, review 1 N5).
func TestFollowFilter(t *testing.T) {
	entry := filepath.Join("..", "..", "testdata", "shape", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	f, err := workflow.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at, expr string
		want     string // followed uses, sorted; "all" when the filter is nil
	}{
		{"set-vars", "steps.build-clusters-matrix.outputs.canary", ""},
		{"set-vars", "fromJSON(steps.build-clusters-matrix.outputs.canary)", ""},
		{"deploy", "steps.meta.outputs.sha", "./.github/actions/meta"},
		{"deploy", "needs.set-vars.result", "actions/checkout@v4"},
		{"deploy", "fromJSON(needs.set-vars.outputs.canary)[0].cluster", "actions/checkout@v4"},
		{"deploy", "matrix.os", "actions/checkout@v4"},
		{"", "github.event_name == 'push'", ""},
		{"", "inputs.cfg", ""},
		{"", "steps.meta.outputs.sha", "all"}, // no point on a multi-job file
		{"", "not an expression ((", "all"},
	} {
		keep := FollowFilter(entry, tc.at, tc.expr)
		got := "all"
		if keep != nil {
			var kept []string
			for _, u := range f.Uses {
				if keep(u) {
					kept = append(kept, u.Raw)
				}
			}
			sort.Strings(kept)
			got = strings.Join(kept, ", ")
		}
		if got != tc.want {
			t.Errorf("--at %q %q: followed %q, want %q", tc.at, tc.expr, got, tc.want)
		}
	}
}
