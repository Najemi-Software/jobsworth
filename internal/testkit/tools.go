package testkit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/najemi-software/jobsworth/internal/scripts"
)

// FakeTools puts deterministic stand-ins for shellcheck, python3, pwsh,
// node and ruby first on PATH (advanced §5 tests), so the script rules
// run the same everywhere. Each stand-in reacts to a marker in its input:
// shellcheck reports SC2086 on a line holding an unquoted $FILES; the
// interpreters fail on a line holding "def broken(:" (python),
// `Write-Host "unterminated` (pwsh), "function (" (node) or "end end"
// (ruby). On Windows nothing is installed and the probes find nothing.
func FakeTools(t *testing.T) {
	t.Helper()
	scripts.ResetProbes()
	if runtime.GOOS == "windows" {
		t.Setenv("PATH", t.TempDir())
		return
	}
	bin := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("shellcheck", `in=$(cat)
n=$(printf '%s\n' "$in" | grep -n '[^"]\$FILES' | head -1 | cut -d: -f1)
if [ -n "$n" ]; then
  printf '[{"file":"-","line":%s,"column":6,"level":"warning","code":2086,"message":"Double quote to prevent globbing and word splitting."}]' "$n"
  exit 1
fi
printf '[]'
`)
	write("python3", `in=$(cat)
n=$(printf '%s\n' "$in" | grep -n 'def broken(:' | head -1 | cut -d: -f1)
if [ -n "$n" ]; then
  echo "  File \"<unknown>\", line $n" >&2
  echo "SyntaxError: invalid syntax (<unknown>, line $n)" >&2
  exit 1
fi
`)
	write("pwsh", `in=$(cat)
n=$(printf '%s\n' "$in" | grep -n 'Write-Host "unterminated' | head -1 | cut -d: -f1)
if [ -n "$n" ]; then
  echo "ParserError: At line:$n char:12 The string is missing the terminator: \"." >&2
  exit 1
fi
`)
	write("node", `f="$2"
n=$(grep -n 'function (' "$f" | head -1 | cut -d: -f1)
if [ -n "$n" ]; then
  echo "$f:$n" >&2
  echo "SyntaxError: Unexpected token '('" >&2
  exit 1
fi
`)
	write("ruby", `in=$(cat)
n=$(printf '%s\n' "$in" | grep -n 'end end' | head -1 | cut -d: -f1)
if [ -n "$n" ]; then
  echo "-:$n: syntax error, unexpected 'end'" >&2
  exit 1
fi
echo "Syntax OK"
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
