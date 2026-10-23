// Package uses parses `uses:` reference strings into their form (spec §3.1).
// It is pure string work; resolving a Ref to a file is the loader's job.
package uses

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Kind is the reference form.
type Kind int

const (
	// Local: ./path inside the referencing file's repo.
	Local Kind = iota
	// External: owner/repo[/sub/path]@ref on GitHub.
	External
	// Docker: docker://image, never followed.
	Docker
	// Dynamic: contains ${{ ... }}, never followed.
	Dynamic
)

func (k Kind) String() string {
	switch k {
	case Local:
		return "local"
	case External:
		return "external"
	case Docker:
		return "docker"
	case Dynamic:
		return "dynamic"
	}
	return "unknown"
}

// Ref is a parsed reference.
type Ref struct {
	Kind Kind
	Raw  string
	// Path: for Local, the cleaned path relative to the repo root (no "./"
	// prefix); for External, the sub-path within the repo ("" for the root).
	Path string
	// Owner/Repo/Ref: External only. Owner and Repo keep the spelling from
	// the source; compare with strings.EqualFold.
	Owner, Repo, Ref string
	// IsWorkflow: the target is a reusable workflow file (.yml/.yaml) rather
	// than an action directory.
	IsWorkflow bool
}

// ErrMissingRef is returned for an external reference with no @ref.
var ErrMissingRef = errors.New("external reference has no @ref")

// Parse classifies a raw uses: value.
func Parse(raw string) (Ref, error) {
	s := strings.TrimSpace(raw)
	r := Ref{Raw: raw}
	switch {
	case s == "":
		return r, errors.New("empty uses: value")
	case strings.Contains(s, "${{"):
		r.Kind = Dynamic
		return r, nil
	case strings.HasPrefix(s, "docker://"):
		r.Kind = Docker
		return r, nil
	case strings.HasPrefix(s, "./") || s == ".":
		r.Kind = Local
		p := path.Clean(strings.TrimPrefix(s, "./"))
		if p == "." {
			p = ""
		}
		if p == ".." || strings.HasPrefix(p, "../") {
			return r, fmt.Errorf("local reference %q escapes the repo", raw)
		}
		r.Path = p
		r.IsWorkflow = isWorkflowPath(p)
		return r, nil
	}

	r.Kind = External
	at := strings.LastIndexByte(s, '@')
	if at < 0 {
		return r, ErrMissingRef
	}
	loc, ref := s[:at], s[at+1:]
	if ref == "" {
		return r, ErrMissingRef
	}
	parts := strings.Split(loc, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return r, fmt.Errorf("external reference %q is not owner/repo[/path]@ref", raw)
	}
	r.Owner, r.Repo, r.Ref = parts[0], parts[1], ref
	sub := strings.Join(parts[2:], "/")
	if sub != "" {
		cleaned := path.Clean(sub)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned == "." {
			return r, fmt.Errorf("external reference %q has an invalid path", raw)
		}
		for _, seg := range parts[2:] {
			if seg == "" {
				return r, fmt.Errorf("external reference %q has an empty path segment", raw)
			}
		}
		sub = cleaned
	}
	r.Path = sub
	r.IsWorkflow = isWorkflowPath(sub)
	return r, nil
}

// isWorkflowPath reports whether p names a YAML file (a reusable workflow)
// rather than an action directory.
func isWorkflowPath(p string) bool {
	return strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")
}

// Display renders an external ref as owner/repo[/path]@ref, the display name
// used for attribution (§3.4).
func Display(r Ref) string {
	if r.Kind != External {
		return r.Raw
	}
	s := r.Owner + "/" + r.Repo
	if r.Path != "" {
		s += "/" + r.Path
	}
	return s + "@" + r.Ref
}

// SlugNoRef renders an external ref as owner/repo[/path], the spelling the
// config skip lists use (§5.5). Lower-cased so lookups are case-insensitive.
func SlugNoRef(r Ref) string {
	s := r.Owner + "/" + r.Repo
	if r.Path != "" {
		s += "/" + r.Path
	}
	return strings.ToLower(s)
}
