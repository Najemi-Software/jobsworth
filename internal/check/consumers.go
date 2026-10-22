package check

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// consumerIndex records, per local action directory, which of its outputs
// some workflow or action in a locally known repo consumes (medium §1.6).
// Built lazily per repo root and shared by every entry of a run.
type consumerIndex struct {
	mu    sync.Mutex
	roots map[string]map[string]map[string]bool // root -> action dir -> output -> true
	// inherit records, per root, the local reusable workflows some caller
	// invokes with secrets: inherit (medium review D6).
	inherit map[string]map[string]bool
	// callers records, per root, every job-level call of a local reusable
	// workflow (callee path -> callers), for the explain secrets block.
	callers map[string]map[string][]CallerTrace
	// names records, per root, the name: of every workflow (advanced §2.1
	// on-workflow-run-unknown).
	names map[string][]rules.NamedWorkflow
}

// workflowNames returns the name: values of the workflows under root.
func (ci *consumerIndex) workflowNames(root string) []string {
	var out []string
	for _, w := range ci.namedWorkflows(root) {
		out = append(out, w.Name)
	}
	return out
}

// namedWorkflows returns the workflows under root with their paths.
func (ci *consumerIndex) namedWorkflows(root string) []rules.NamedWorkflow {
	ci.scanRoot(root)
	ci.mu.Lock()
	defer ci.mu.Unlock()
	return ci.names[root]
}

var consumers = &consumerIndex{roots: map[string]map[string]map[string]bool{}, inherit: map[string]map[string]bool{}, callers: map[string]map[string][]CallerTrace{}, names: map[string][]rules.NamedWorkflow{}}

// callersByRoot returns every local job-level call under root, keyed by
// the callee's absolute path.
func (ci *consumerIndex) callersByRoot(root string) map[string][]CallerTrace {
	ci.scanRoot(root)
	ci.mu.Lock()
	defer ci.mu.Unlock()
	return ci.callers[root]
}

// inheritCallers returns the local reusable workflows called with
// secrets: inherit from somewhere under root.
func (ci *consumerIndex) inheritCallers(root string) map[string]bool {
	ci.scanRoot(root)
	ci.mu.Lock()
	defer ci.mu.Unlock()
	return ci.inherit[root]
}

// scanRoot parses every workflow and action under root and collects
// steps.<id>.outputs.<o> references through `./` uses. Findings are never
// produced; this only ever suppresses a note.
func (ci *consumerIndex) scanRoot(root string) map[string]map[string]bool {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	if m, ok := ci.roots[root]; ok {
		return m
	}
	m := map[string]map[string]bool{}
	ci.roots[root] = m
	inh := map[string]bool{}
	ci.inherit[root] = inh
	callers := map[string][]CallerTrace{}
	ci.callers[root] = callers
	var files []string
	for _, g := range []string{".github/workflows/*.{yml,yaml}", ".github/actions/**/action.{yml,yaml}"} {
		matches, _ := doublestar.FilepathGlob(filepath.Join(root, g), doublestar.WithFilesOnly())
		files = append(files, matches...)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f, err := workflow.Parse(data)
		if err != nil {
			continue
		}
		scopes := []string{""}
		if f.Kind == workflow.KindWorkflow {
			scopes = scopes[:0]
			if f.Name.Value != "" {
				ci.names[root] = append(ci.names[root], rules.NamedWorkflow{Name: f.Name.Value, Path: path})
			}
			for _, j := range f.Jobs {
				scopes = append(scopes, j.Name)
				if j.Uses != nil {
					if ref, err := uses.Parse(j.Uses.Raw); err == nil && ref.Kind == uses.Local {
						callee := filepath.Join(root, filepath.FromSlash(ref.Path))
						rel, _ := filepath.Rel(root, path)
						callers[callee] = append(callers[callee], CallerTrace{File: filepath.ToSlash(rel), Job: j.Name, Inherit: j.SecretsInherit})
						if j.SecretsInherit {
							inh[callee] = true
						}
					}
				}
			}
		}
		for _, job := range scopes {
			byID := map[string]string{} // step id -> action dir
			for _, st := range workflow.StepsOf(f, job) {
				if st.ID == "" || st.Uses == "" {
					continue
				}
				ref, err := uses.Parse(st.Uses)
				if err != nil || ref.Kind != uses.Local {
					continue
				}
				byID[st.ID] = filepath.Join(root, filepath.FromSlash(ref.Path))
			}
			for _, e := range f.Exprs {
				if e.Job != job {
					continue
				}
				for _, r := range workflow.StepRefs(e.Text) {
					dir, ok := byID[r.ID]
					if !ok || r.Field != "outputs" {
						continue
					}
					if m[dir] == nil {
						m[dir] = map[string]bool{}
					}
					m[dir][r.Output] = true
				}
			}
		}
	}
	return m
}

// actionOutputUnused reports a local composite action's outputs that no
// workflow or action in the entry repo or any linked repo consumes.
func actionOutputUnused(l *loader, cur *loaded) []finding.Finding {
	if !cur.src.local || cur.file.Kind != workflow.KindCompositeAction || len(cur.file.Outputs) == 0 {
		return nil
	}
	dir := filepath.Dir(cur.src.path)
	roots := []string{cur.src.root}
	if l.links != nil {
		for _, lk := range repolink.All(l.links) {
			roots = append(roots, lk.Path)
		}
	}
	consumed := map[string]bool{}
	for o := range l.consumedBy(identity(cur.src)) {
		consumed[o] = true
	}
	for _, root := range roots {
		for d, outs := range consumers.scanRoot(root) {
			if sameDir(d, dir) {
				for o := range outs {
					consumed[o] = true
				}
			}
		}
	}
	var out []finding.Finding
	for _, o := range cur.file.Outputs {
		if !consumed[o.Name] {
			out = append(out, rules.Finding("action-output-unused", o.Pos, "output '"+o.Name+"' is declared but no workflow or action in the entry repo or a linked repo consumes it"))
		}
	}
	return out
}

func sameDir(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(ca)
	rb, err2 := filepath.EvalSymlinks(cb)
	return err1 == nil && err2 == nil && strings.EqualFold(ra, rb)
}
