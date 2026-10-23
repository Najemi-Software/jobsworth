package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"

	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family security (advanced §1): script injection, privileged triggers
// with untrusted checkouts, unpinned actions, permissions, credential
// hygiene.

// UntrustedPaths are the github.* context paths an attacker controls
// (§1.1). A trailing ".*" matches any sub-field; a "*" segment matches
// one segment.
var UntrustedPaths = []string{
	"github.event.issue.title", "github.event.issue.body",
	"github.event.pull_request.title", "github.event.pull_request.body",
	"github.event.pull_request.head.ref", "github.event.pull_request.head.label",
	"github.event.pull_request.head.repo.default_branch",
	"github.event.pull_request.head.repo.description",
	"github.event.pull_request.head.repo.homepage",
	"github.event.comment.body", "github.event.review.body",
	"github.event.review_comment.body",
	"github.event.commits.*.message", "github.event.commits.*.author.name",
	"github.event.commits.*.author.email",
	"github.event.head_commit.message", "github.event.head_commit.author.name",
	"github.event.head_commit.author.email",
	"github.event.discussion.title", "github.event.discussion.body",
	"github.event.pages.*.page_name",
	"github.event.workflow_run.head_branch",
	"github.event.workflow_run.head_commit.message",
	"github.event.workflow_run.head_commit.author.*",
	"github.event.workflow_run.pull_requests.*.head.ref",
	"github.head_ref",
}

// PrivilegedTriggers run with a write token in the base context (§1.3).
var PrivilegedTriggers = []string{"pull_request_target", "issue_comment", "workflow_run", "discussion_comment"}

// untrustedRefs are checkout ref: values that fetch attacker code.
var untrustedRefs = []string{
	"github.event.pull_request.head.sha", "github.event.pull_request.head.ref", "github.head_ref",
	"github.event.workflow_run.head_sha", "github.event.workflow_run.head_branch",
}

// OIDCActions consume id-token: write (§1.5).
var OIDCActions = []string{"aws-actions/configure-aws-credentials", "google-github-actions/auth", "azure/login", "hashicorp/vault-action", "sigstore/"}

// PermissionScopes are the documented permissions keys.
var PermissionScopes = []string{"actions", "attestations", "checks", "contents", "deployments", "discussions", "id-token", "issues", "models", "packages", "pages", "pull-requests", "repository-projects", "security-events", "statuses"}

// SecurityInputs is what CheckSecurity knows beyond the file.
type SecurityInputs struct {
	// Self is the entry repo's owner/name ("" when unknown).
	Self             string
	TrustedOwners    []string
	OIDCActions      []string
	Strict           bool
	SelfHostedLabels []string
	// JobCallees maps a uses: job to its callee, for injection-via-input.
	JobCallees map[string]*workflow.File
	// Checkouts are the effective checkouts per job and step
	// (checkout-inference §3); nil falls back to direct checkouts.
	Checkouts map[string]Checkouts
	// Resolve loads a callee's own callee by its uses: text, so taint
	// follows with: values through as many hops as the walk makes (§1.2,
	// capped at taintDepth). nil means one hop.
	Resolve func(callee *workflow.File, raw string) *workflow.File
}

// CheckSecurity implements advanced §1 over one file.
func CheckSecurity(f *workflow.File, scopes map[string]*Scope, in SecurityInputs) []finding.Finding {
	var out []finding.Finding
	out = append(out, checkInjection(f, scopes, in)...)
	out = append(out, checkUnpinned(f, in)...)
	if f.Kind == workflow.KindWorkflow {
		out = append(out, checkPrivilegedTriggers(f, scopes, in)...)
		out = append(out, checkPermissions(f, scopes)...)
		out = append(out, checkSelfHostedPR(f, in)...)
	}
	for _, sc := range scopes {
		out = append(out, checkCredentialHygiene(f, sc, in)...)
	}
	return out
}

// --- §1.1 injection -----------------------------------------------------------------

// untrustedIn lists the untrusted paths an expression touches.
func untrustedIn(text string) []string {
	node, err := expr.Parse(text)
	if err != nil {
		return nil
	}
	var out []string
	for _, c := range expr.Chains(node) {
		for ch := &c; ch != nil; ch = ch.Inner {
			if !strings.EqualFold(ch.Root, "github") {
				continue
			}
			path := "github"
			for _, s := range ch.Segs {
				switch {
				case s.Wildcard || s.IsIndex || s.Dynamic:
					path += ".*"
				default:
					path += "." + s.Name
				}
			}
			if matchUntrusted(path) {
				out = append(out, path)
			}
		}
	}
	return out
}

