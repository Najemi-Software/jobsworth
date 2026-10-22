package check

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/enrich"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/suppress"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Options configures one entry's run. Links and Fetcher are shared across
// entries (§1.1); Config is the entry repo's config (§9).
type Options struct {
	Links   *repolink.Links
	Fetcher *fetch.Client
	Config  config.Config
	// RootOverride forces the repo root for the entry's ./ references
	// instead of detecting it (test spec §1.1).
	RootOverride string
	// LoadOnly walks and loads everything reachable but runs no rules
	// (spec-cache §2.4 warm): only loader findings are produced.
	LoadOnly bool
	// SelfSlug overrides the entry repo's own owner/repo (normally read
	// from the git origin), for tests of checkout tracking.
	SelfSlug string
	// Trace, when set, receives each validated file's analysis.
	Trace *Trace
	// Graph, when set, records the traversal (spec-graph). GraphWith adds
	// each edge's with: keys. NoFetch makes external references
	// unresolved nodes instead of fetching them. Depth, when set, stops
	// the walk that many levels below the entry.
	Graph     *Graph
	GraphWith bool
	NoFetch   bool
	Depth     *int
	// ParseMemo, when set, shares parsed callees across entries.
	ParseMemo *ParseMemo
	// Inputs seeds the entry's workflow_dispatch inputs for condition
	// evaluation (advanced §4.3, `check --input k=v`).
	Inputs map[string]string
	// Enrich enables the read-only API lookups of advanced §11; Store is
	// the cache they persist in.
	Enrich bool
	Store  *cache.Cache
	// FollowRef, when set, decides which of the entry's own references are
	// followed; the rest are left unloaded as if cut by depth. The shape
	// command uses it to fetch only what an expression can touch
	// (spec-shape §7). Callees reached are still walked in full.
	FollowRef func(u workflow.Uses) bool
}

// FileResult is the findings for one file reached in the walk. Name is the
// absolute path (local) or display name (fetched).
type FileResult struct {
	Name     string            `json:"file"`
	Findings []finding.Finding `json:"findings"`
	// Suppressed holds findings hidden by inline jobsworth-ignore comments
	// (spec-rules §5.3); they never count toward the exit code.
	Suppressed []finding.Finding `json:"suppressed,omitempty"`
}

// Result is one entry's outcome: the entry's own findings first, then each
// callee in walk order (§8.1).
type Result struct {
	Entry string
	Files []FileResult
}

// AllFindings flattens a result's kept findings in file order.
func AllFindings(r Result) []finding.Finding {
	var out []finding.Finding
	for _, f := range r.Files {
		out = append(out, f.Findings...)
	}
	return out
}

// AllSuppressed flattens a result's suppressed findings in file order.
func AllSuppressed(r Result) []finding.Finding {
	var out []finding.Finding
	for _, f := range r.Files {
		out = append(out, f.Suppressed...)
	}
	return out
}

// Failed reports whether any finding reaches the threshold (§1.3).
func Failed(r Result, threshold finding.Severity) bool {
	for _, f := range AllFindings(r) {
		if finding.AtOrAbove(f, threshold) {
			return true
		}
	}
	return false
}

