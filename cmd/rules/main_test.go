package rules

import (
	"os"
	"testing"

	"github.com/najemi-software/jobsworth/internal/scripts"
)

// TestMain empties PATH so the "(unavailable)" label of dynamic rules is
// the same on every machine.
func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "jobsworth-nopath")
	os.Setenv("PATH", dir)
	scripts.ResetProbes()
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
