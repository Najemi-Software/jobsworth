package check

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/enrich"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/suppress"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// source says where a file came from and how its own ./ references resolve
// (§3.1 "repo root", §4.3, §5.4).
type source struct {
	// local is true for files read from disk (entries, local refs, and
	// linked checkouts). path is the absolute file path; root is the repo
	// root for ./ references.
	local bool
	path  string
	root  string
	// Remote context: owner/repo at sha, path within the repo, and the ref
	// as written (for display names). disp is the path as the user wrote
	// it in uses: (the action directory, or the workflow file).
	owner, repo, sha, ref, rpath, disp string
	// Graph metadata (spec-graph §3.1), not part of the identity: how the
	// ref was classified (tag, branch, sha, default-branch) and how the
	// ref cache answered (hit, miss, stale, bypassed).
	refKind, cacheStatus string
}

// refKindName maps a cache ref kind to the graph vocabulary.
func refKindName(k cache.RefKind) string {
	if k == cache.Commit {
		return "sha"
	}
	return k.String()
}

// identity keys the visited set (§3.3).
func identity(s source) string {
	if s.local {
		return "local:" + s.path
	}
	return "remote:" + strings.ToLower(s.owner+"/"+s.repo) + "@" + s.sha + "/" + s.rpath
}

// display is the name findings are attributed to (§3.4): the absolute path
// for local files, owner/repo[/path]@ref for fetched ones, where path is
// what the user wrote (the action directory, not its action.yml).
func display(s source) string {
	if s.local {
		return s.path
	}
	name := s.owner + "/" + s.repo
	if s.disp != "" {
		name += "/" + s.disp
	}
	return name + "@" + s.ref
}

// loaded is a parsed file with its source and inline suppressions.
type loaded struct {
	src  source
	file *workflow.File
	sup  *suppress.Set
	raw  []byte
}

// canonical maps a rule alias to its id for suppression directives.
func canonical(id string) string {
	if _, c, ok := rules.Lookup(id); ok {
		return c
	}
	return id
}

// loader bundles the run-wide collaborators a load needs.
type loader struct {
	links    *repolink.Links
	fetcher  *fetch.Client
	cfg      config.Config
	loadOnly bool
	// parsed memoises parse results per identity so a callee referenced
	// from N places is parsed once and its own findings reported once.
	// It may be shared across entries (ParseMemo) by scans that load many
	// files and ignore findings.
	parsed *ParseMemo
	// entryRoot is the entry's repository root: a link registered for it
	// (the implicit cwd/entry link) does not make its files "linked".
	entryRoot string
	// readFile is os.ReadFile unless a test injects one.
	readFile func(string) ([]byte, error)
	// consumed records, per callee identity, the outputs some caller in
	// this walk referenced (medium §1.6); callers are processed before
	// their callees, so it is complete when the callee is validated.
	consumed       map[string]map[string]bool
	inherit        map[string]bool
	callers        map[string][]CallerTrace // callee identity -> callers seen in this walk
	outputShapes   map[string]map[string]*shape.Shape
	callerShapes   map[string]map[string]*shape.Shape
	callerSiteList map[string]map[string][]string
	callerLits     map[string]map[string][]rules.CallerValue // callee identity -> input -> values
	callerEvents   map[string][]string                       // callee identity -> callers' triggers
	// entryPath is the entry file of this walk, for --input seeds.
	entryPath  string
	byKey      sync.Map // identity -> *loaded
	consumedMu sync.Mutex
	selfSlug   string
	trace      *Trace
	// graph, when set, records the walk (spec-graph); withKeys adds each
	// edge's with: keys. noFetch turns external references into
	// unresolved nodes instead of fetching. maxDepth < 0 is unlimited.
	graph    *Graph
	withKeys bool
	noFetch  bool
	maxDepth int
	// inputs are the --input k=v seeds for the entry (advanced §4.3).
	inputs map[string]string
	// enricher makes the --enrich lookups (advanced §11); nil when off.
	enricher *enrich.Client
	// followRef filters the entry's own references (Options.FollowRef).
	followRef func(u workflow.Uses) bool
}

