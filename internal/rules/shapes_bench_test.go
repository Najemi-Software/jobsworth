package rules_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/najemi-software/jobsworth/internal/check"
)

// TestEnvForwardingIsFast is medium review D5: ten `K: ${{ env.K }}` step
// env entries and fifty env.* references must check in well under a
// second, not minutes.
func TestEnvForwardingIsFast(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("name: Bench\non: push\npermissions: read-all\nenv:\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "  K%d: v%d\n", i, i)
	}
	sb.WriteString("jobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n")
	for s := 0; s < 5; s++ {
		sb.WriteString("      - env:\n")
		for i := 0; i < 10; i++ {
			fmt.Fprintf(&sb, "          K%d: ${{ env.K%d }}\n", i, i)
		}
		sb.WriteString("        run: |\n")
		for i := 0; i < 10; i++ {
			fmt.Fprintf(&sb, "          echo ${{ env.K%d }} ${{ github.sha }}\n", i)
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "env.yml")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res := check.RunEntry(path, check.Options{RootOverride: root})
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
	if fs := check.AllFindings(res); len(fs) != 0 {
		t.Fatalf("unexpected findings: %+v", fs)
	}
}