// RunEntry validates one entry and everything reachable from it (§3.2).
func RunEntry(entry string, opts Options) Result {
	if opts.Config.Schema == 0 {
		// A zero Config means "no config": the built-in defaults apply
		// (github_event_open: true, default TTLs, ...).
		opts.Config = config.Defaults()
	}
	l := &loader{links: opts.Links, fetcher: opts.Fetcher, cfg: opts.Config, loadOnly: opts.LoadOnly, selfSlug: opts.SelfSlug, trace: opts.Trace, followRef: opts.FollowRef,
		graph: opts.Graph, withKeys: opts.GraphWith, noFetch: opts.NoFetch, maxDepth: -1, parsed: opts.ParseMemo, inputs: opts.Inputs}
	if opts.Depth != nil {
		l.maxDepth = *opts.Depth
	}
	if l.parsed == nil {
		l.parsed = NewParseMemo() // before any concurrent follow touches it
	}
	if opts.Enrich && opts.Fetcher != nil {
		l.enricher = enrich.New(opts.Fetcher, opts.Store, opts.Config.EnrichTTL)
		l.enricher.Actions = opts.Config.EnrichActions
	}
	res := Result{Entry: entry}
	fileFindings := map[string]*FileResult{}
	var order []string
	get := func(name string) *FileResult {
		fr, ok := fileFindings[name]
		if !ok {
			fr = &FileResult{Name: name}
			fileFindings[name] = fr
			order = append(order, name)
		}
		return fr
	}
	collect := func(name string, kept, suppressed []finding.Finding) {
		fr := get(name)
		fr.Findings = append(fr.Findings, kept...)
		fr.Suppressed = append(fr.Suppressed, suppressed...)
	}

	root, errs := l.loadEntry(entry, opts.RootOverride)
	collect(entry, errs, nil)
	if root != nil {
		if l.graph != nil {
			l.graph.recordEntry(l, root)
		}
		walk(l, root, collect)
	}

	stamp := func(name string, fs []finding.Finding) []finding.Finding {
		fs = rules.ApplyEffective(opts.Config, fs)
		for i := range fs {
			fs[i].File = name
		}
		finding.Sort(fs)
		return fs
	}
	for _, name := range order {
		fr := fileFindings[name]
		fr.Findings = stamp(name, fr.Findings)
		fr.Suppressed = stamp(name, fr.Suppressed)
		res.Files = append(res.Files, *fr)
	}
	return res
}

