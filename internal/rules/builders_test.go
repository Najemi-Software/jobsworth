package rules_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

func ruleIDs(fs []finding.Finding) string {
	var ids []string
	for _, f := range fs {
		ids = append(ids, f.Rule)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// TestJobOutputsWithBuilders is the §3.3 builders driving a rule function
// directly: needs.<job>.outputs against declared outputs.
func TestJobOutputsWithBuilders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		wf   *testkit.WorkflowBuilder
		want string
	}{
		{"declared output consumed", testkit.Workflow().
			Job("build", testkit.Job().Outputs("version", "${{ steps.v.outputs.version }}").Steps(testkit.Step().ID("v").Run(`echo "version=1" >> "$GITHUB_OUTPUT"`))).
			Job("deploy", testkit.Job().Needs("build").Steps(testkit.Step().Run("echo ${{ needs.build.outputs.version }}"))), ""},
		{"undeclared output", testkit.Workflow().
			Job("build", testkit.Job().Outputs("version", "${{ steps.v.outputs.version }}").Steps(testkit.Step().ID("v").Run(`echo "version=1" >> "$GITHUB_OUTPUT"`))).
			Job("deploy", testkit.Job().Needs("build").Steps(testkit.Step().Run("echo ${{ needs.build.outputs.sha }}"))), "needs-output-undeclared,output-unused"},
		{"output of a job not needed", testkit.Workflow().
			Job("build", testkit.Job().Outputs("version", "${{ steps.v.outputs.version }}").Steps(testkit.Step().ID("v").Run(`echo "version=1" >> "$GITHUB_OUTPUT"`))).
			Job("deploy", testkit.Job().Steps(testkit.Step().Run("echo ${{ needs.build.outputs.version }}"))), "needs-output-not-needed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rules.CheckJobOutputs(tc.wf.Build(), nil)
			if ruleIDs(got) != tc.want {
				t.Fatalf("rules %q want %q\n%+v", ruleIDs(got), tc.want, got)
			}
		})
	}
}