func (l *loader) consume(key string, outs map[string]bool) {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	if l.consumed == nil {
		l.consumed = map[string]map[string]bool{}
	}
	if l.consumed[key] == nil {
		l.consumed[key] = map[string]bool{}
	}
	for o := range outs {
		l.consumed[key][o] = true
	}
}

// markInherit records that a caller passes secrets: inherit to the callee.
func (l *loader) markInherit(key string) {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	if l.inherit == nil {
		l.inherit = map[string]bool{}
	}
	l.inherit[key] = true
}

// recordCaller remembers that a job in caller invokes the callee.
func (l *loader) recordCaller(key string, ct CallerTrace) {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	if l.callers == nil {
		l.callers = map[string][]CallerTrace{}
	}
	l.callers[key] = append(l.callers[key], ct)
}

func (l *loader) callersOf(key string) []CallerTrace {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	return l.callers[key]
}

func (l *loader) inherited(key string) bool {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	return l.inherit[key]
}

// followVia resolves a ./ reference against another repository: the one a
// job's root checkout fetched (medium §3.2). External references are
// unaffected.
func (l *loader) followVia(src source, u workflow.Uses, ctx *rules.CheckoutContext) loadResult {
	ref, err := uses.Parse(u.Raw)
	if err != nil || ref.Kind != uses.Local {
		return l.follow(src, u)
	}
	if ctx.Subdir != "" {
		// Through a subdirectory checkout: the path inside that checkout.
		ref.Path = strings.TrimPrefix(strings.TrimPrefix(ref.Path, ctx.Subdir), "/")
	}
	if ctx.Repository == "" {
		// The checkout is of the file's own repository.
		if ctx.Subdir == "" {
			return l.follow(src, u)
		}
		if src.local {
			return l.loadLocal(src.root, ref, u, "")
		}
		return withMeta(l.loadRemote(src.owner, src.repo, src.sha, src.ref, ref, u), src.refKind, src.cacheStatus)
	}
	parts := strings.SplitN(ctx.Repository, "/", 2)
	if len(parts) != 2 || l.fetcher == nil {
		return l.follow(src, u)
	}
	owner, repo := parts[0], parts[1]
	if link, ok := repolink.Lookup(l.links, owner, repo); ok {
		return l.loadLocal(link.Path, ref, u, link.Path)
	}
	if l.noFetch {
		return unresolvedResult(u)
	}
	refName := ctx.Ref
	kindName := ""
	if refName == "" {
		kindName = "default-branch"
		b, err := fetch.DefaultBranch(l.fetcher, owner, repo)
		if err != nil {
			return loadResult{callerFindings: []finding.Finding{fetchFinding(u, err)}}
		}
		refName = b
	}
	r, err := fetch.ResolveRef(l.fetcher, owner, repo, refName)
	if err != nil {
		return loadResult{callerFindings: []finding.Finding{fetchFinding(u, err)}}
	}
	if kindName == "" {
		kindName = refKindName(r.Kind)
	}
	return withMeta(l.loadRemote(owner, repo, r.SHA, refName, ref, u), kindName, r.Cache)
}

// withMeta stamps graph metadata on a loaded remote file.
func withMeta(r loadResult, refKind, cacheStatus string) loadResult {
	if r.callee != nil {
		r.callee.src.refKind = refKind
		if cacheStatus != "" {
			// A resolved ref's status wins; a literal SHA keeps the
			// content cache's answer set by loadRemote.
			r.callee.src.cacheStatus = cacheStatus
		}
	}
	return r
}

