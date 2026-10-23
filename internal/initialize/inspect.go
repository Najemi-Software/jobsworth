// Package initialize implements `jobsworth init` (spec-init): repo
// inspection, local checkout lookup, optional probing, and rendering of
// .jobsworth.yml. It never writes outside the target repo and writes only
// the files the spec names.
package initialize

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"

	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// RefUse is one distinct external reference base (owner/repo[/path], no
// @ref) and where it is used.
type RefUse struct {
	Base string
	// Refs are the distinct @refs seen for this base, sorted.
	Refs []string
	// Files is the number of workflow/action files that use it.
	Files int
	// IsWorkflow is true when the base names a reusable workflow file.
	IsWorkflow bool
}

// RepoUse is one distinct external owner/repo and how many files use it.
type RepoUse struct {
	Slug  string // as first written
	Files int
}

// Inspection is what detection found (spec-init §3).
type Inspection struct {
	Root      string
	Workflows int
	Actions   int
	// ParseErrors are "path: message" lines for files that were skipped.
	ParseErrors []string
	// Refs is keyed by lower-cased base.
	Refs map[string]*RefUse
	// Repos is keyed by lower-cased owner/repo, excluding the repo's own.
	Repos map[string]*RepoUse
	// Self is the repo's own owner/repo from origin, "" if unknown.
	Self string
	// NonBashSteps counts steps that will not be analysed (§3.4).
	NonBashSteps int
}

// ExternalRefCount is the total number of distinct external references.
func ExternalRefCount(in Inspection) int {
	n := 0
	for _, r := range in.Refs {
		n += len(r.Refs)
	}
	return n
}

// Inspect walks root's .github tree and collects references (§3).
func Inspect(root string) Inspection {
	in := Inspection{Root: root, Refs: map[string]*RefUse{}, Repos: map[string]*RepoUse{}}
	if lk, ok := repolink.Detect(root, repolink.CWD); ok {
		in.Self = lk.Slug
	}
	var files []string
	for _, g := range []string{".github/workflows/*.{yml,yaml}", ".github/actions/**/action.{yml,yaml}"} {
		m, _ := doublestar.FilepathGlob(filepath.Join(root, g), doublestar.WithFilesOnly())
		files = append(files, m...)
	}
	sort.Strings(files)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			in.ParseErrors = append(in.ParseErrors, rel(root, path)+": "+err.Error())
			continue
		}
		f, err := workflow.Parse(data)
		if err != nil || f.Kind == workflow.KindUnknown {
			msg := "not a workflow or action"
			if err != nil {
				msg = err.Error()
			}
			in.ParseErrors = append(in.ParseErrors, rel(root, path)+": "+msg)
			continue
		}
		if f.Kind == workflow.KindWorkflow {
			in.Workflows++
		} else {
			in.Actions++
		}
		in.NonBashSteps += countNonBashSteps(data)
		seenBase := map[string]bool{}
		seenRepo := map[string]bool{}
		for _, u := range f.Uses {
			ref, err := uses.Parse(u.Raw)
			if err != nil || ref.Kind != uses.External {
				continue
			}
			base := ref.Owner + "/" + ref.Repo
			if ref.Path != "" {
				base += "/" + ref.Path
			}
			key := strings.ToLower(base)
			ru, ok := in.Refs[key]
			if !ok {
				ru = &RefUse{Base: base, IsWorkflow: ref.IsWorkflow}
				in.Refs[key] = ru
			}
			if !contains(ru.Refs, ref.Ref) {
				ru.Refs = append(ru.Refs, ref.Ref)
				sort.Strings(ru.Refs)
			}
			if !seenBase[key] {
				seenBase[key] = true
				ru.Files++
			}
			slug := strings.ToLower(ref.Owner + "/" + ref.Repo)
			if slug == in.Self {
				continue
			}
			rp, ok := in.Repos[slug]
			if !ok {
				rp = &RepoUse{Slug: ref.Owner + "/" + ref.Repo}
				in.Repos[slug] = rp
			}
			if !seenRepo[slug] {
				seenRepo[slug] = true
				rp.Files++
			}
		}
	}
	return in
}

// SortedRepos returns the external repos by descending use, then name.
func SortedRepos(in Inspection) []*RepoUse {
	out := make([]*RepoUse, 0, len(in.Repos))
	for _, r := range in.Repos {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return strings.ToLower(out[i].Slug) < strings.ToLower(out[j].Slug)
	})
	return out
}

// SortedRefs returns the references by base.
func SortedRefs(in Inspection) []*RefUse {
	out := make([]*RefUse, 0, len(in.Refs))
	for _, r := range in.Refs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Base) < strings.ToLower(out[j].Base) })
	return out
}

// countNonBashSteps counts run: steps whose effective shell is not bash/sh
// (§3.4): an explicit non-bash `shell:` on the step, or a non-bash
// `defaults.run.shell` with no step override.
func countNonBashSteps(data []byte) int {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return 0
	}
	root := doc.Content[0]
	fileDefault := defaultShell(root)
	n := 0
	var walk func(node *yaml.Node, inherited string)
	walk = func(node *yaml.Node, inherited string) {
		switch node.Kind {
		case yaml.MappingNode:
			if d := defaultShell(node); d != "" {
				inherited = d
			}
			if steps := mapValue(node, "steps"); steps != nil && steps.Kind == yaml.SequenceNode {
				for _, step := range steps.Content {
					if step.Kind != yaml.MappingNode || mapValue(step, "run") == nil {
						continue
					}
					shell := inherited
					if s := mapValue(step, "shell"); s != nil && s.Kind == yaml.ScalarNode {
						shell = s.Value
					}
					if shell != "" && !isBash(shell) {
						n++
					}
				}
			}
			for i := 0; i+1 < len(node.Content); i += 2 {
				walk(node.Content[i+1], inherited)
			}
		case yaml.SequenceNode:
			for _, c := range node.Content {
				walk(c, inherited)
			}
		}
	}
	walk(root, fileDefault)
	return n
}

func defaultShell(m *yaml.Node) string {
	d := mapValue(m, "defaults")
	if d == nil {
		return ""
	}
	r := mapValue(d, "run")
	if r == nil {
		return ""
	}
	s := mapValue(r, "shell")
	if s == nil || s.Kind != yaml.ScalarNode {
		return ""
	}
	return s.Value
}

func isBash(shell string) bool {
	s := strings.ToLower(strings.TrimSpace(shell))
	return s == "" || s == "bash" || s == "sh" || strings.HasPrefix(s, "bash ") || strings.HasPrefix(s, "sh ")
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

// Summary renders the "Inspecting …" line (§7).
func Summary(in Inspection) string {
	return fmt.Sprintf("%d workflows, %d actions, %d external references", in.Workflows, in.Actions, ExternalRefCount(in))
}
