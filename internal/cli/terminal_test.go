package cli

import (
	"os"
	"testing"
)

// TestIsTerminalDevNull is explain review D2: a character device that is
// not a tty must not count as a terminal.
func TestIsTerminalDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Fatal("os.DevNull reported as a terminal")
	}
	if IsTerminal(nil) {
		t.Fatal("nil reported as a terminal")
	}
	tmp, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer tmp.Close()
	if IsTerminal(tmp) {
		t.Fatal("a regular file reported as a terminal")
	}
}