// unresolvedResult is an external reference under --no-fetch.
func unresolvedResult(u workflow.Uses) loadResult {
	return loadResult{how: "unresolved", err: "not fetched (--no-fetch)", callerFindings: []finding.Finding{callerFinding("fetch-failed", u,
		fmt.Sprintf("uses: %q: fetching is disabled", u.Raw))}}
}

func (l *loader) consumedBy(key string) map[string]bool {
	l.consumedMu.Lock()
	defer l.consumedMu.Unlock()
	return l.consumed[key]
}

// ParseMemo memoises parsed callees across entries. Pass one in
// Options.ParseMemo when walking many entries of one repository and
// ignoring findings (a callee's own parse error is reported once per memo).
type ParseMemo struct {
	m sync.Map
	// profiles memoises checkout wrapper profiles by callee identity
	// (checkout-inference §3.5); localFiles memoises local action files
	// read for profiling by path.
	profiles   sync.Map
	localFiles sync.Map
}

// NewParseMemo returns an empty memo.
func NewParseMemo() *ParseMemo { return &ParseMemo{} }

// parsedFile is the memoised outcome of parsing one callee.
type parsedFile struct {
	once sync.Once
	file *workflow.File
	sup  *suppress.Set
	err  error
	// reported flips when the callee's own parse error has been emitted.
	reported atomic.Bool
}

// loadResult is the outcome of following one uses: reference.
type loadResult struct {
	// callee is nil when the reference was not followed (docker, dynamic,
	// skipped) or failed to load.
	callee *loaded
	// callerFindings are attributed to the referencing file.
	callerFindings []finding.Finding
	// calleeFindings are attributed to the callee (malformed YAML).
	calleeFindings []finding.Finding
	calleeName     string
	// how/err classify a reference that was not followed, for the graph:
	// docker, skipped, dynamic, unresolved ("" when loaded or failed).
	how, err string
}

func (l *loader) read(p string) ([]byte, error) {
	if l.readFile != nil {
		return l.readFile(p)
	}
	return os.ReadFile(p)
}

// follow resolves and loads one uses: reference from a file at src.
func (l *loader) follow(src source, u workflow.Uses) loadResult {
	ref, err := uses.Parse(u.Raw)
	if err != nil {
		rule := "uses-invalid"
		if errors.Is(err, uses.ErrMissingRef) {
			rule = "uses-no-ref"
		}
		return loadResult{callerFindings: []finding.Finding{callerFinding(rule, u, fmt.Sprintf("uses: %q: %v", u.Raw, err))}}
	}
	switch ref.Kind {
	case uses.Docker:
		return loadResult{how: "docker"}
	case uses.Dynamic:
		return loadResult{how: "dynamic", err: "dynamic reference", callerFindings: []finding.Finding{callerFinding("uses-dynamic", u,
			fmt.Sprintf("uses: %q is dynamic and cannot be followed", u.Raw))}}
	case uses.Local:
		if src.local {
			return l.loadLocal(src.root, ref, u, "")
		}
		return withMeta(l.loadRemote(src.owner, src.repo, src.sha, src.ref, ref, u), src.refKind, src.cacheStatus)
	}

	// External.
	if link, ok := repolink.Lookup(l.links, ref.Owner, ref.Repo); ok {
		return l.loadLocal(link.Path, ref, u, link.Path)
	}
	skip := l.cfg.SkipActions
	if ref.IsWorkflow {
		skip = l.cfg.SkipWorkflows
	}
	if skip[uses.SlugNoRef(ref)] {
		return loadResult{how: "skipped", err: "skipped by config"}
	}
	if l.noFetch {
		return unresolvedResult(u)
	}
	if l.fetcher == nil {
		return loadResult{callerFindings: []finding.Finding{callerFinding("fetch-failed", u,
			fmt.Sprintf("%s: fetching is disabled", uses.Display(ref)))}}
	}
	r, err := fetch.ResolveRef(l.fetcher, ref.Owner, ref.Repo, ref.Ref)
	if err != nil {
		return loadResult{callerFindings: []finding.Finding{fetchFinding(u, err)}}
	}
	return withMeta(l.loadRemote(ref.Owner, ref.Repo, r.SHA, ref.Ref, ref, u), refKindName(r.Kind), r.Cache)
}

