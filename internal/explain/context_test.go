package explain

import (
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// TestStatementsLineNumbers pins the arithmetic explain review D1 found
// wrong: a block scalar's first script line is the line after run: |.
func TestStatementsLineNumbers(t *testing.T) {
	script := "VERSION=1\necho \"version=$VERSION\"\necho \"sha=x\" >> \"$GITHUB_OUTPUT\"\n"
	a := bash.Analyse(script, false, nil)
	for _, tc := range []struct {
		name  string
		step  workflow.Step
		first int
	}{
		{"block scalar", workflow.Step{RunPos: workflow.Pos{Line: 7}, RunBlock: true, Run: script}, 8},
		{"plain scalar", workflow.Step{RunPos: workflow.Pos{Line: 7}, Run: script}, 7},
	} {
		lines, data := statements(tc.step, a, map[int]string{tc.first + 1: "marked"})
		if len(lines) != 3 || len(data) != 3 {
			t.Fatalf("%s: %d lines", tc.name, len(lines))
		}
		for i, want := range []int{tc.first, tc.first + 1, tc.first + 2} {
			if got := data[i]["line"]; got != want {
				t.Errorf("%s: statement %d at line %v, want %d", tc.name, i, got, want)
			}
		}
		if !strings.Contains(lines[1], "marked") || strings.Contains(lines[0], "marked") || strings.Contains(lines[2], "marked") {
			t.Errorf("%s: marker on the wrong row:\n%s", tc.name, strings.Join(lines, "\n"))
		}
	}
}