// walk is the breadth-first traversal over a worklist. Each popped file is
// validated on its own (§7.1, §7.3), its callees are loaded in parallel,
// caller-side checks run against the loaded declarations (§7.2, §7.4), and
// the callees are queued.
func walk(l *loader, root *loaded, collect func(string, []finding.Finding, []finding.Finding)) {
	type item struct {
		ld    *loaded
		depth int
	}
	visited := map[string]bool{}
	queue := []item{{root, 0}}
	for len(queue) > 0 {
		cur, depth := queue[0].ld, queue[0].depth
		queue = queue[1:]
		id := identity(cur.src)
		if visited[id] {
			continue
		}
		visited[id] = true
		name := display(cur.src)
		cut := l.maxDepth >= 0 && depth >= l.maxDepth

		var own []finding.Finding
		if !l.loadOnly {
			own = append(own, rules.CheckInputs(cur.file)...)
			if cur.file.Kind == workflow.KindWorkflow {
				own = append(own, rules.CheckJobGraph(cur.file)...)
				own = append(own, rules.CheckTriggers(cur.file, triggerInputs(l, cur))...)
			}
			own = append(own, rules.CheckStructure(cur.file, structureInputs(l, cur))...)
		}

		// Phase 1: external references. A job's checkout state depends on
		// the bodies of its external callees (checkout-inference §3.1), so
		// they are followed before the ./ references are resolved.
		keep := func(u workflow.Uses) bool { return depth > 0 || l.followRef == nil || l.followRef(u) }
		results := make([]loadResult, len(cur.file.Uses))
		if !cut {
			results = followSome(l, cur.src, cur.file.Uses, nil, func(u workflow.Uses) bool { return !isLocalRef(u.Raw) && keep(u) })
		}
		// Checkout tracking (medium §3) decides where each job's ./
		// references resolve. It also runs when only loading (warm, graph)
		// so the set of files reached is the same as check's (spec-graph
		// §6); only the findings are dropped then.
		var checkoutFindings []finding.Finding
		overrides := map[int]*rules.CheckoutContext{}
		jobCheckouts := map[string]rules.Checkouts{}
		if !cut {
			self := selfSlug(l, cur.src)
			for _, j := range jobsOrAction(cur.file) {
				cos := l.checkoutsFor(cur, j, results)
				jobCheckouts[j] = cos
				if cur.file.Kind != workflow.KindWorkflow {
					continue
				}
				fs, ctx, subdirs := rules.CheckCheckoutSubdirs(cur.file, j, self, localRoot(cur.src), cos)
				if !l.loadOnly {
					checkoutFindings = append(checkoutFindings, fs...)
				}
				for i, u := range cur.file.Uses {
					if u.Job != j || u.JobLevel {
						continue
					}
					if idx, ok := stepIndexOfUses(cur.file, u); ok && subdirs[idx] != nil {
						overrides[i] = subdirs[idx] // provided by a subdirectory checkout (review 2 D7)
						continue
					}
					if ctx != nil && ctx.Repository != "" {
						overrides[i] = ctx
					}
				}
			}
		}
		// Phase 2: local references, through the checkout overrides.
		if !cut {
			local := followSome(l, cur.src, cur.file.Uses, overrides, func(u workflow.Uses) bool { return isLocalRef(u.Raw) && keep(u) })
			for i, u := range cur.file.Uses {
				if isLocalRef(u.Raw) {
					results[i] = local[i]
				}
			}
		}
		own = append(own, checkoutFindings...)
		if l.graph != nil {
			l.graph.record(l, cur, results, overrides, cut, l.withKeys)
		}

		calleeOutputs := map[string]rules.JobOutputs{}
		stepCallees := map[string][]rules.Callee{}
		jobCallees := map[string]*loaded{}
		calleeSrc := map[*workflow.File]source{} // for multi-hop taint (advanced §1.2)
		ft := &FileTrace{File: cur.file, Raw: cur.raw, How: howLoaded(cur.src, l), JobCallees: map[string]*CalleeTrace{}, StepCallees: map[string]map[int]*CalleeTrace{}, Suppressions: cur.sup, Checkouts: jobCheckouts}
		for i, u := range cur.file.Uses {
			r := results[i]
			own = append(own, r.callerFindings...)
			ct := calleeTrace(u, r)
			if r.callee != nil {
				calleeSrc[r.callee.file] = r.callee.src
			}
			if u.JobLevel {
				ft.JobCallees[u.Job] = ct
			} else if idx, ok := stepIndexOfUses(cur.file, u); ok {
				if ft.StepCallees[u.Job] == nil {
					ft.StepCallees[u.Job] = map[int]*CalleeTrace{}
				}
				ft.StepCallees[u.Job][idx] = ct
			}
			if len(r.calleeFindings) > 0 {
				collect(r.calleeName, r.calleeFindings, nil)
			}
			if !u.JobLevel {
				if r.callee != nil {
					if idx, ok := stepIndexOfUses(cur.file, u); ok {
						name := r.calleeName
						if r.callee.src.local {
							name = u.Raw // what the user wrote reads better than an absolute path
						}
						stepCallees[u.Job] = append(stepCallees[u.Job], rules.Callee{Step: idx, File: r.callee.file, Name: name, Key: identity(r.callee.src)})
					}
				}
				continue
			}
			job := workflow.JobByName(cur.file, u.Job)
			if r.callee != nil && job != nil {
				l.recordCaller(identity(r.callee.src), CallerTrace{File: callerName(cur.src), Job: job.Name, Inherit: job.SecretsInherit})
			}
			if r.callee == nil || job == nil || l.loadOnly {
				continue
			}
			own = append(own, rules.CheckCallInputs(*job, r.callee.file, r.calleeName)...)
			own = append(own, rules.CheckCallSecrets(*job, r.callee.file, r.calleeName)...)
			if job.SecretsInherit {
				l.markInherit(identity(r.callee.src))
			}
			jobCallees[u.Job] = r.callee
			jo := rules.JobOutputs{Known: true}
			for _, o := range r.callee.file.Outputs {
				jo.Names = append(jo.Names, o.Name)
			}
			calleeOutputs[u.Job] = jo
		}
		if cur.file.Kind == workflow.KindWorkflow && !l.loadOnly {
			own = append(own, rules.CheckJobOutputs(cur.file, calleeOutputs)...)
		}
		if !l.loadOnly {
			scopeFindings, scopes := checkScopes(l, cur.file, stepCallees, bashOptions(l))
			ft.Scopes = scopes
			own = append(own, scopeFindings...)
			shapeFindings, shapeIn := l.checkShapes(cur, scopes, stepCallees, jobCallees)
			own = append(own, shapeFindings...)
			ft.ShapeInputs = &shapeIn
			own = append(own, actionOutputUnused(l, cur)...)
			own = append(own, rules.CheckSecrets(cur.file, l.inherited(id) || inheritedBySibling(cur), len(knownCallers(l, cur, id)) > 0)...)
			own = append(own, rules.CheckValueSets(cur.file)...)
			own = append(own, rules.CheckChoices(cur.file)...)
			own = append(own, rules.CheckIfs(cur.file)...)
			own = append(own, rules.CheckSecurity(cur.file, scopes, securityInputs(l, cur, jobCallees, jobCheckouts, calleeSrc))...)
			own = append(own, rules.CheckConditions(cur.file, conditionInputs(l, cur, id))...)
			own = append(own, rules.CheckReachability(cur.file, scopes)...)
			own = append(own, rules.CheckPerformance(cur.file, scopes, rules.PerformanceInputs{FSCleanActions: l.cfg.PerformanceFSClean, Checkouts: jobCheckouts})...)
			own = append(own, rules.CheckScripts(cur.file, scopes, rules.ScriptInputs{
				ShellcheckEnabled: l.cfg.ShellcheckEnabled, ShellcheckPath: l.cfg.ShellcheckPath, ShellcheckMinSeverity: l.cfg.ShellcheckMinSeverity,
				ShellcheckExtraArgs: l.cfg.ShellcheckExtraArgs, Python: l.cfg.ScriptsPython, Pwsh: l.cfg.ScriptsPwsh,
			})...)
		}
		if l.enricher != nil && !l.loadOnly {
			own = l.enricher.Apply(cur.file, own, enrichInputs(l, cur))
		}
		kept, suppressed := suppress.Apply(cur.sup, own)
		collect(name, kept, suppressed)
		if l.trace != nil && cur.file.Kind == workflow.KindWorkflow && cur.file.HasWorkflowCall {
			ft.Callers = knownCallers(l, cur, id)
		}
		l.trace.put(name, ft)

		for _, r := range results {
			if r.callee != nil && !visited[identity(r.callee.src)] {
				queue = append(queue, item{r.callee, depth + 1})
			}
		}
	}
}

