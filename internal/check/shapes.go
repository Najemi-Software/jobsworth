package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// checkShapes runs medium §6 over a file: it lifts output shapes from the
// file's callees (§6.2.11), records the JSON this file passes to each
// callee's inputs (§6.2.4), and checks every chain (§6.3). Caller-side
// typing of expression-valued with: (§5.2) rides along.
func (l *loader) checkShapes(cur *loaded, scopes map[string]*rules.Scope, stepCallees map[string][]rules.Callee, jobCallees map[string]*loaded) ([]finding.Finding, rules.ShapeInputs) {
	in := rules.ShapeInputs{
		JobCallee:       map[string]map[string]*shape.Shape{},
		StepCallee:      map[string]map[int]map[string]*shape.Shape{},
		CallerInputs:    l.callerInputs(identity(cur.src)),
		CallerSites:     l.callerSites(identity(cur.src)),
		CallerLiterals:  l.callerLiterals(identity(cur.src)),
		CallerEvents:    l.callerEventList(identity(cur.src)),
		GitHubEventOpen: l.cfg.ShapesGitHubEventOpen,
		ReadFile:        repoReader(cur.src),
	}
	if in.ReadFile == nil && l.cfg.ShapesFetchStaticFiles && l.fetcher != nil && !cur.src.local {
		in.ReadFile = fetchedReader(l, cur.src) // advanced §7.2, opt-in
	}
	for job, callee := range jobCallees {
		in.JobCallee[job] = l.outputShapesOf(callee)
	}
	for job, cs := range stepCallees {
		for _, c := range cs {
			if in.StepCallee[job] == nil {
				in.StepCallee[job] = map[int]map[string]*shape.Shape{}
			}
			if ld := l.loadedByKey(c.Key); ld != nil {
				in.StepCallee[job][c.Step] = l.outputShapesOf(ld)
			}
		}
	}
	var out []finding.Finding
	out = append(out, rules.CheckShapes(cur.file, scopes, in)...)
	out = append(out, rules.CheckMatrix(cur.file)...)

	// Step-level with: expressions against typed action inputs (§5.2, N1).
	for job, sc := range scopes {
		for _, st := range sc.Steps {
			callee := sc.Callees[st.Index]
			if callee == nil || st.Uses == "" {
				continue
			}
			shapes := rules.WithShapes(cur.file, scopes, in, job, st.With)
			out = append(out, rules.CheckStepExprTypes(sc, st, callee, sc.CalleeNames[st.Index], shapes)...)
		}
	}

	// Caller-supplied shapes and expression typing for reusable-workflow
	// calls (§5.2, §6.2.4).
	for _, j := range cur.file.Jobs {
		callee, ok := jobCallees[j.Name]
		if !ok || j.Uses == nil {
			continue
		}
		shapes := rules.WithShapes(cur.file, scopes, in, j.Name, j.With)
		l.recordCallerInputs(identity(callee.src), shapes, fmt.Sprintf("%s:%d (job %s)", callerName(cur.src), j.Uses.Pos.Line, j.Name), j.With, cur.file.Events)
		out = append(out, rules.CheckExprTypes(j, callee.file, j.Uses.Raw, shapes)...)
	}
	return out, in
}

// repoReader reads repo-relative files for a local source; nil for fetched
// files (their repos are not on disk).
func repoReader(src source) func(string) ([]byte, bool) {
	if !src.local || src.root == "" {
		return nil
	}
	root := src.root
	return func(p string) ([]byte, bool) {
		clean := filepath.Clean("/" + filepath.FromSlash(p))
		data, err := os.ReadFile(filepath.Join(root, clean))
		if err != nil {
			return nil, false
		}
		return data, true
	}
}

// outputShapesOf evaluates a callee's output shapes once per run. Callees
// are analysed before their own validation here, without their own callees
// (those lift as Any), which is the documented degrade.
func (l *loader) outputShapesOf(ld *loaded) map[string]*shape.Shape {
	key := identity(ld.src)
	l.consumedMu.Lock()
	if l.outputShapes == nil {
		l.outputShapes = map[string]map[string]*shape.Shape{}
	}
	if m, ok := l.outputShapes[key]; ok {
		l.consumedMu.Unlock()
		return m
	}
	l.consumedMu.Unlock()
	scopes := map[string]*rules.Scope{}
	if ld.file.Kind == workflow.KindWorkflow {
		for _, j := range ld.file.Jobs {
			if len(j.Steps) > 0 {
				scopes[j.Name] = rules.BuildScopeWith(ld.file, j.Name, nil, bashOptions(l))
			}
		}
	} else if len(ld.file.Steps) > 0 {
		scopes[""] = rules.BuildScopeWith(ld.file, "", nil, bashOptions(l))
	}
	in := rules.ShapeInputs{CallerInputs: l.callerInputs(key), GitHubEventOpen: l.cfg.ShapesGitHubEventOpen}
	m := rules.OutputShapes(ld.file, scopes, in)
	l.consumedMu.Lock()
	l.outputShapes[key] = m
	l.consumedMu.Unlock()
	return m
}

