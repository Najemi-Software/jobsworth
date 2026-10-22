// Package check is the `check` pipeline (spec §1, §3): entry resolution, the
// uses: graph walk, and per-entry result collection. cmd/check turns flags
// into Options and prints what this package returns.
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/najemi-software/jobsworth/internal/workflow"
)

// ExpandEntriesWith turns positional arguments into the sorted, deduplicated list
// of absolute entry paths (§1.1). Literal paths are taken as-is (existence is
// checked, kind is not — a non-workflow literal is a finding, not a usage
// error). Globs are expanded and filtered to files that probe as a workflow
// or action. A glob matching nothing is an error. cwd is the directory
// relative paths resolve against; home replaces a leading "~". defaultGlobs
// apply as a set when args is empty: a repo with workflows but no actions
// directory is fine; only no match at all is an error.
func ExpandEntriesWith(args, defaultGlobs []string, cwd, home string) ([]string, error) {
	defaults := len(args) == 0
	if defaults {
		args = defaultGlobs
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, arg := range args {
		p := ExpandTilde(arg, home)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		if !IsGlob(arg) {
			st, err := os.Stat(p)
			if err != nil {
				return nil, fmt.Errorf("entry %s: %w", arg, err)
			}
			if st.IsDir() {
				return nil, fmt.Errorf("entry %s: is a directory", arg)
			}
			f, err := os.Open(p)
			if err != nil {
				return nil, fmt.Errorf("entry %s: %w", arg, err)
			}
			_ = f.Close()
			add(filepath.Clean(p))
			continue
		}
		matches, err := doublestar.FilepathGlob(p, doublestar.WithFilesOnly())
		if err != nil {
			return nil, fmt.Errorf("entry %s: bad pattern: %w", arg, err)
		}
		n := 0
		for _, m := range matches {
			if isWorkflowOrAction(m) {
				add(filepath.Clean(m))
				n++
			}
		}
		if n == 0 && !defaults {
			return nil, fmt.Errorf("entry %s: no workflow or action files match", arg)
		}
	}
	if defaults && len(out) == 0 {
		return nil, fmt.Errorf("no workflow or action files found under %s (looked in .github/workflows and .github/actions)", cwd)
	}
	sort.Strings(out)
	return out, nil
}

// IsGlob reports whether an argument contains glob metacharacters.
func IsGlob(s string) bool {
	return strings.ContainsAny(s, "*?[{")
}

// ExpandTilde replaces a leading "~" or "~/" with home.
func ExpandTilde(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// isWorkflowOrAction reports whether the file parses as YAML with a workflow
// or action shape (§1.1 glob filter).
func isWorkflowOrAction(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	kind, ok := workflow.ProbeKind(data)
	return ok && kind != workflow.KindUnknown
}
