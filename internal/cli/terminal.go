package cli

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether f is an interactive terminal. A character
// device such as /dev/null is not one (explain review D2).
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