// RunRef validates one raw external reference (e.g. "owner/repo/path@ref")
// as the entry, so explain can take a fetched display name as a location
// (spec-explain §7). Findings are attributed to display names.
func RunRef(raw string, opts Options) Result {
	if opts.Config.Schema == 0 {
		opts.Config = config.Defaults()
	}
	return runRef(raw, opts, false)
}

// WarmRef follows one raw external reference (e.g. "actions/checkout@v4")
// as if a workflow used it, loading it and everything it reaches
// (spec-cache §6). The returned findings are the loader's.
func WarmRef(raw string, opts Options) Result {
	return runRef(raw, opts, true)
}

func runRef(raw string, opts Options, loadOnly bool) Result {
	l := &loader{links: opts.Links, fetcher: opts.Fetcher, cfg: opts.Config, loadOnly: loadOnly, trace: opts.Trace, maxDepth: -1, parsed: NewParseMemo()}
	res := Result{Entry: raw}
	u := workflow.Uses{Raw: raw, JobLevel: strings.HasSuffix(strings.SplitN(raw, "@", 2)[0], ".yml") || strings.HasSuffix(strings.SplitN(raw, "@", 2)[0], ".yaml")}
	r := l.follow(source{local: true, root: ""}, u)
	fr := FileResult{Name: raw, Findings: r.callerFindings}
	for i := range fr.Findings {
		fr.Findings[i].File = raw
	}
	res.Files = append(res.Files, fr)
	if len(r.calleeFindings) > 0 {
		res.Files = append(res.Files, FileResult{Name: r.calleeName, Findings: r.calleeFindings})
	}
	if r.callee != nil {
		collect := func(name string, kept, suppressed []finding.Finding) {
			kept = rules.ApplyEffective(opts.Config, kept)
			for i := range kept {
				kept[i].File = name
			}
			for i := range suppressed {
				suppressed[i].File = name
			}
			finding.Sort(kept)
			res.Files = append(res.Files, FileResult{Name: name, Findings: kept, Suppressed: suppressed})
		}
		walk(l, r.callee, collect)
	}
	return res
}