func matchUntrusted(path string) bool {
	segs := strings.Split(strings.ToLower(path), ".")
	for _, pat := range UntrustedPaths {
		p := strings.Split(pat, ".")
		if matchSegs(segs, p) {
			return true
		}
	}
	return false
}

func matchSegs(segs, pat []string) bool {
	if len(pat) > 0 && pat[len(pat)-1] == "*" && len(segs) >= len(pat) {
		return matchSegs(segs[:len(pat)-1], pat[:len(pat)-1])
	}
	if len(segs) != len(pat) {
		return false
	}
	for i := range pat {
		if pat[i] != "*" && pat[i] != segs[i] {
			return false
		}
	}
	return true
}

// sinkInputs lists the inputs a callee interpolates into a sink.
func sinkInputs(callee *workflow.File) map[string]int {
	return sinkInputsDeep(callee, nil, 0, map[*workflow.File]bool{})
}

// taintDepth caps the hops taint follows through with: forwarding.
const taintDepth = 3

// sinkInputsDeep is sinkInputs plus the inputs the callee forwards, in a
// with: value, into a sink input of its own callees (§1.2 multi-hop). The
// line is where the forwarding or the sink sits in the callee.
func sinkInputsDeep(callee *workflow.File, resolve func(*workflow.File, string) *workflow.File, depth int, seen map[*workflow.File]bool) map[string]int {
	out := map[string]int{}
	if callee == nil || seen[callee] {
		return out
	}
	seen[callee] = true
	defer delete(seen, callee)
	for _, e := range callee.Exprs {
		if !isSinkKey(callee, e) {
			continue
		}
		for _, name := range workflow.InputRefs(e.Text) {
			if _, done := out[name]; !done {
				out[name] = e.Pos.Line
			}
		}
	}
	if resolve == nil || depth+1 >= taintDepth {
		return out
	}
	forward := func(raw string, with []workflow.With) {
		sub := resolve(callee, raw)
		if sub == nil {
			return
		}
		subSinks := sinkInputsDeep(sub, resolve, depth+1, seen)
		for _, w := range with {
			if _, ok := subSinks[w.Name]; !ok {
				continue
			}
			for _, name := range workflow.InputRefs(w.Value.Value) {
				if _, done := out[name]; !done {
					out[name] = w.Value.Pos.Line
				}
			}
		}
	}
	for _, j := range callee.Jobs {
		if j.Uses != nil {
			forward(j.Uses.Raw, j.With)
		}
		for _, st := range workflow.StepsOf(callee, j.Name) {
			if st.Uses != "" {
				forward(st.Uses, st.With)
			}
		}
	}
	if callee.Kind != workflow.KindWorkflow {
		for _, st := range callee.Steps {
			if st.Uses != "" {
				forward(st.Uses, st.With)
			}
		}
	}
	return out
}

// taintedOutputs finds step and job outputs written from a bash variable
// seeded by a tainted env: value (§1.2, the bash crossing): the map key
// is "steps.<id>.outputs.<k>" or "needs.<job>.outputs.<k>", lower-cased,
// the value a description of the source.
func taintedOutputs(f *workflow.File, scopes map[string]*Scope) map[string]string {
	out := map[string]string{}
	for _, sc := range scopes {
		jobEnv := map[string]string{}
		if j := workflow.JobByName(f, sc.Job); j != nil {
			for _, w := range j.Env {
				if p := untrustedInValue(w.Value); p != "" {
					jobEnv[w.Name] = p
				}
			}
		}
		for _, st := range sc.Steps {
			a := sc.Bash[st.Index]
			if a == nil || a.ParseErr != nil || st.ID == "" {
				continue
			}
			tainted := map[string]string{}
			for k, v := range jobEnv {
				tainted[k] = v
			}
			for _, w := range st.Env {
				if p := untrustedInValue(w.Value); p != "" {
					tainted[w.Name] = p
				}
			}
			if len(tainted) == 0 {
				continue
			}
			for k, w := range a.Target(bash.Output).Writes {
				if w.Source == nil {
					continue
				}
				for name, path := range tainted {
					if refsVariable(w.Source, name) {
						out[strings.ToLower("steps."+st.ID+".outputs."+k)] = fmt.Sprintf("%s via env %s in step '%s'", path, name, st.ID)
						break
					}
				}
			}
		}
	}
	// Job outputs carrying a tainted step output.
	for _, j := range f.Jobs {
		for _, o := range j.Outputs {
			for _, r := range workflow.StepRefs(o.Value) {
				if desc, ok := out[strings.ToLower("steps."+r.ID+".outputs."+r.Output)]; ok {
					out[strings.ToLower("needs."+j.Name+".outputs."+o.Name)] = desc
				}
			}
		}
	}
	return out
}