// recordCallerInputs intersects the JSON shapes a caller passes for each
// input of the callee with what earlier callers passed.
// fetchedReader reads repo files of a fetched source through the content
// cache at the callee's SHA.
func fetchedReader(l *loader, src source) func(string) ([]byte, bool) {
	return func(p string) ([]byte, bool) {
		data, err := fetch.Content(l.fetcher, src.owner, src.repo, src.sha, strings.TrimPrefix(p, "./"))
		return data, err == nil
	}
}

// callerLiterals returns every literal passed per input by walked callers.
func (l *loader) callerLiterals(key string) map[string][]string {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	out := map[string][]string{}
	for name, vs := range l.callerLits[key] {
		for _, v := range vs {
			if !v.IsExpr {
				out[name] = append(out[name], v.Value)
			}
		}
	}
	return out
}

// callerEventList returns the triggers of the walked callers.
func (l *loader) callerEventList(key string) []string {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	return l.callerEvents[key]
}

func (l *loader) recordCallerInputs(key string, with map[string]*shape.Shape, site string, values []workflow.With, callerEvents []string) {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	if l.callerShapes == nil {
		l.callerShapes = map[string]map[string]*shape.Shape{}
		l.callerSiteList = map[string]map[string][]string{}
	}
	if l.callerShapes[key] == nil {
		l.callerShapes[key] = map[string]*shape.Shape{}
		l.callerSiteList[key] = map[string][]string{}
	}
	literal := map[string]string{}
	if l.callerLits == nil {
		l.callerLits = map[string]map[string][]rules.CallerValue{}
	}
	if l.callerLits[key] == nil {
		l.callerLits[key] = map[string][]rules.CallerValue{}
	}
	for _, w := range values {
		literal[w.Name] = w.Value.Value
		l.callerLits[key][w.Name] = append(l.callerLits[key][w.Name], rules.CallerValue{Site: site, Value: w.Value.Value, IsExpr: w.Value.IsExpr()})
	}
	if l.callerEvents == nil {
		l.callerEvents = map[string][]string{}
	}
	l.callerEvents[key] = append(l.callerEvents[key], callerEvents...)
	for name, s := range with {
		if s == nil || s.Kind != shape.String || s.JSON == nil {
			continue
		}
		text := literal[name]
		if len(text) > 60 {
			text = text[:59] + "…"
		}
		l.callerSiteList[key][name] = append(l.callerSiteList[key][name], site+": "+text)
		if prev, ok := l.callerShapes[key][name]; ok {
			l.callerShapes[key][name] = shape.Intersect(prev, s.JSON)
		} else {
			l.callerShapes[key][name] = s.JSON
		}
	}
}

func (l *loader) callerInputs(key string) map[string]*shape.Shape {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	return l.callerShapes[key]
}

// callerValues returns the literal with: values every walked caller
// passed per input, and how many distinct callers contributed.
func (l *loader) callerValues(key string) (map[string][]rules.CallerValue, int) {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	sites := map[string]bool{}
	for _, vs := range l.callerLits[key] {
		for _, v := range vs {
			sites[v.Site] = true
		}
	}
	return l.callerLits[key], len(sites)
}

func (l *loader) callerSites(key string) map[string][]string {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	return l.callerSiteList[key]
}

// loadedByKey finds a loaded callee by identity.
func (l *loader) loadedByKey(key string) *loaded {
	v, ok := l.byKey.Load(key)
	if !ok {
		return nil
	}
	return v.(*loaded)
}

// rememberLoaded indexes a loaded file by identity for lift-up lookups.
func (l *loader) rememberLoaded(ld *loaded) {
	if ld != nil {
		l.byKey.Store(identity(ld.src), ld)
	}
}
