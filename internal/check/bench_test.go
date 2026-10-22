package check_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/check"
)

// BenchmarkCheck runs the pipeline over a synthetic 50-job, 200-step
// workflow with only local references (test spec §4.2).
func BenchmarkCheck(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("on:\n  workflow_call:\n    inputs:\n      env: {type: string}\njobs:\nname: W1\n")
	for j := 0; j < 50; j++ {
		fmt.Fprintf(&sb, "  job%d:\n    runs-on: ubuntu-latest\n", j)
		if j > 0 {
			fmt.Fprintf(&sb, "    needs: [job%d]\n", j-1)
		}
		fmt.Fprintf(&sb, "    outputs:\n      out: ${{ steps.s0.outputs.x }}\n    steps:\n")
		for s := 0; s < 4; s++ {
			fmt.Fprintf(&sb, "      - id: s%d\n        if: inputs.env == 'x'\n        run: echo ${{ needs.job%d.outputs.out }}\n", s, max(j-1, 0))
		}
	}
	root := b.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "big.yml")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		check.RunEntry(path, check.Options{RootOverride: root})
	}
}