// enrichInputs names the file's repository for the --enrich lookups.
func enrichInputs(l *loader, cur *loaded) enrich.Inputs {
	in := enrich.Inputs{Local: cur.src.local}
	slug := selfSlug(l, cur.src)
	if i := strings.IndexByte(slug, '/'); i > 0 {
		in.Owner, in.Repo = slug[:i], slug[i+1:]
	}
	return in
}

// conditionInputs is what the conditions family knows about callers and
// seeds (advanced §4.3).
func conditionInputs(l *loader, cur *loaded, id string) rules.ConditionInputs {
	values, walked := l.callerValues(id)
	// Only a local callee's caller set can be complete: the repository
	// scan finds its local callers; a fetched file's callers elsewhere
	// are unknown by construction (medium review 7 D15).
	in := rules.ConditionInputs{CallerValues: values, WalkedCallers: walked, Complete: cur.src.local}
	in.KnownCallers = len(knownCallers(l, cur, id))
	if in.KnownCallers < walked {
		in.KnownCallers = walked
	}
	if cur.src.local && cur.src.path == l.entryPath && len(l.inputs) > 0 {
		in.Seeded = l.inputs
	}
	return in
}

// securityInputs is what the security family knows about the repo.
func securityInputs(l *loader, cur *loaded, jobCallees map[string]*loaded, checkouts map[string]rules.Checkouts, calleeSrc map[*workflow.File]source) rules.SecurityInputs {
	in := rules.SecurityInputs{
		Self: selfSlug(l, cur.src), TrustedOwners: l.cfg.SecurityTrustedOwners, OIDCActions: l.cfg.SecurityOIDCActions,
		Strict: l.cfg.SecurityStrict, SelfHostedLabels: l.cfg.SelfHostedLabels, Checkouts: checkouts,
		JobCallees: map[string]*workflow.File{},
	}
	// Resolve follows a callee's own uses: through the walker (cache,
	// links), so with: forwarding is traced across hops.
	in.Resolve = func(callee *workflow.File, raw string) *workflow.File {
		src, ok := calleeSrc[callee]
		if !ok {
			return nil
		}
		r := l.follow(src, workflow.Uses{Raw: raw})
		if r.callee == nil {
			return nil
		}
		calleeSrc[r.callee.file] = r.callee.src
		return r.callee.file
	}
	for job, ld := range jobCallees {
		in.JobCallees[job] = ld.file
	}
	return in
}

// structureInputs is what the structure family knows about the repo.
func structureInputs(l *loader, cur *loaded) rules.StructureInputs {
	in := rules.StructureInputs{RunnerLabels: l.cfg.StructureRunnerLabels, SelfHostedLabels: l.cfg.SelfHostedLabels, Path: cur.src.path}
	if cur.src.local && cur.src.root != "" {
		in.Workflows = consumers.namedWorkflows(cur.src.root)
	}
	return in
}

// triggerInputs is what the triggers family knows about the repo.
func triggerInputs(l *loader, cur *loaded) rules.TriggerInputs {
	in := rules.TriggerInputs{ExtraEvents: l.cfg.TriggersExtraEvents}
	if cur.src.local && cur.src.root != "" {
		in.HasRepo = true
		in.WorkflowNames = consumers.workflowNames(cur.src.root)
		if cur.file.Name.Value != "" {
			in.WorkflowNames = append(in.WorkflowNames, cur.file.Name.Value)
		}
	}
	return in
}