// loadLocal reads a local or linked reference under root. linked is the
// linked checkout path when the reference came through a link, for the
// "not found in linked checkout" message.
func (l *loader) loadLocal(root string, ref uses.Ref, u workflow.Uses, linked string) loadResult {
	var candidates []string
	if ref.IsWorkflow {
		candidates = []string{filepath.Join(root, filepath.FromSlash(ref.Path))}
	} else {
		dir := filepath.Join(root, filepath.FromSlash(ref.Path))
		candidates = []string{filepath.Join(dir, "action.yml"), filepath.Join(dir, "action.yaml")}
	}
	var data []byte
	var path string
	for _, c := range candidates {
		d, err := l.read(c)
		if err == nil {
			data, path = d, c
			break
		}
	}
	if path == "" {
		if linked != "" {
			return loadResult{callerFindings: []finding.Finding{callerFinding("uses-not-found", u,
				fmt.Sprintf("%s not found in linked checkout %s", uses.Display(ref), linked))}}
		}
		what := "action"
		if ref.IsWorkflow {
			what = "workflow"
		}
		return loadResult{callerFindings: []finding.Finding{callerFinding("uses-not-found", u,
			fmt.Sprintf("local %s %q not found under %s", what, u.Raw, root))}}
	}
	src := source{local: true, path: path, root: root}
	return l.parse(src, data, ref, u)
}

// loadRemote fetches a reference inside owner/repo at sha (§5.3, §5.4).
func (l *loader) loadRemote(owner, repo, sha, refName string, ref uses.Ref, u workflow.Uses) loadResult {
	if l.fetcher == nil {
		return loadResult{callerFindings: []finding.Finding{callerFinding("fetch-failed", u,
			fmt.Sprintf("%s: fetching is disabled", uses.Display(ref)))}}
	}
	var candidates []string
	if ref.IsWorkflow {
		candidates = []string{ref.Path}
	} else {
		base := ref.Path
		if base != "" {
			base += "/"
		}
		candidates = []string{base + "action.yml", base + "action.yaml"}
	}
	var data []byte
	var rpath, blobStatus string
	for _, c := range candidates {
		d, status, err := fetch.ContentWithStatus(l.fetcher, owner, repo, sha, c)
		if err == nil {
			data, rpath, blobStatus = d, c, status
			break
		}
		if k, _ := fetch.KindOf(err); k != fetch.NotFound {
			return loadResult{callerFindings: []finding.Finding{fetchFinding(u, err)}}
		}
	}
	if rpath == "" {
		name := owner + "/" + repo
		if ref.Path != "" {
			name += "/" + ref.Path
		}
		name += "@" + refName
		if ref.IsWorkflow {
			return loadResult{callerFindings: []finding.Finding{callerFinding("fetch-not-found", u,
				fmt.Sprintf("%s: workflow file not found", name))}}
		}
		return loadResult{callerFindings: []finding.Finding{callerFinding("fetch-not-found", u,
			fmt.Sprintf("%s: no action.yml or action.yaml at that path", name))}}
	}
	src := source{owner: owner, repo: repo, sha: sha, ref: refName, rpath: rpath, disp: ref.Path, cacheStatus: blobStatus}
	return l.parse(src, data, ref, u)
}

