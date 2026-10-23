package check

import (
	"sort"
	"sync"

	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/suppress"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Trace captures what the walk had in hand for every file it validated, so
// explain can render findings in context (spec-explain §3). Keyed by the
// file name findings are attributed to.
type Trace struct {
	mu    sync.Mutex
	Files map[string]*FileTrace
}

// NewTrace returns an empty trace to pass in Options.
func NewTrace() *Trace { return &Trace{Files: map[string]*FileTrace{}} }

// FileTrace is one validated file.
type FileTrace struct {
	File   *workflow.File
	Scopes map[string]*rules.Scope
	// Raw is the file's bytes (for run: block line ranges).
	Raw []byte
	// How says where the file came from: local, linked, fetched.
	How string
	// JobCallees maps a uses: job to its callee; StepCallees maps job ->
	// step index -> callee. A callee that failed to load is recorded with
	// File nil and Err set.
	JobCallees  map[string]*CalleeTrace
	StepCallees map[string]map[int]*CalleeTrace
	// Suppressions are the file's parsed directives.
	Suppressions *suppress.Set
	// Callers lists the known callers of a reusable workflow: those seen
	// earlier in this walk and those found by scanning its own repo.
	Callers []CallerTrace
	// ShapeInputs is what shape inference knew about the file's
	// neighbours, so `shape` and explain can re-run the inference (spec-shape §7).
	ShapeInputs *rules.ShapeInputs
	// Checkouts are the effective checkouts per job and step, wrappers
	// resolved (checkout-inference §3).
	Checkouts map[string]rules.Checkouts
}

// CallerTrace is one job that calls a reusable workflow.
type CallerTrace struct {
	// File is the caller as the user would name it: relative to the repo
	// root for local files, the display name for fetched ones.
	File string
	Job  string
	// Inherit is true when the job passes secrets: inherit.
	Inherit bool
}

// CalleeTrace is one resolved (or failed) reference.
type CalleeTrace struct {
	Name string
	File *workflow.File
	// How: local, linked, fetched, skipped, docker, dynamic, failed.
	How string
	Err string
	// Ref is the uses: text.
	Ref string
}

func (t *Trace) put(name string, ft *FileTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.Files[name] = ft
	t.mu.Unlock()
}

// Each calls fn for every traced file, in name order.
func (t *Trace) Each(fn func(name string, ft *FileTrace)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	names := make([]string, 0, len(t.Files))
	for n := range t.Files {
		names = append(names, n)
	}
	t.mu.Unlock()
	sort.Strings(names)
	for _, n := range names {
		fn(n, t.Get(n))
	}
}

// Get returns the trace for a file name.
func (t *Trace) Get(name string) *FileTrace {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Files[name]
}