// untrustedInValue is the first untrusted path in a scalar's expressions.
func untrustedInValue(v workflow.Scalar) string {
	for _, span := range workflow.ExtractExprs(v.Value, v.Pos) {
		if paths := untrustedIn(span.Text); len(paths) > 0 {
			return paths[0]
		}
	}
	return ""
}

// refsVariable reports whether a bash value expands the named variable.
func refsVariable(src *bash.ValueSource, name string) bool {
	for _, v := range src.Vars {
		if v == name {
			return true
		}
	}
	return false
}

// taintedIn is untrustedIn plus references to tainted step and job
// outputs, rendered as "steps.x.outputs.t (<source>)".
func taintedIn(text string, taints map[string]string) []string {
	out := untrustedIn(text)
	if len(taints) == 0 {
		return out
	}
	node, err := expr.Parse(text)
	if err != nil {
		return out
	}
	for _, c := range expr.Chains(node) {
		for ch := &c; ch != nil; ch = ch.Inner {
			root := strings.ToLower(ch.Root)
			if (root != "steps" && root != "needs") || len(ch.Segs) < 3 {
				continue
			}
			key := root + "." + strings.ToLower(ch.Segs[0].Name) + ".outputs." + strings.ToLower(ch.Segs[2].Name)
			if desc, ok := taints[key]; ok && strings.EqualFold(ch.Segs[1].Name, "outputs") {
				out = append(out, key+" ("+desc+")")
			}
		}
	}
	return out
}

// isSinkKey reports whether an expression sits in a shell or script sink.
func isSinkKey(f *workflow.File, e workflow.Expr) bool {
	if e.Key == "run" {
		return true
	}
	if e.Key == "script" && e.Step >= 0 {
		steps := workflow.StepsOf(f, e.Job)
		if e.Step < len(steps) && isGitHubScript(steps[e.Step].Uses) {
			return true
		}
	}
	return false
}

func isGitHubScript(raw string) bool {
	return strings.HasPrefix(strings.ToLower(raw), "actions/github-script@")
}

func checkInjection(f *workflow.File, scopes map[string]*Scope, in SecurityInputs) []finding.Finding {
	var out []finding.Finding
	taints := taintedOutputs(f, scopes)
	for _, e := range f.Exprs {
		paths := taintedIn(e.Text, taints)
		if len(paths) == 0 {
			continue
		}
		switch {
		case e.Key == "run":
			out = append(out, at("injection-run", e.Pos, fmt.Sprintf("%s is interpolated into a run: script; an attacker controls it and quoting does not help. Put it in env: and read the variable in the script", paths[0])))
		case e.Key == "script" && e.Step >= 0:
			steps := workflow.StepsOf(f, e.Job)
			if e.Step < len(steps) && isGitHubScript(steps[e.Step].Uses) {
				out = append(out, at("injection-script", e.Pos, fmt.Sprintf("%s is interpolated into actions/github-script code; read it with core.getInput or from env: instead", paths[0])))
			}
		}
	}
	// One hop: a with: value carrying an untrusted path into an input the
	// callee interpolates into a sink.
	report := func(pos workflow.Pos, input, calleeName string, line int, path string) {
		out = append(out, at("injection-via-input", pos, fmt.Sprintf("with.%s carries %s into %s, which interpolates or forwards inputs.%s into a run: sink (line %d of the callee)", input, path, calleeName, input, line)))
	}
	for _, sc := range scopes {
		for _, st := range sc.Steps {
			callee := sc.Callees[st.Index]
			if callee == nil {
				continue
			}
			sinks := sinkInputsDeep(callee, in.Resolve, 0, map[*workflow.File]bool{})
			for _, w := range st.With {
				line, ok := sinks[w.Name]
				if !ok {
					continue
				}
				for _, span := range workflow.ExtractExprs(w.Value.Value, w.Value.Pos) {
					if paths := taintedIn(span.Text, taints); len(paths) > 0 {
						report(w.Value.Pos, w.Name, sc.CalleeNames[st.Index], line, paths[0])
						break
					}
				}
			}
		}
	}
	for _, j := range f.Jobs {
		callee := in.JobCallees[j.Name]
		if callee == nil || j.Uses == nil {
			continue
		}
		sinks := sinkInputsDeep(callee, in.Resolve, 0, map[*workflow.File]bool{})
		for _, w := range j.With {
			line, ok := sinks[w.Name]
			if !ok {
				continue
			}
			for _, span := range workflow.ExtractExprs(w.Value.Value, w.Value.Pos) {
				if paths := taintedIn(span.Text, taints); len(paths) > 0 {
					report(w.Value.Pos, w.Name, j.Uses.Raw, line, paths[0])
					break
				}
			}
		}
	}
	return out
}