// parse builds the model and checks the callee's kind matches what the
// reference asked for.
func (l *loader) parse(src source, data []byte, ref uses.Ref, u workflow.Uses) loadResult {
	name := display(src)
	v, _ := l.parsed.m.LoadOrStore(identity(src), &parsedFile{})
	pf := v.(*parsedFile)
	pf.once.Do(func() {
		pf.file, pf.err = workflow.Parse(data)
		if pf.err == nil {
			pf.sup = suppress.Parse(data, rules.IsKnown, canonical)
		}
	})
	if pf.err != nil {
		res := loadResult{calleeName: name}
		if pf.reported.CompareAndSwap(false, true) {
			res.calleeFindings = []finding.Finding{{
				Severity: rules.Registry["callee-unparseable"], Rule: "callee-unparseable", File: name,
				Message: fmt.Sprintf("cannot parse: %v", pf.err),
			}}
		}
		return res
	}
	f := pf.file
	wantWorkflow := u.JobLevel
	switch {
	case f.Kind == workflow.KindUnknown:
		return loadResult{callerFindings: []finding.Finding{callerFinding("callee-unparseable", u,
			fmt.Sprintf("%s is neither a workflow nor an action", name))}}
	case wantWorkflow && f.Kind != workflow.KindWorkflow:
		return loadResult{callerFindings: []finding.Finding{callerFinding("uses-invalid", u,
			fmt.Sprintf("job-level uses: must reference a reusable workflow, but %s is a %s", name, f.Kind))}}
	case !wantWorkflow && f.Kind == workflow.KindWorkflow:
		return loadResult{callerFindings: []finding.Finding{callerFinding("uses-invalid", u,
			fmt.Sprintf("step-level uses: must reference an action, but %s is a workflow", name))}}
	}
	ld := &loaded{src: src, file: f, sup: pf.sup, raw: data}
	l.rememberLoaded(ld)
	return loadResult{callee: ld, calleeName: name}
}

// loadEntry reads an entry file from disk (§1.1, §2). rootOverride, when
// set, is the repo root for its ./ references; otherwise it is detected.
func (l *loader) loadEntry(path, rootOverride string) (*loaded, []finding.Finding) {
	root := rootOverride
	if root == "" {
		root = RepoRoot(filepath.Dir(path))
	}
	src := source{local: true, path: path, root: root}
	l.entryRoot = root
	l.entryPath = path
	data, err := l.read(path)
	if err != nil {
		return nil, []finding.Finding{{Severity: finding.Error, Rule: "file-invalid", File: path, Message: err.Error()}}
	}
	f, err := workflow.Parse(data)
	if err != nil {
		return nil, []finding.Finding{{Severity: finding.Error, Rule: "file-invalid", File: path, Message: fmt.Sprintf("cannot parse: %v", err)}}
	}
	if f.Kind == workflow.KindUnknown {
		return nil, []finding.Finding{{Severity: finding.Error, Rule: "file-invalid", File: path, Message: "not a workflow (no top-level jobs:) or action (no runs.using)"}}
	}
	return &loaded{src: src, file: f, sup: suppress.Parse(data, rules.IsKnown, canonical), raw: data}, nil
}

// RepoRoot finds the repository root for a local file (§3.1): the git
// top-level if inside a repo, else the nearest ancestor containing .github,
// else dir itself.
func RepoRoot(dir string) string {
	if root, ok := repolink.GitRoot(dir); ok {
		return root
	}
	for d := dir; ; {
		if st, err := os.Stat(filepath.Join(d, ".github")); err == nil && st.IsDir() {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return dir
}

func callerFinding(rule string, u workflow.Uses, msg string) finding.Finding {
	return finding.Finding{
		Severity: rules.Registry[rule],
		Rule:     rule,
		Line:     u.Pos.Line,
		Column:   u.Pos.Column,
		Message:  msg,
	}
}

// fetchFinding maps a fetch error to the §5.6 finding at the caller.
func fetchFinding(u workflow.Uses, err error) finding.Finding {
	kind, _ := fetch.KindOf(err)
	switch kind {
	case fetch.NotFound:
		return callerFinding("fetch-not-found", u, err.Error())
	case fetch.Offline:
		return callerFinding("fetch-offline", u, err.Error()+", skipping")
	}
	return callerFinding("fetch-failed", u, err.Error()+", skipping")
}