// callerName is how a caller is shown in a callee's trace: relative to
// its repo root for local files, the display name otherwise.
func callerName(src source) string {
	if src.local && src.root != "" {
		if rel, err := filepath.Rel(src.root, src.path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return display(src)
}

// knownCallers merges the callers seen in this walk with those found by
// scanning the file's own repository (explain review N1), deduplicated
// by file and job.
func knownCallers(l *loader, cur *loaded, id string) []CallerTrace {
	var out []CallerTrace
	seen := map[string]bool{}
	add := func(c CallerTrace) {
		k := c.File + "\x00" + c.Job
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	for _, c := range l.callersOf(id) {
		add(c)
	}
	if cur.src.local && cur.src.root != "" {
		rel, err := filepath.Rel(cur.src.root, cur.src.path)
		underRoot := err == nil && !strings.HasPrefix(rel, "..")
		for path, cs := range consumers.callersByRoot(cur.src.root) {
			if (underRoot && path == cur.src.path) || (!underRoot && filepath.Base(path) == filepath.Base(cur.src.path)) {
				for _, c := range cs {
					add(c)
				}
			}
		}
	}
	return out
}

// inheritedBySibling reports whether some caller in the file's own repo
// invokes it with secrets: inherit (medium review D6), so a reusable
// workflow checked as its own entry gets the same severity as through its
// callers.
func inheritedBySibling(cur *loaded) bool {
	if !cur.src.local || cur.src.root == "" {
		return false
	}
	rel, err := filepath.Rel(cur.src.root, cur.src.path)
	underRoot := err == nil && !strings.HasPrefix(rel, "..")
	for p := range consumers.inheritCallers(cur.src.root) {
		if underRoot && p == cur.src.path {
			return true
		}
		// An entry checked from outside its root (the fixture corpus
		// keeps entries beside, not under, the root) matches by name.
		if !underRoot && filepath.Base(p) == filepath.Base(cur.src.path) {
			return true
		}
	}
	return false
}

// howLoaded names a source for the trace.
func howLoaded(src source, l *loader) string {
	switch {
	case !src.local:
		return "fetched"
	case l.links != nil && linkedRoot(l, src.root):
		return "linked"
	}
	return "local"
}

// linkedRoot reports whether root is a linked checkout other than the
// entry's own repository (whose implicit link does not make its files
// "linked"; spec-graph review D1).
func linkedRoot(l *loader, root string) bool {
	if root == l.entryRoot {
		return false
	}
	for _, lk := range repolink.All(l.links) {
		if lk.Path == root {
			return true
		}
	}
	return false
}

// calleeTrace summarises one followed reference.
func calleeTrace(u workflow.Uses, r loadResult) *CalleeTrace {
	ct := &CalleeTrace{Ref: u.Raw, Name: r.calleeName}
	switch {
	case r.callee != nil:
		ct.File = r.callee.file
		ct.How = "local"
		if !r.callee.src.local {
			ct.How = "fetched"
		}
	case len(r.callerFindings) > 0:
		ct.How = "failed"
		ct.Err = r.callerFindings[0].Message
		if r.callerFindings[0].Rule == "uses-dynamic" {
			ct.How = "dynamic"
		}
	case r.calleeName != "":
		ct.How = "failed"
		if len(r.calleeFindings) > 0 {
			ct.Err = r.calleeFindings[0].Message
		}
	default:
		ct.How = "skipped"
		if strings.HasPrefix(u.Raw, "docker://") {
			ct.How = "docker"
		}
	}
	return ct
}

// checkScopes runs the per-scope families (medium §1, §2) over every job
// body and the composite action body.
func checkScopes(l *loader, f *workflow.File, stepCallees map[string][]rules.Callee, bo bash.Options) ([]finding.Finding, map[string]*rules.Scope) {
	var out []finding.Finding
	scopes := map[string]*rules.Scope{}
	run := func(job string) {
		steps := workflow.StepsOf(f, job)
		if len(steps) == 0 {
			return
		}
		sc := rules.BuildScopeWith(f, job, stepCallees[job], bo)
		scopes[job] = sc
		for key, outs := range sc.ConsumedOutputs() {
			l.consume(key, outs)
		}
		out = append(out, rules.CheckSteps(sc)...)
		out = append(out, rules.CheckStepContinue(sc)...)
		out = append(out, rules.CheckBash(sc)...)
		for _, st := range steps {
			out = append(out, rules.CheckStepInputs(sc, st, sc.Callees[st.Index], sc.CalleeNames[st.Index])...)
		}
	}
	if f.Kind == workflow.KindWorkflow {
		for _, j := range f.Jobs {
			run(j.Name)
		}
	} else if f.Kind == workflow.KindCompositeAction {
		run("")
	}
	return out, scopes
}

// bashOptions is the analyser configuration from the entry's config.
func bashOptions(l *loader) bash.Options {
	return bash.Options{Opaque: l.cfg.BashOpaqueCommands, Runners: l.cfg.BashRunnerCommands}
}

// stepIndexOfUses finds the step a step-level uses: belongs to by position.
func stepIndexOfUses(f *workflow.File, u workflow.Uses) (int, bool) {
	for _, st := range workflow.StepsOf(f, u.Job) {
		if st.Uses == u.Raw && st.UsesPos == u.Pos {
			return st.Index, true
		}
	}
	return 0, false
}

// jobsOrAction names the scopes of a file: its jobs, or "" for an action.
func jobsOrAction(f *workflow.File) []string {
	if f.Kind != workflow.KindWorkflow {
		return []string{""}
	}
	var out []string
	for _, j := range f.Jobs {
		out = append(out, j.Name)
	}
	return out
}

// followSome follows only the references keep accepts; the others get an
// empty result, as a depth cut would.
func followSome(l *loader, src source, refs []workflow.Uses, overrides map[int]*rules.CheckoutContext, keep func(workflow.Uses) bool) []loadResult {
	var wanted []workflow.Uses
	var idx []int
	wantedOverrides := map[int]*rules.CheckoutContext{}
	for i, u := range refs {
		if keep(u) {
			if ctx := overrides[i]; ctx != nil {
				wantedOverrides[len(wanted)] = ctx
			}
			wanted = append(wanted, u)
			idx = append(idx, i)
		}
	}
	out := make([]loadResult, len(refs))
	for j, r := range followAllWith(l, src, wanted, wantedOverrides) {
		out[idx[j]] = r
	}
	return out
}

// followAll loads every reference of a file concurrently, preserving order.
// The fetch client bounds in-flight requests (§5.7).
func followAll(l *loader, src source, refs []workflow.Uses) []loadResult {
	return followAllWith(l, src, refs, nil)
}

// followAllWith is followAll with per-reference checkout overrides: a ./
// reference in a job whose root checkout is another repository resolves
// against that repository (medium §3.2).
func followAllWith(l *loader, src source, refs []workflow.Uses, overrides map[int]*rules.CheckoutContext) []loadResult {
	out := make([]loadResult, len(refs))
	var wg sync.WaitGroup
	for i, u := range refs {
		wg.Add(1)
		go func(i int, u workflow.Uses) {
			defer wg.Done()
			if ctx := overrides[i]; ctx != nil {
				out[i] = l.followVia(src, u, ctx)
				return
			}
			out[i] = l.follow(src, u)
		}(i, u)
	}
	wg.Wait()
	return out
}

// selfSlug is the file's own owner/repo: the remote context for fetched
// files, the git origin for local ones ("" when unknown).
func selfSlug(l *loader, src source) string {
	if !src.local {
		return src.owner + "/" + src.repo
	}
	if l.selfSlug != "" {
		return l.selfSlug
	}
	if lk, ok := repolink.Detect(src.root, repolink.Entry); ok {
		return lk.Display
	}
	return ""
}

func localRoot(src source) string {
	if src.local {
		return src.root
	}
	return ""
}