// --- §1.3 privileged triggers ------------------------------------------------------

func privileged(f *workflow.File) []string {
	var out []string
	for _, ev := range f.Events {
		if contains(PrivilegedTriggers, ev) {
			out = append(out, ev)
		}
	}
	return out
}

var reRunsFromTree = regexp.MustCompile(`(?m)(^|[\s;&|(])(\./|bash\s+[^-\s]|sh\s+[^-\s]|npm (ci|install)|yarn( install)?\b|pnpm install|pip install|pip3 install|poetry install|bundle( install| exec)|make\b|cargo (build|run|test)|go (build|run|test|generate)|gradle|\./gradlew|mvn)`)

func checkPrivilegedTriggers(f *workflow.File, scopes map[string]*Scope, in SecurityInputs) []finding.Finding {
	var out []finding.Finding
	triggers := privileged(f)
	if len(triggers) == 0 {
		return nil
	}
	if f.Permissions == nil {
		hasJobPerms := false
		for _, j := range f.Jobs {
			if j.Permissions != nil {
				hasJobPerms = true
			}
		}
		if !hasJobPerms {
			out = append(out, at("prt-no-permissions", f.On.Pos, fmt.Sprintf("workflow runs on %s with a write token and declares no permissions: block; set the least privilege it needs", strings.Join(triggers, ", "))))
		}
	}
	for _, j := range f.Jobs {
		sc := scopes[j.Name]
		if sc == nil {
			continue
		}
		for _, st := range sc.Steps {
			ref := ""
			for _, cs := range checkoutsOf(in.Checkouts, f, j.Name)[st.Index] {
				if !cs.Unknown && cs.RefExpr != "" {
					ref = cs.RefExpr
				}
			}
			if ref == "" {
				continue
			}
			untrusted := ""
			for _, span := range workflow.ExtractExprs(ref, workflow.Pos{}) {
				for _, u := range untrustedRefs {
					if strings.Contains(strings.ToLower(span.Text), u) {
						untrusted = u
					}
				}
			}
			if untrusted == "" {
				continue
			}
			if later, what := runsFromTree(sc, st.Index); later >= 0 {
				out = append(out, at("prt-untrusted-checkout-and-run", st.UsesPos, fmt.Sprintf("job '%s' checks out %s on %s and step #%d then %s; attacker code runs with the base repo's secrets and a write token", j.Name, untrusted, strings.Join(triggers, ", "), later+1, what)))
				continue
			}
			out = append(out, at("prt-untrusted-checkout", st.UsesPos, fmt.Sprintf("job '%s' checks out %s on %s; secrets and a write token are exposed to code from the pull request", j.Name, untrusted, strings.Join(triggers, ", "))))
		}
	}
	return out
}

// runsFromTree finds a step after idx that executes code from the working
// tree; it returns the step index and what it does.
func runsFromTree(sc *Scope, idx int) (int, string) {
	for _, st := range sc.Steps {
		if st.Index <= idx {
			continue
		}
		if st.Uses != "" {
			if ref, err := uses.Parse(st.Uses); err == nil && ref.Kind == uses.Local {
				return st.Index, "runs the local action " + st.Uses
			}
			continue
		}
		if !st.HasRun {
			continue
		}
		if reRunsFromTree.MatchString(st.Run) {
			return st.Index, "runs code from the checked-out tree"
		}
		if a := sc.Bash[st.Index]; a != nil && a.ParseErr == nil {
			for _, s := range a.Statements {
				if strings.HasPrefix(s.Class, "opaque") {
					return st.Index, "runs an opaque script (" + s.Class + ")"
				}
			}
		}
	}
	return -1, ""
}

// --- §1.4 unpinned actions -----------------------------------------------------------

var reSHA40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var rePinComment = regexp.MustCompile(`^\s*v?\d`)

func checkUnpinned(f *workflow.File, in SecurityInputs) []finding.Finding {
	var out []finding.Finding
	trusted := map[string]bool{"actions": true, "github": true}
	for _, o := range in.TrustedOwners {
		trusted[strings.ToLower(o)] = true
	}
	if i := strings.IndexByte(in.Self, '/'); i > 0 {
		trusted[strings.ToLower(in.Self[:i])] = true
	}
	for _, u := range f.Uses {
		if strings.HasPrefix(u.Raw, "docker://") {
			if !strings.Contains(u.Raw, "@sha256:") {
				out = append(out, at("uses-docker-unpinned", u.Pos, fmt.Sprintf("%s is not pinned to a digest (@sha256:...); the tag can be moved", u.Raw)))
			}
			continue
		}
		ref, err := uses.Parse(u.Raw)
		if err != nil || ref.Kind != uses.External {
			continue
		}
		if reSHA40.MatchString(ref.Ref) {
			if !rePinComment.MatchString(u.Comment) {
				out = append(out, at("uses-unpinned-no-comment", u.Pos, fmt.Sprintf("%s is pinned to a SHA with no trailing version comment (# vX.Y); readers cannot tell which release it is", uses.Display(ref))))
			}
			continue
		}
		if trusted[strings.ToLower(ref.Owner)] {
			continue
		}
		out = append(out, at("uses-unpinned", u.Pos, fmt.Sprintf("%s is pinned to %q, not a full commit SHA; the owner can move the tag (trusted owners: %s)", uses.Display(ref), ref.Ref, strings.Join(sortedKeys(trusted), ", "))))
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- §1.5 permissions ------------------------------------------------------------------

func checkPermissions(f *workflow.File, scopes map[string]*Scope) []finding.Finding {
	var out []finding.Finding
	check := func(p *workflow.Permissions, where string) {
		if p == nil {
			return
		}
		switch p.Form {
		case "write-all":
			out = append(out, at("permissions-write-all", p.Pos, where+" permissions: write-all grants every scope; list the scopes the workflow needs"))
		case "other":
			out = append(out, at("permissions-unknown-level", p.Pos, where+" permissions: must be read-all, write-all, or a map of scopes"))
		case "map":
			for _, s := range p.Scopes {
				if !contains(PermissionScopes, s.Name) {
					out = append(out, at("permissions-unknown-scope", s.Pos, fmt.Sprintf("%s permissions: %q is not a permissions scope (known: %s); GitHub rejects the workflow", where, s.Name, strings.Join(PermissionScopes, ", "))))
				}
				if s.Level != "read" && s.Level != "write" && s.Level != "none" {
					out = append(out, at("permissions-unknown-level", s.Pos, fmt.Sprintf("%s permissions: %s: %q is not read, write or none", where, s.Name, s.Level)))
				}
			}
		}
	}
	check(f.Permissions, "workflow")
	for _, j := range f.Jobs {
		check(j.Permissions, "job '"+j.Name+"'")
	}
	// permissions-missing: no block anywhere on a token-bearing workflow.
	if f.Permissions == nil && len(f.Events) > 0 && !contains(f.Events, "workflow_call") {
		missing := false
		for _, j := range f.Jobs {
			if j.Permissions == nil && j.Uses == nil {
				missing = true
			}
		}
		if missing {
			out = append(out, at("permissions-missing", f.On.Pos, "no permissions: block at workflow level and at least one job without one; the token gets the repository default, which may be read-write"))
		}
	}
	// id-token: write without a consumer.
	grants := func(p *workflow.Permissions) bool {
		if p == nil || p.Form == "write-all" {
			return p != nil
		}
		for _, s := range p.Scopes {
			if s.Name == "id-token" && s.Level == "write" {
				return true
			}
		}
		return false
	}
	usesOIDC := func(steps []workflow.Step) bool {
		for _, st := range steps {
			for _, a := range OIDCActions {
				if strings.HasPrefix(strings.ToLower(st.Uses), a) {
					return true
				}
			}
			if strings.Contains(st.Run, "ACTIONS_ID_TOKEN_REQUEST_URL") {
				return true
			}
		}
		return false
	}
	for _, j := range f.Jobs {
		if j.Uses != nil {
			continue
		}
		p := j.Permissions
		if p == nil {
			p = f.Permissions
		}
		if p == nil || p.Form == "write-all" || !grants(p) {
			continue
		}
		if !usesOIDC(j.Steps) {
			out = append(out, at("permissions-id-token-unused", p.Pos, fmt.Sprintf("id-token: write is granted but job '%s' uses no OIDC-consuming action and no step requests a token; drop the grant", j.Name)))
		}
	}
	return out
}

// --- §1.6 credential hygiene ---------------------------------------------------------

func checkSelfHostedPR(f *workflow.File, in SecurityInputs) []finding.Finding {
	var out []finding.Finding
	if f.On == nil {
		return nil
	}
	forkPR := false
	for _, ev := range f.On.Events {
		if ev.Name != "pull_request" {
			continue
		}
		restricted := false
		for _, k := range ev.Keys {
			if k.Name == "branches" {
				restricted = true
			}
		}
		if !restricted {
			forkPR = true
		}
	}
	if !forkPR {
		return nil
	}
	for _, j := range f.Jobs {
		if j.RunsOn == nil {
			continue
		}
		for _, l := range j.RunsOn.Labels {
			if l.Value == "self-hosted" || contains(in.SelfHostedLabels, l.Value) {
				out = append(out, at("self-hosted-pr-trigger", l.Pos, fmt.Sprintf("job '%s' runs on a self-hosted runner for pull_request without a branches: restriction; pull requests from forks execute on that machine", j.Name)))
				break
			}
		}
	}
	return out
}

var reRedirectTarget = regexp.MustCompile(`>>?\s*"?([A-Za-z0-9_./-]+)"?`)

func checkCredentialHygiene(f *workflow.File, sc *Scope, in SecurityInputs) []finding.Finding {
	var out []finding.Finding
	privilegedWF := f.Kind == workflow.KindWorkflow && len(privileged(f)) > 0
	// Secret-seeded env names per step and at job/workflow level.
	seeded := map[string]bool{}
	addSeeded := func(ws []workflow.With) {
		for _, w := range ws {
			if strings.Contains(w.Value.Value, "secrets.") {
				seeded[w.Name] = true
			}
		}
	}
	addSeeded(f.Env)
	if sc.Job != "" {
		if j := workflow.JobByName(f, sc.Job); j != nil {
			addSeeded(j.Env)
		}
	}
	type secretFile struct {
		path string
		line int
	}
	var written []secretFile
	for _, st := range sc.Steps {
		stepSeeded := map[string]bool{}
		for k := range seeded {
			stepSeeded[k] = true
		}
		for _, w := range st.Env {
			if strings.Contains(w.Value.Value, "secrets.") {
				stepSeeded[w.Name] = true
			}
		}
		if st.HasRun {
			for i, line := range strings.Split(st.Run, "\n") {
				hasSecret := strings.Contains(line, "secrets.")
				for name := range stepSeeded {
					if strings.Contains(line, "$"+name) || strings.Contains(line, "${"+name+"}") {
						hasSecret = true
					}
				}
				if !hasSecret {
					continue
				}
				trim := strings.TrimSpace(line)
				if strings.HasPrefix(trim, "echo ") || strings.HasPrefix(trim, "printf ") {
					if !strings.Contains(line, ">") {
						out = append(out, at("secret-echo", st.RunPos, fmt.Sprintf("step %s echoes a secret (script line %d); the runner masks exact matches only", StepLabel(st), i+1)))
					}
				}
				if m := reRedirectTarget.FindStringSubmatch(line); m != nil && !strings.HasPrefix(m[1], "/dev/") && !strings.HasPrefix(m[1], "$") {
					written = append(written, secretFile{path: m[1], line: absScriptLine(st, i+1)})
				}
			}
		}
		if isUploadArtifact(st.Uses) {
			for _, w := range st.With {
				if w.Name != "path" {
					continue
				}
				for _, sf := range written {
					if artifactCovers(w.Value.Value, sf.path) {
						out = append(out, at("secret-to-artifact", w.Value.Pos, fmt.Sprintf("a secret is written to %s (line %d) and this upload-artifact path covers it; the artifact is downloadable by anyone with read access", sf.path, sf.line)))
						break
					}
				}
			}
		}
		if privilegedWF && (strings.HasPrefix(strings.ToLower(st.Uses), "actions/cache@") || (strings.HasPrefix(strings.ToLower(st.Uses), "actions/setup-") && withHas(st, "cache"))) {
			for _, w := range st.With {
				if w.Name != "key" && w.Name != "cache-dependency-path" && w.Name != "restore-keys" {
					continue
				}
				for _, span := range workflow.ExtractExprs(w.Value.Value, w.Value.Pos) {
					if paths := untrustedIn(span.Text); len(paths) > 0 {
						out = append(out, at("cache-poisoning-path", w.Value.Pos, fmt.Sprintf("cache key includes %s on a privileged trigger; a pull request can poison the cache that later runs restore", paths[0])))
					}
				}
			}
		}
	}
	// checkout-persist-credentials: default-off; security.strict turns it on.
	{
		for _, st := range sc.Steps {
			persisting := false
			for _, cs := range checkoutsOf(in.Checkouts, f, sc.Job)[st.Index] {
				if !cs.Unknown && cs.PersistCredentials != "false" {
					persisting = true
				}
			}
			if !persisting {
				continue
			}
			for _, later := range sc.Steps {
				if later.Index <= st.Index {
					continue
				}
				risky := ""
				switch {
				case isUploadArtifact(later.Uses):
					risky = "uploads an artifact"
				case strings.HasPrefix(strings.ToLower(later.Uses), "actions/cache@"):
					risky = "saves a cache"
				case later.Uses != "" && !trustedUses(later.Uses, in):
					risky = "runs the untrusted action " + later.Uses
				}
				if risky != "" {
					out = append(out, at("checkout-persist-credentials", st.UsesPos, fmt.Sprintf("checkout keeps the token in .git/config and step #%d later %s; set persist-credentials: false", later.Index+1, risky)))
					break
				}
			}
		}
	}
	return out
}

func isUploadArtifact(raw string) bool {
	return strings.HasPrefix(strings.ToLower(raw), "actions/upload-artifact@")
}

func withHas(st workflow.Step, name string) bool {
	for _, w := range st.With {
		if w.Name == name {
			return true
		}
	}
	return false
}

// checkoutsOf is a job's checkouts from the walk, or its direct checkouts
// when none were supplied.
func checkoutsOf(all map[string]Checkouts, f *workflow.File, job string) Checkouts {
	if all != nil {
		if cos, ok := all[job]; ok {
			return cos
		}
	}
	return DirectCheckouts(f, job)
}

func withValue(st workflow.Step, name string) string {
	for _, w := range st.With {
		if w.Name == name {
			return w.Value.Value
		}
	}
	return ""
}

// artifactCovers reports whether an upload path covers a written file.
func artifactCovers(pattern, file string) bool {
	for _, p := range strings.Split(pattern, "\n") {
		p = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p), "./"))
		file = strings.TrimPrefix(file, "./")
		if p == "" {
			continue
		}
		if p == "." || p == "**" || p == "*" || p == file || strings.HasPrefix(file, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
		if strings.Contains(p, "*") {
			prefix := p[:strings.IndexByte(p, '*')]
			if strings.HasPrefix(file, prefix) {
				return true
			}
		}
	}
	return false
}

func trustedUses(raw string, in SecurityInputs) bool {
	ref, err := uses.Parse(raw)
	if err != nil || ref.Kind != uses.External {
		return true
	}
	owner := strings.ToLower(ref.Owner)
	if owner == "actions" || owner == "github" {
		return true
	}
	for _, o := range in.TrustedOwners {
		if strings.EqualFold(o, ref.Owner) {
			return true
		}
	}
	if i := strings.IndexByte(in.Self, '/'); i > 0 && strings.EqualFold(in.Self[:i], ref.Owner) {
		return true
	}
	return false
}
