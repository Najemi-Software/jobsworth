package explain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/checkout"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/shapeq"
	"github.com/najemi-software/jobsworth/internal/strutil"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// render picks the family renderer (spec-explain §3.1). A nil trace (the
// file was not validated by this run) yields the generic block.
func render(f finding.Finding, ft *check.FileTrace, res check.Result, env Env) Context {
	r, _, ok := rules.Lookup(f.Rule)
	if ok && f.Rule == "callee-unparseable" {
		return unparseableContext(f, env)
	}
	if !ok || ft == nil {
		return generic(f)
	}
	switch r.Family {
	case "steps":
		return stepsContext(f, ft, res)
	case "bash":
		return bashContext(f, ft)
	case "calls":
		return callsContext(f, ft)
	case "jobs":
		return jobsContext(f, ft)
	case "checkout":
		return checkoutContext(f, ft)
	case "secrets":
		return secretsContext(f, ft)
	case "values":
		return valuesContext(f, ft)
	case "fetch":
		return fetchContext(f, ft, env)
	case "inputs":
		return inputsContext(f, ft)
	case "shapes":
		return shapesContext(f, ft)
	case "suppress":
		return suppressContext(f, ft)
	case "structure":
		if f.Rule == "file-invalid" {
			return generic(f)
		}
		return siteContext("structure", f, ft)
	case "triggers":
		return siteContext("trigger", f, ft)
	case "security":
		return siteContext("security", f, ft)
	case "conditions":
		return siteContext("condition", f, ft)
	case "expressions":
		return siteContext("expression", f, ft)
	case "scripts":
		return siteContext("script", f, ft)
	case "reachability":
		return siteContext("reachability", f, ft)
	case "performance":
		return siteContext("performance", f, ft)
	}
	return generic(f)
}

// outputsContext explains action-output-unused: the action's declared
// outputs, with the flagged one marked, and where consumers were looked
// for.
func outputsContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "step", Data: map[string]any{}}
	var decl []string
	var data []map[string]any
	for _, o := range ft.File.Outputs {
		mark := ""
		if strings.Contains(f.Message, "'"+o.Name+"'") {
			mark = "   ← not consumed"
		}
		decl = append(decl, fmt.Sprintf("%s (:%d)%s", o.Name, o.Pos.Line, mark))
		data = append(data, map[string]any{"name": o.Name, "line": o.Pos.Line, "value": o.Value, "unused": mark != ""})
	}
	c.Lines = append(c.Lines, "outputs        "+listOrNone(decl))
	c.Lines = append(c.Lines, "consumers      every workflow and action in the entry repo and each linked repo, through ./ uses: of this action")
	c.Data["outputs"] = data
	c.Conclusion = f.Message
	return c
}

// unparseableContext explains a callee that could not be parsed: nothing
// in it was validated, so the context is who referenced it.
func unparseableContext(f finding.Finding, env Env) Context {
	c := Context{Kind: "callee", Data: map[string]any{"file": f.File, "error": f.Message}}
	c.Lines = append(c.Lines, fmt.Sprintf("file           %s", display(f.File, env.Cwd)))
	c.Lines = append(c.Lines, fmt.Sprintf("error          %s", strings.TrimPrefix(f.Message, "cannot parse: ")))
	var refs []string
	var refData []map[string]any
	if env.Trace != nil {
		env.Trace.Each(func(name string, ft *check.FileTrace) {
			add := func(site string, ct *check.CalleeTrace) {
				if ct == nil || ct.Name != f.File {
					return
				}
				line := usesLine(ft, ct.Ref)
				refs = append(refs, fmt.Sprintf("%s:%d (%s, uses: %s)", display(name, env.Cwd), line, site, ct.Ref))
				refData = append(refData, map[string]any{"file": name, "line": line, "site": site, "uses": ct.Ref})
			}
			for job, ct := range ft.JobCallees {
				add("job "+job, ct)
			}
			for job, steps := range ft.StepCallees {
				for idx, ct := range steps {
					add(fmt.Sprintf("job %s / step #%d", job, idx+1), ct)
				}
			}
		})
	}
	sort.Strings(refs)
	c.Lines = append(c.Lines, "referenced by  "+listOrNone(refs))
	c.Data["referenced_by"] = refData
	c.Conclusion = "nothing in this file was validated; fix the YAML first, then re-run check."
	return c
}

// usesLine finds the line of a uses: value in a file (0 when unknown).
func usesLine(ft *check.FileTrace, raw string) int {
	for _, u := range ft.File.Uses {
		if u.Raw == raw {
			return u.Pos.Line
		}
	}
	return 0
}

func generic(f finding.Finding) Context {
	return Context{
		Kind:  "generic",
		Lines: []string{fmt.Sprintf("rule           %s", f.Rule), fmt.Sprintf("message        %s", f.Message)},
		Data:  map[string]any{"message": f.Message},
	}
}

// --- helpers ---------------------------------------------------------------------

// exprAt finds the expression anchored at a line.
func exprAt(ft *check.FileTrace, line int) (workflow.Expr, bool) {
	for _, e := range ft.File.Exprs {
		if e.Pos.Line == line {
			return e, true
		}
	}
	return workflow.Expr{}, false
}

// stepAtRun finds the scope and step whose run: block covers a line.
func stepAtRun(ft *check.FileTrace, line int) (*rules.Scope, workflow.Step, bool) {
	for _, sc := range ft.Scopes {
		for _, st := range sc.Steps {
			if !st.HasRun {
				continue
			}
			if line == st.RunPos.Line {
				return sc, st, true
			}
		}
	}
	return nil, workflow.Step{}, false
}

// stepAt finds the scope and step whose mapping contains the line.
func stepAt(ft *check.FileTrace, line int) (*rules.Scope, workflow.Step, bool) {
	var best *rules.Scope
	var bestStep workflow.Step
	found := false
	for _, sc := range ft.Scopes {
		for _, st := range sc.Steps {
			if st.Pos.Line <= line && (!found || st.Pos.Line > bestStep.Pos.Line) {
				best, bestStep, found = sc, st, true
			}
		}
	}
	return best, bestStep, found
}

func site(ft *check.FileTrace, e workflow.Expr) string {
	var parts []string
	if e.Job != "" {
		parts = append(parts, "job "+e.Job)
	}
	if e.Step >= 0 {
		for _, st := range workflow.StepsOf(ft.File, e.Job) {
			if st.Index == e.Step {
				parts = append(parts, "step "+rules.StepLabel(st))
			}
		}
	}
	if e.Key != "" {
		parts = append(parts, e.Key)
	}
	return strings.Join(parts, " / ")
}

func shellSource(ft *check.FileTrace, job string, st workflow.Step) string {
	switch {
	case st.Shell != "":
		return "step"
	case job != "":
		if j := workflow.JobByName(ft.File, job); j != nil && j.DefaultShell != "" {
			return "job default"
		}
		if ft.File.DefaultShell != "" {
			return "workflow default"
		}
		return "runner default"
	}
	return "missing"
}

// statements renders a step's classified statements with absolute lines.
func statements(st workflow.Step, a *bash.Analysis, relevant map[int]string) ([]string, []map[string]any) {
	var lines []string
	var data []map[string]any
	for _, s := range a.Statements {
		abs := absLine(st, s.Line)
		desc := s.Class
		switch {
		case s.Open != "":
			desc = "opaque: " + s.Open
		case len(s.Keys) > 0:
			kind := "definite"
			if !s.Definite {
				kind = "conditional"
			}
			desc = fmt.Sprintf("%s, writes %s key %s (%s)", s.Class, s.Redirect, quoteList(s.Keys), kind)
		case s.Redirect != "" && (s.Class == "echo" || s.Class == "printf" || s.Class == "cat"):
			desc = fmt.Sprintf("%s to %s, no key", s.Class, s.Redirect)
		case s.Class == "echo" || s.Class == "printf":
			desc = s.Class + ", NO REDIRECT"
		}
		line := fmt.Sprintf("  :%-5d %-46s %s", abs, truncateText(s.Text, 46), desc)
		if note, ok := relevant[abs]; ok {
			line += "   ← " + note
		}
		lines = append(lines, line)
		d := map[string]any{"line": abs, "text": s.Text, "class": s.Class, "definite": s.Definite}
		if s.Redirect != "" {
			d["redirect"] = s.Redirect
		}
		if len(s.Keys) > 0 {
			d["writes"] = s.Keys
		}
		if s.Open != "" {
			d["open"] = s.Open
		}
		if note, ok := relevant[abs]; ok {
			d["relevant"] = true
			d["note"] = note
		}
		data = append(data, d)
	}
	return lines, data
}

// relevantLines marks the statements a bash-family finding is about: the
// message names a script line ("line N" / "script line N") when it has one.
func relevantLines(f finding.Finding, st workflow.Step, a *bash.Analysis) map[int]string {
	out := map[int]string{}
	for _, m := range reScriptLine.FindAllStringSubmatch(f.Message, -1) {
		n := 0
		fmt.Sscanf(m[1], "%d", &n)
		if n > 0 {
			out[absLine(st, n)] = "named in the finding"
		}
	}
	// An echo of "key=..." that was not redirected (bash-output-unredirected):
	// the message quotes the key (explain review 2 N1).
	for _, kv := range a.Unredirected {
		if strings.Contains(f.Message, "\""+kv.Key+"=") {
			out[absLine(st, kv.Line)] = fmt.Sprintf("key '%s' echoed here, not redirected", kv.Key)
		}
	}
	if len(out) == 0 {
		// Fall back to statements that touch the family's target or key.
		for _, s := range a.Statements {
			for _, k := range s.Keys {
				if strings.Contains(f.Message, "'"+k+"'") || strings.Contains(f.Message, "\""+k) {
					out[absLine(st, s.Line)] = "writes " + k
				}
			}
			if s.Open != "" && strings.Contains(f.Message, "open") {
				out[absLine(st, s.Line)] = "opens the target"
			}
		}
	}
	return out
}

var reScriptLine = regexp.MustCompile(`(?:script )?line (\d+)`)

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func quoteList(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = "'" + s + "'"
	}
	return strings.Join(out, ", ")
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func targetBlock(a *bash.Analysis, t bash.Target) (string, map[string]any) {
	ts := a.Target(t)
	state := "closed"
	if ts.Open {
		state = "OPEN (" + ts.OpenReason + ")"
	}
	keys := map[string]string{}
	var names []string
	for _, k := range ts.Keys() {
		kind := "definite"
		if !ts.Writes[k].Definite {
			kind = "conditional"
		}
		keys[k] = kind
		names = append(names, k+" ("+kind[:3]+")")
	}
	return fmt.Sprintf("%s target: %s   keys written: %s", strings.TrimPrefix(t.String(), "$"), state, listOrNone(names)),
		map[string]any{"name": strings.TrimPrefix(t.String(), "$"), "open": ts.Open, "open_reason": ts.OpenReason, "keys": keys}
}

// --- steps ---------------------------------------------------------------------------

var reStepOutput = regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs\.([A-Za-z0-9_-]+)`)

func stepsContext(f finding.Finding, ft *check.FileTrace, res check.Result) Context {
	c := Context{Kind: "step-output", Data: map[string]any{}}
	e, ok := exprAt(ft, f.Line)
	var ref workflow.StepRef
	if ok {
		for _, r := range workflow.StepRefs(e.Text) {
			if strings.Contains(f.Message, "steps."+r.ID) {
				ref = r
				break
			}
		}
		if ref.ID == "" {
			if refs := workflow.StepRefs(e.Text); len(refs) > 0 {
				ref = refs[0]
			}
		}
	}
	sc := ft.Scopes[e.Job]
	if f.Rule == "action-output-unused" {
		return outputsContext(f, ft)
	}
	if !ok || ref.ID == "" || sc == nil {
		// Producer-side rules (unused output, missing shell, duplicate id).
		sc2, st, found := stepAt(ft, f.Line)
		if !found {
			return generic(f)
		}
		c.Kind = "step"
		c.Lines = append(c.Lines, fmt.Sprintf("step           %-36s at :%d   shell: %s (%s)", rules.StepLabel(st), st.Pos.Line, orNone(sc2.Shell[st.Index]), shellSource(ft, sc2.Job, st)))
		c.Data["step"] = map[string]any{"id": st.ID, "line": st.Pos.Line, "shell": sc2.Shell[st.Index], "shell_source": shellSource(ft, sc2.Job, st)}
		if a := sc2.Bash[st.Index]; a != nil && a.ParseErr == nil {
			lines, data := statements(st, a, relevantLines(f, st, a))
			c.Lines = append(c.Lines, fmt.Sprintf("script analysed as %s, %d statements:", sc2.Shell[st.Index], len(a.Statements)))
			c.Lines = append(c.Lines, lines...)
			tb, td := targetBlock(a, bash.Output)
			c.Lines = append(c.Lines, tb)
			c.Data["statements"], c.Data["target"] = data, td
		}
		c.Lines = append(c.Lines, "referenced ids in scope: "+listOrNone(referencedIDs(sc2)))
		c.Data["referenced_ids"] = referencedIDs(sc2)
		c.Conclusion = f.Message
		return c
	}
	c.Lines = append(c.Lines, fmt.Sprintf("reference      steps.%s.%s%s   at :%d  (%s)", ref.ID, ref.Field, dotOutput(ref), f.Line, site(ft, e)))
	c.Data["reference"] = map[string]any{"step": ref.ID, "field": ref.Field, "output": ref.Output, "line": f.Line, "site": site(ft, e)}
	st, found := sc.StepByID(ref.ID)
	if !found {
		c.Lines = append(c.Lines, "producing step not found; declared ids: "+listOrNone(sc.IDs()))
		c.Data["declared_ids"] = sc.IDs()
		if near := strutil.Nearest(ref.ID, sc.IDs(), 1); len(near) > 0 && len(sc.IDs()) > 0 {
			c.Conclusion = fmt.Sprintf("no step has id '%s'; nearest declared id is '%s'.", ref.ID, near[0])
		} else {
			c.Conclusion = fmt.Sprintf("no step has id '%s' and no step in this scope declares an id.", ref.ID)
		}
		return c
	}
	shell := sc.Shell[st.Index]
	c.Lines = append(c.Lines, fmt.Sprintf("producing step %-36s at :%d   shell: %s (%s)", rules.StepLabel(st), st.Pos.Line, orNone(shell), shellSource(ft, sc.Job, st)))
	c.Data["producer"] = map[string]any{"step": st.ID, "line": st.Pos.Line, "shell": shell, "shell_source": shellSource(ft, sc.Job, st), "index": st.Index}
	if e.Step >= 0 && st.Index >= e.Step && !e.InOutputs {
		c.Conclusion = fmt.Sprintf("step '%s' (index %d) is declared at or after the referencing step (index %d); the value is empty when evaluated.", st.ID, st.Index+1, e.Step+1)
	}
	if st.Uses != "" {
		ct := ft.StepCallees[sc.Job][st.Index]
		c.Data["producer"].(map[string]any)["uses"] = st.Uses
		if ct != nil && ct.File != nil {
			var names []string
			for _, o := range ct.File.Outputs {
				names = append(names, o.Name)
			}
			c.Lines = append(c.Lines, fmt.Sprintf("action         %-36s %s, declares outputs: %s", ct.Name, ct.How, listOrNone(names)))
			c.Data["action"] = map[string]any{"name": ct.Name, "how": ct.How, "outputs": names}
			if ref.Field == "outputs" && !contains(names, ref.Output) {
				near := strutil.Nearest(ref.Output, names, 1)
				if len(near) > 0 {
					c.Conclusion = fmt.Sprintf("'%s' is not among the declared outputs; nearest is '%s'.", ref.Output, near[0])
				} else {
					c.Conclusion = fmt.Sprintf("'%s' is not among the declared outputs.", ref.Output)
				}
			}
		} else if ct != nil {
			c.Lines = append(c.Lines, fmt.Sprintf("action         %-36s %s: %s", st.Uses, ct.How, ct.Err))
		}
		return c
	}
	a := sc.Bash[st.Index]
	if a == nil {
		c.Lines = append(c.Lines, "script not analysed: shell is not bash")
		return c
	}
	if a.ParseErr != nil {
		c.Lines = append(c.Lines, "script failed to parse: "+a.ParseErr.Error())
		c.Data["parse_error"] = a.ParseErr.Error()
		return c
	}
	relevant := map[int]string{}
	for _, kv := range a.Unredirected {
		if kv.Key == ref.Output {
			relevant[absLine(st, kv.Line)] = fmt.Sprintf("key '%s' seen here, not written", ref.Output)
		}
	}
	if w, ok := a.Target(bash.Output).Writes[ref.Output]; ok {
		kind := "definite"
		if !w.Definite {
			kind = "conditional"
		}
		relevant[absLine(st, w.Line)] = fmt.Sprintf("writes '%s' (%s)", ref.Output, kind)
	}
	lines, data := statements(st, a, relevant)
	c.Lines = append(c.Lines, fmt.Sprintf("script analysed as %s, %d statements:", shell, len(a.Statements)))
	c.Lines = append(c.Lines, lines...)
	tb, td := targetBlock(a, bash.Output)
	c.Lines = append(c.Lines, tb)
	c.Data["statements"], c.Data["target"] = data, td
	keys := a.Target(bash.Output).Keys()
	if c.Conclusion == "" {
		switch {
		case a.Target(bash.Output).Open:
			c.Conclusion = fmt.Sprintf("the target is open, so '%s' may be written by code the analyser cannot see.", ref.Output)
		case contains(keys, ref.Output):
			c.Conclusion = fmt.Sprintf("'%s' is written, but only on some paths.", ref.Output)
		default:
			c.Conclusion = fmt.Sprintf("'%s' is not among the keys written; %s.", ref.Output, writtenPhrase(keys))
		}
	}
	for _, other := range findingsOf(res, f.File) {
		if other.Rule == "bash-output-unredirected" && strings.Contains(other.Message, "'"+ref.Output+"'") || (other.Rule == "bash-output-unredirected" && strings.Contains(other.Message, ref.Output+"=")) {
			c.SeeAlso = append(c.SeeAlso, SeeAlso{Rule: other.Rule, Line: other.Line})
		}
	}
	return c
}

func dotOutput(r workflow.StepRef) string {
	if r.Field == "outputs" {
		return "." + r.Output
	}
	return ""
}

func writtenPhrase(keys []string) string {
	if len(keys) == 0 {
		return "nothing is written to GITHUB_OUTPUT"
	}
	return quoteList(keys) + " " + isAre(len(keys))
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func referencedIDs(sc *rules.Scope) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range sc.Refs {
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r.ID)
		}
	}
	sort.Strings(out)
	return out
}

// --- bash ------------------------------------------------------------------------------

func bashContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "bash", Data: map[string]any{}}
	sc, st, ok := stepAtRun(ft, f.Line)
	if !ok {
		return generic(f)
	}
	shell := sc.Shell[st.Index]
	c.Lines = append(c.Lines, fmt.Sprintf("step           %-36s run: at :%d   shell: %s (%s)", rules.StepLabel(st), st.RunPos.Line, orNone(shell), shellSource(ft, sc.Job, st)))
	c.Data["step"] = map[string]any{"id": st.ID, "line": st.RunPos.Line, "shell": shell, "shell_source": shellSource(ft, sc.Job, st)}
	a := sc.Bash[st.Index]
	if a == nil {
		return c
	}
	if a.ParseErr != nil {
		c.Lines = append(c.Lines, fmt.Sprintf("parse error    line %d column %d of the script (file line %d): %s", a.ParseErr.Line, a.ParseErr.Col, absLine(st, a.ParseErr.Line), a.ParseErr.Msg))
		c.Data["parse_error"] = map[string]any{"line": absLine(st, a.ParseErr.Line), "column": a.ParseErr.Col, "message": a.ParseErr.Msg}
		c.Conclusion = "no bash rule ran on this step; fix the syntax first."
		return c
	}
	lines, data := statements(st, a, relevantLines(f, st, a))
	c.Lines = append(c.Lines, fmt.Sprintf("script analysed as %s, %d statements:", shell, len(a.Statements)))
	c.Lines = append(c.Lines, lines...)
	c.Data["statements"] = data
	targets := []bash.Target{bash.Output}
	switch {
	case strings.Contains(f.Rule, "env"):
		targets = []bash.Target{bash.Env}
	case strings.Contains(f.Rule, "path"):
		targets = []bash.Target{bash.Path}
	case strings.HasPrefix(f.Rule, "heredoc"):
		targets = []bash.Target{bash.Output, bash.Env}
	}
	var tdata []map[string]any
	for _, t := range targets {
		tb, td := targetBlock(a, t)
		c.Lines = append(c.Lines, tb)
		ts := a.Target(t)
		if len(ts.Lines) > 0 {
			c.Lines = append(c.Lines, fmt.Sprintf("%s line stream:", strings.TrimPrefix(t.String(), "$")))
			var stream []map[string]any
			for _, l := range ts.Lines {
				flags := []string{}
				if l.Dynamic {
					flags = append(flags, "dynamic")
				}
				if l.InHeredoc {
					flags = append(flags, "in heredoc value")
				}
				if l.Opaque {
					flags = append(flags, "opaque source")
				}
				c.Lines = append(c.Lines, fmt.Sprintf("  :%-5d %-46s %s", absLine(st, l.Line), truncateText(l.Text, 46), strings.Join(flags, ", ")))
				stream = append(stream, map[string]any{"line": absLine(st, l.Line), "text": l.Text, "dynamic": l.Dynamic, "in_heredoc": l.InHeredoc, "opaque": l.Opaque})
			}
			td["stream"] = stream
		}
		if len(ts.Heredocs) > 0 && strings.HasPrefix(f.Rule, "heredoc") {
			var hd []map[string]any
			for _, h := range ts.Heredocs {
				state := "terminated"
				switch {
				case h.EmptyDelim:
					state = "empty delimiter"
				case h.EscapedNewline:
					state = "escaped newline, never opened"
				case !h.Terminated:
					state = "UNTERMINATED"
				}
				c.Lines = append(c.Lines, fmt.Sprintf("  heredoc %s<<%s at :%d  %s, %d opaque value line(s)", h.Key, h.Delim, absLine(st, h.Line), state, h.OpaqueLines))
				hd = append(hd, map[string]any{"key": h.Key, "delim": h.Delim, "line": absLine(st, h.Line), "state": state, "opaque_lines": h.OpaqueLines})
			}
			td["heredocs"] = hd
		}
		tdata = append(tdata, td)
	}
	c.Data["targets"] = tdata
	c.Conclusion = f.Message
	return c
}

// --- calls --------------------------------------------------------------------------------

func callsContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "call", Data: map[string]any{}}
	// Job-level call?
	for _, j := range ft.File.Jobs {
		if j.Uses == nil {
			continue
		}
		inJob := j.Uses.Pos.Line == f.Line
		for _, w := range j.With {
			if w.Value.Pos.Line == f.Line {
				inJob = true
			}
		}
		if !inJob {
			continue
		}
		ct := ft.JobCallees[j.Name]
		c.Lines = append(c.Lines, fmt.Sprintf("callsite       job %-32s at :%d", j.Name, j.Uses.Pos.Line))
		c.Data["callsite"] = map[string]any{"job": j.Name, "line": j.Uses.Pos.Line}
		if ct == nil || ct.File == nil {
			c.Lines = append(c.Lines, "callee         "+j.Uses.Raw+"  "+calleeStatus(ct))
			return c
		}
		return callDetail(c, j.Uses.Raw, ct, j.With, workflow.WorkflowCallInputs(ct.File))
	}
	sc, st, ok := stepAt(ft, f.Line)
	if !ok {
		return generic(f)
	}
	c.Lines = append(c.Lines, fmt.Sprintf("callsite       job %s / step %-20s at :%d", orNone(sc.Job), rules.StepLabel(st), st.Pos.Line))
	c.Data["callsite"] = map[string]any{"job": sc.Job, "step": rules.StepLabel(st), "line": st.Pos.Line}
	if st.HasRun && st.Uses == "" {
		c.Lines = append(c.Lines, "step kind      run: (with: is ignored on run steps)")
		c.Conclusion = f.Message
		return c
	}
	if st.HasRun && st.Uses != "" {
		c.Lines = append(c.Lines, fmt.Sprintf("step kind      both run: (:%d) and uses: (:%d)", st.RunPos.Line, st.UsesPos.Line))
		c.Conclusion = f.Message
		return c
	}
	ct := ft.StepCallees[sc.Job][st.Index]
	if ct == nil || ct.File == nil {
		c.Lines = append(c.Lines, "callee         "+st.Uses+"  "+calleeStatus(ct))
		return c
	}
	return callDetail(c, st.Uses, ct, st.With, ct.File.Inputs)
}

func calleeStatus(ct *check.CalleeTrace) string {
	if ct == nil {
		return "not followed"
	}
	if ct.Err != "" {
		return ct.How + ": " + ct.Err
	}
	return ct.How
}

func callDetail(c Context, ref string, ct *check.CalleeTrace, with []workflow.With, declared []workflow.Input) Context {
	c.Lines = append(c.Lines, fmt.Sprintf("callee         %-36s %s", ct.Name, ct.How))
	c.Data["callee"] = map[string]any{"name": ct.Name, "how": ct.How, "ref": ref}
	decl := map[string]workflow.Input{}
	var declNames, required []string
	for _, in := range declared {
		decl[in.Name] = in
		declNames = append(declNames, in.Name+typeTag(in))
		if in.Required && !in.HasDefault {
			required = append(required, in.Name)
		}
	}
	var supplied []string
	suppliedSet := map[string]bool{}
	var marks []string
	var sdata []map[string]any
	for _, w := range with {
		supplied = append(supplied, w.Name)
		suppliedSet[w.Name] = true
		in, ok := decl[w.Name]
		d := map[string]any{"name": w.Name, "declared": ok, "value": w.Value.Value, "expression": w.Value.IsExpr()}
		switch {
		case !ok:
			near := strutil.Nearest(w.Name, keysOf(decl), 1)
			mark := fmt.Sprintf("%s: UNKNOWN", w.Name)
			if len(near) > 0 {
				mark += fmt.Sprintf(" (nearest declared: %s)", near[0])
				d["nearest"] = near[0]
			}
			marks = append(marks, mark)
		case in.Type != "" && !w.Value.IsExpr():
			fits, want := rules.LiteralFits(w.Value, in.Type, in.Options)
			kind := strings.TrimPrefix(w.Value.Tag, "!!")
			if fits {
				marks = append(marks, fmt.Sprintf("%s: declared, %s literal fits type %s", w.Name, kind, in.Type))
			} else {
				marks = append(marks, fmt.Sprintf("%s: declared, %s literal %q does not fit type %s (expected %s)", w.Name, kind, w.Value.Value, in.Type, want))
				d["expected"] = want
			}
			d["kind"], d["type"] = kind, in.Type
			if len(in.Options) > 0 {
				d["options"] = in.Options
			}
		default:
			marks = append(marks, w.Name+": declared")
		}
		sdata = append(sdata, d)
	}
	var missing []string
	for _, r := range required {
		if !suppliedSet[r] {
			missing = append(missing, r)
		}
	}
	c.Lines = append(c.Lines, "supplied       "+listOrNone(supplied))
	c.Lines = append(c.Lines, fmt.Sprintf("declared       %s   (%d inputs)", listOrNone(declNames), len(declared)))
	c.Lines = append(c.Lines, "required       "+listOrNone(required)+"   missing: "+listOrNone(missing))
	for _, m := range marks {
		c.Lines = append(c.Lines, "  "+m)
	}
	c.Data["supplied"], c.Data["declared"], c.Data["required"], c.Data["missing"], c.Data["checks"] = sdata, declNames, required, missing, marks
	return c
}

func typeTag(in workflow.Input) string {
	if in.Type == "" {
		return ""
	}
	return " (" + in.Type + ")"
}

func keysOf(m map[string]workflow.Input) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- jobs ---------------------------------------------------------------------------------

func jobsContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "jobs", Data: map[string]any{}}
	fl := ft.File
	jobsOf := func() []string {
		var out []string
		for _, j := range fl.Jobs {
			out = append(out, j.Name)
		}
		return out
	}
	if f.Rule == "needs-cycle" {
		edges := map[string][]string{}
		exists := map[string]bool{}
		for _, j := range fl.Jobs {
			exists[j.Name] = true
		}
		for _, j := range fl.Jobs {
			for _, n := range j.Needs {
				if exists[n.Name] && n.Name != j.Name {
					edges[j.Name] = append(edges[j.Name], n.Name)
				}
			}
		}
		for _, cyc := range rules.Cycles(fl, edges) {
			if workflow.JobByName(fl, cyc[0]).Pos.Line != f.Line {
				continue
			}
			var parts []string
			for i, name := range cyc {
				next := cyc[(i+1)%len(cyc)]
				line := 0
				for _, n := range workflow.JobByName(fl, name).Needs {
					if n.Name == next {
						line = n.Pos.Line
					}
				}
				parts = append(parts, fmt.Sprintf("%s → %s (needs at :%d)", name, next, line))
			}
			c.Lines = append(c.Lines, "cycle          "+strings.Join(parts, ", "))
			c.Data["cycle"] = cyc
		}
		c.Conclusion = "no job in the cycle can start; break one needs: edge."
		return c
	}
	e, ok := exprAt(ft, f.Line)
	if ok {
		for _, r := range workflow.NeedsOutputRefs(e.Text) {
			c.Lines = append(c.Lines, fmt.Sprintf("reference      needs.%s.outputs.%s   at :%d  (%s)", r.Job, r.Output, f.Line, site(ft, e)))
			c.Data["reference"] = map[string]any{"job": r.Job, "output": r.Output, "line": f.Line, "site": site(ft, e)}
			if e.Job != "" {
				j := workflow.JobByName(fl, e.Job)
				var direct []string
				for _, n := range j.Needs {
					direct = append(direct, n.Name)
				}
				trans := rules.TransitiveNeeds(fl, e.Job)
				var transitive []string
				for name := range trans {
					if !contains(direct, name) {
						transitive = append(transitive, name)
					}
				}
				sort.Strings(transitive)
				c.Lines = append(c.Lines, fmt.Sprintf("job '%s'   needs: [%s]   direct", e.Job, strings.Join(direct, ", ")))
				c.Lines = append(c.Lines, fmt.Sprintf("               transitive: [%s]", strings.Join(transitive, ", ")))
				c.Data["needs"] = map[string]any{"direct": direct, "transitive": transitive}
			}
			if target := workflow.JobByName(fl, r.Job); target != nil {
				var outs []string
				for _, o := range target.Outputs {
					outs = append(outs, o.Name)
				}
				desc := "outputs: [" + strings.Join(outs, ", ") + "]"
				if target.Uses != nil {
					desc = "uses: " + target.Uses.Raw + " (outputs come from the callee)"
					if ct := ft.JobCallees[r.Job]; ct != nil && ct.File != nil {
						outs = outs[:0]
						for _, o := range ct.File.Outputs {
							outs = append(outs, o.Name)
						}
						desc += " declares [" + strings.Join(outs, ", ") + "]"
					}
				}
				c.Lines = append(c.Lines, fmt.Sprintf("job '%s'   %s   at :%d", r.Job, desc, target.Pos.Line))
				c.Data["target"] = map[string]any{"job": r.Job, "outputs": outs, "line": target.Pos.Line}
			} else {
				near := strutil.Nearest(r.Job, jobsOf(), 1)
				c.Lines = append(c.Lines, fmt.Sprintf("job '%s'   does not exist; jobs: [%s]", r.Job, strings.Join(jobsOf(), ", ")))
				if len(near) > 0 {
					c.Conclusion = fmt.Sprintf("nearest existing job is '%s'.", near[0])
				}
			}
			break
		}
		for _, r := range workflow.JobsOutputRefs(e.Text) {
			c.Lines = append(c.Lines, fmt.Sprintf("reference      jobs.%s.outputs.%s   at :%d  (workflow output)", r.Job, r.Output, f.Line))
			if target := workflow.JobByName(fl, r.Job); target != nil {
				var outs []string
				for _, o := range target.Outputs {
					outs = append(outs, o.Name)
				}
				c.Lines = append(c.Lines, fmt.Sprintf("job '%s'   outputs: [%s]   at :%d", r.Job, strings.Join(outs, ", "), target.Pos.Line))
			} else {
				c.Lines = append(c.Lines, fmt.Sprintf("jobs: [%s]", strings.Join(jobsOf(), ", ")))
			}
			break
		}
	} else {
		// needs-unknown / needs-self anchored at the needs: item, or output-unused at an output key.
		for _, j := range fl.Jobs {
			for _, n := range j.Needs {
				if n.Pos.Line == f.Line {
					c.Lines = append(c.Lines, fmt.Sprintf("job '%s'   needs '%s'   at :%d", j.Name, n.Name, n.Pos.Line))
					c.Lines = append(c.Lines, "jobs in file:  ["+strings.Join(jobsOf(), ", ")+"]")
					c.Data["job"], c.Data["needs"], c.Data["jobs"] = j.Name, n.Name, jobsOf()
					if near := strutil.Nearest(n.Name, jobsOf(), 1); len(near) > 0 && near[0] != n.Name {
						c.Conclusion = fmt.Sprintf("nearest existing job is '%s'.", near[0])
					}
				}
			}
			for _, o := range j.Outputs {
				if o.Pos.Line == f.Line {
					c.Lines = append(c.Lines, fmt.Sprintf("job '%s'   output '%s'   at :%d", j.Name, o.Name, o.Pos.Line))
					var consumers []string
					for _, e := range fl.Exprs {
						for _, r := range workflow.NeedsOutputRefs(e.Text) {
							if r.Job == j.Name {
								consumers = append(consumers, fmt.Sprintf("needs.%s.outputs.%s at :%d", r.Job, r.Output, e.Pos.Line))
							}
						}
					}
					c.Lines = append(c.Lines, "references to this job's outputs: "+listOrNone(consumers))
					c.Data["consumers"] = consumers
				}
			}
		}
	}
	if c.Conclusion == "" {
		c.Conclusion = f.Message
	}
	return c
}

// --- checkout ---------------------------------------------------------------------------

func checkoutContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "checkout", Data: map[string]any{}}
	sc, st, ok := stepAt(ft, f.Line)
	if !ok || sc.Job == "" {
		return generic(f)
	}
	label := "local action  "
	if !strings.HasPrefix(st.Uses, "./") {
		label = "step          "
	}
	c.Lines = append(c.Lines, fmt.Sprintf("%s %-36s at :%d  (job %s / step %s)", label, st.Uses, st.UsesPos.Line, sc.Job, rules.StepLabel(st)))
	c.Data["action"] = map[string]any{"uses": st.Uses, "line": st.UsesPos.Line, "job": sc.Job, "step": rules.StepLabel(st)}
	lines, data, active := CheckoutLines(ft, sc.Job, st.Index, st.If.Value)
	c.Lines = append(c.Lines, lines...)
	for k, v := range data {
		c.Data[k] = v
	}
	if active != nil && active.Sparse != nil {
		if ref, err := uses.Parse(st.Uses); err == nil {
			covered := checkout.SparseCovers(*active.Sparse, ref.Path)
			c.Lines = append(c.Lines, fmt.Sprintf("sparse list of active root checkout: [%s]", strings.Join(active.Sparse.Entries, ", ")))
			if covered {
				c.Conclusion = fmt.Sprintf("action path '%s' is covered.", ref.Path)
			} else {
				var tried []string
				for _, e := range active.Sparse.Entries {
					tried = append(tried, fmt.Sprintf("'%s' does not cover it", e))
				}
				c.Conclusion = fmt.Sprintf("action path '%s' is not covered: %s.", ref.Path, strings.Join(tried, "; "))
			}
			c.Data["sparse"] = map[string]any{"entries": active.Sparse.Entries, "covered": covered}
		}
	}
	if c.Conclusion == "" {
		c.Conclusion = f.Message
	}
	return c
}

// CheckoutLines renders the checkouts of a job in order (checkout-inference
// §5): one line per checkout with its wrapper chain and wiring, the
// unknown-state line, and the "none" placeholder. before limits the
// listing to steps before that index (-1 for the whole job); consumerIf
// is the consuming step's if:, so a conditional root the consumer shares
// is shown as its active root (review 1 D1). active is the root checkout
// the consumer sees, nil when none.
func CheckoutLines(ft *check.FileTrace, job string, before int, consumerIf string) (lines []string, data map[string]any, active *checkout.Step) {
	data = map[string]any{}
	sc := ft.Scopes[job]
	if sc == nil {
		return []string{"  none"}, data, nil
	}
	lines = append(lines, fmt.Sprintf("checkouts seen in job '%s', in order:", job))
	cos := rules.DirectCheckouts(ft.File, job)
	if ft.Checkouts != nil {
		if got, ok := ft.Checkouts[job]; ok {
			cos = got
		}
	}
	var rows []map[string]any
	unknownAt := -1
	unknownReason := ""
	var matching *checkout.Step
	for _, s := range sc.Steps {
		if (before >= 0 && s.Index >= before) || s.Uses == "" {
			continue
		}
		entries := cos[s.Index]
		if len(entries) == 0 {
			if hint := opaqueActionHint(ft, job, s); hint != "" {
				lines = append(lines, fmt.Sprintf("  step #%-3d %-36s %s", s.Index+1, truncateText(s.Uses, 36), hint))
			}
			continue
		}
		for _, cs := range entries {
			if cs.Unknown {
				lines = append(lines, fmt.Sprintf("  step #%-3d %-36s → unknown  (%s)", s.Index+1, truncateText(s.Uses, 36), cs.Reason))
				rows = append(rows, map[string]any{"line": s.UsesPos.Line, "uses": s.Uses, "target": "unknown", "reason": cs.Reason})
				if unknownAt < 0 {
					unknownAt, unknownReason = s.Index, cs.Reason
				}
				continue
			}
			kind := map[checkout.Target]string{checkout.Root: "root", checkout.Subdir: "subdir", checkout.Dynamic: "dynamic path"}[cs.Target]
			extra := ""
			if cs.Path != "" {
				extra += "  path: " + cs.Path
			}
			if cs.Repository != "" {
				extra += "  repository: " + cs.Repository
			}
			if cs.Sparse != nil {
				extra += "  sparse: [" + strings.Join(cs.Sparse.Entries, ", ") + "]"
			}
			if cs.Conditional {
				extra += "  (conditional)"
			}
			note := ""
			adopt := cs.Target == checkout.Root && (active == nil || !cs.Conditional) || (cs.Target == checkout.Dynamic && active == nil)
			if unknownAt >= 0 {
				adopt = false
			}
			if adopt {
				copyCS := cs
				active = &copyCS
				note = "   ← active root checkout"
			} else if cs.Target == checkout.Root && cs.Conditional && consumerIf != "" && rules.SameCondition(cs.If, consumerIf) {
				copyCS := cs
				matching = &copyCS
				note = "   ← active root checkout (same condition as this step)"
			} else if cs.Target == checkout.Subdir {
				note = "   (ignored for root)"
			}
			lines = append(lines, fmt.Sprintf("  step #%-3d %-36s %-13s%s%s", s.Index+1, truncateText(s.Uses, 36), kind, extra, note))
			row := map[string]any{"line": s.UsesPos.Line, "uses": s.Uses, "target": kind, "path": cs.Path, "repository": cs.Repository, "active": note != "" && cs.Target != checkout.Subdir}
			if cs.IsWrapper() {
				lines = append(lines, wiringLines(cs)...)
				row["via"] = cs.Via
				row["wiring"] = wiringData(cs)
			}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 && len(lines) == 1 {
		lines = append(lines, "  none")
	}
	if unknownAt >= 0 {
		lines = append(lines, fmt.Sprintf("  checkout state unknown after step #%d; local-action rules are not applied to later steps", unknownAt+1))
		data["unknown_after"] = map[string]any{"step": unknownAt + 1, "reason": unknownReason}
	}
	data["checkouts"] = rows
	if matching != nil {
		active = matching
	}
	return lines, data, active
}

// wiringLines renders a wrapper's chain and the wiring behind its
// effective values (checkout-inference §5).
func wiringLines(cs checkout.Step) []string {
	var out []string
	chain := cs.Via
	if len(chain) > 1 {
		chain = chain[1:] // the step's own uses: is on the line above
	}
	out = append(out, fmt.Sprintf("           via %s at its step %d", strings.Join(chain, " → "), cs.ViaStep))
	for _, k := range []string{"path", "repository", "ref", "sparse-checkout"} {
		w, ok := cs.Wiring[k]
		label := k
		if k == "sparse-checkout" {
			label = "sparse"
		}
		value, how := "absent", "wrapper passes nothing"
		if ok {
			value, how = describeWire(k, w)
		}
		out = append(out, fmt.Sprintf("           %-13s %-30s (%s)", label+":", value, how))
	}
	return out
}

func describeWire(k string, w checkout.Wire) (value, how string) {
	switch w.Kind {
	case checkout.WireLiteral:
		return quoteOrOwn(k, w.Value), "fixed by the wrapper"
	case checkout.WirePassthrough:
		d := "no default"
		if w.Default != nil {
			switch w.Default.Kind {
			case checkout.WireLiteral:
				d = "default " + quoteOrOwn(k, w.Default.Value)
			case checkout.WireDynamic:
				d = "default is an expression"
			case checkout.WireAbsent:
				if k == "repository" {
					d = "default github.repository"
				}
			}
		}
		return "caller's with." + w.Input, "inputs." + w.Input + ", " + d
	case checkout.WireFallback:
		return "caller's with." + w.Input, "inputs." + w.Input + " || " + quoteOrOwn(k, w.Value)
	case checkout.WireDynamic:
		return "dynamic", "an expression the wrapper computes"
	}
	return "absent", "wrapper passes nothing"
}

func quoteOrOwn(k, v string) string {
	if v == "" {
		if k == "repository" {
			return "own repo"
		}
		return "''"
	}
	return "'" + v + "'"
}

func wiringData(cs checkout.Step) map[string]any {
	out := map[string]any{}
	for k, w := range cs.Wiring {
		row := map[string]any{"kind": w.Kind.String()}
		if w.Value != "" {
			row["value"] = w.Value
		}
		if w.Input != "" {
			row["input"] = w.Input
		}
		if w.Default != nil && w.Default.Kind != checkout.WireAbsent {
			row["default"] = map[string]any{"kind": w.Default.Kind.String(), "value": w.Default.Value}
		}
		out[k] = row
	}
	return out
}

// opaqueActionHint explains a step that looks like a checkout but cannot
// be inferred (checkout-inference §6): a node or docker action.
func opaqueActionHint(ft *check.FileTrace, job string, s workflow.Step) string {
	if !strings.Contains(strings.ToLower(strings.SplitN(s.Uses, "@", 2)[0]), "checkout") {
		return ""
	}
	if ct := ft.StepCallees[job][s.Index]; ct != nil && ct.File != nil && ct.File.Kind != workflow.KindCompositeAction {
		return "node/docker action: cannot see whether it checks out"
	}
	return ""
}

// --- secrets ------------------------------------------------------------------------------

func secretsContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "secrets", Data: map[string]any{}}
	fl := ft.File
	var declared, required []string
	for _, s := range fl.Secrets {
		declared = append(declared, fmt.Sprintf("%s (:%d)", s.Name, s.Pos.Line))
		if s.Required {
			required = append(required, s.Name)
		}
	}
	// Caller side: a job with secrets: near the line.
	for _, j := range fl.Jobs {
		if j.Uses == nil {
			continue
		}
		hit := j.Uses.Pos.Line == f.Line || j.SecretsPos.Line == f.Line
		for _, s := range j.Secrets {
			if s.Value.Pos.Line == f.Line {
				hit = true
			}
		}
		if !hit {
			continue
		}
		ct := ft.JobCallees[j.Name]
		c.Lines = append(c.Lines, fmt.Sprintf("callsite       job %-32s at :%d", j.Name, j.Uses.Pos.Line))
		var supplied []string
		for _, s := range j.Secrets {
			kind := "expression"
			if !s.Value.IsExpr() {
				kind = "LITERAL"
			}
			supplied = append(supplied, s.Name+" ("+kind+")")
		}
		if j.SecretsInherit {
			supplied = []string{"inherit"}
		}
		c.Lines = append(c.Lines, "supplied       "+listOrNone(supplied))
		c.Data["callsite"] = map[string]any{"job": j.Name, "line": j.Uses.Pos.Line, "supplied": supplied, "inherit": j.SecretsInherit}
		if ct != nil && ct.File != nil {
			var cd, cr []string
			for _, s := range ct.File.Secrets {
				cd = append(cd, s.Name)
				if s.Required {
					cr = append(cr, s.Name)
				}
			}
			c.Lines = append(c.Lines, fmt.Sprintf("callee         %-36s declares: %s   required: %s", ct.Name, listOrNone(cd), listOrNone(cr)))
			c.Data["callee"] = map[string]any{"name": ct.Name, "declared": cd, "required": cr}
		}
		c.Conclusion = f.Message
		return c
	}
	c.Lines = append(c.Lines, "declared       "+listOrNone(declared)+"   required: "+listOrNone(required))
	var referenced []string
	for _, e := range fl.Exprs {
		for _, name := range rules.SecretRefs(e.Text) {
			referenced = append(referenced, fmt.Sprintf("secrets.%s (:%d, %s)", name, e.Pos.Line, e.Key))
		}
	}
	c.Lines = append(c.Lines, "referenced     "+listOrNone(referenced))
	c.Lines = append(c.Lines, "triggers       "+listOrNone(fl.Events))
	// Known callers and whether each passes inherit: the fact that decides
	// the severity (explain review N1).
	var callers []string
	var callerData []map[string]any
	for _, ct := range ft.Callers {
		how := "explicit secrets"
		if ct.Inherit {
			how = "secrets: inherit"
		}
		callers = append(callers, fmt.Sprintf("%s job %s (%s)", ct.File, ct.Job, how))
		callerData = append(callerData, map[string]any{"file": ct.File, "job": ct.Job, "inherit": ct.Inherit})
	}
	if callers == nil {
		c.Lines = append(c.Lines, "callers        none known")
	} else {
		c.Lines = append(c.Lines, "callers        "+strings.Join(callers, "; "))
	}
	c.Data["declared"], c.Data["required"], c.Data["referenced"], c.Data["triggers"], c.Data["callers"] = declared, required, referenced, fl.Events, callerData
	c.Conclusion = f.Message
	return c
}

// --- values -------------------------------------------------------------------------------

var reCmp = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.-]*)\s*(==|!=)\s*'([^']*)'|'([^']*)'\s*(==|!=)\s*([A-Za-z_][A-Za-z0-9_.-]*)`)

func valuesContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "values", Data: map[string]any{}}
	e, ok := exprAt(ft, f.Line)
	if !ok {
		return generic(f)
	}
	c.Lines = append(c.Lines, fmt.Sprintf("expression     %s   at :%d  (%s)", e.Text, f.Line, site(ft, e)))
	c.Data["expression"] = e.Text
	if f.Rule == "value-set-mismatch" {
		for _, m := range reCmp.FindAllStringSubmatch(e.Text, -1) {
			path, lit := m[1], m[3]
			if path == "" {
				path, lit = m[6], m[4]
			}
			set, source := valueSet(path, ft.File.Events)
			if set == nil {
				continue
			}
			if contains(set, lit) {
				continue
			}
			near := strutil.Nearest(lit, set, 1)
			c.Lines = append(c.Lines, fmt.Sprintf("compared path  %s   value set: [%s]   (%s)", path, strings.Join(set, ", "), source))
			c.Lines = append(c.Lines, fmt.Sprintf("literal        %q", lit))
			c.Data["path"], c.Data["set"], c.Data["source"], c.Data["literal"] = path, set, source, lit
			if len(near) > 0 {
				c.Lines = append(c.Lines, fmt.Sprintf("nearest value  %q", near[0]))
				c.Data["nearest"] = near[0]
				c.Conclusion = fmt.Sprintf("%q is not in the set; did you mean %q?", lit, near[0])
			}
		}
	}
	if c.Conclusion == "" {
		c.Conclusion = f.Message
	}
	return c
}

func valueSet(path string, events []string) ([]string, string) {
	switch {
	case strings.HasSuffix(path, ".outcome") || strings.HasSuffix(path, ".conclusion") || strings.HasSuffix(path, ".result"):
		return []string{"success", "failure", "cancelled", "skipped"}, "documented runner values"
	case path == "job.status":
		return []string{"success", "failure", "cancelled"}, "documented runner values"
	case path == "runner.os":
		return []string{"Linux", "Windows", "macOS"}, "documented runner values"
	case path == "runner.arch":
		return []string{"X86", "X64", "ARM", "ARM64"}, "documented runner values"
	case path == "github.ref_type":
		return []string{"branch", "tag"}, "documented values"
	case path == "github.event_name":
		return events, "this file's on: list"
	}
	return nil, ""
}

// --- fetch ----------------------------------------------------------------------------------

func fetchContext(f finding.Finding, ft *check.FileTrace, env Env) Context {
	c := Context{Kind: "fetch", Data: map[string]any{}}
	var ct *check.CalleeTrace
	var raw string
	for _, j := range ft.File.Jobs {
		if j.Uses != nil && j.Uses.Pos.Line == f.Line {
			ct, raw = ft.JobCallees[j.Name], j.Uses.Raw
		}
		for _, st := range j.Steps {
			if st.UsesPos.Line == f.Line && st.Uses != "" {
				ct, raw = ft.StepCallees[j.Name][st.Index], st.Uses
			}
		}
	}
	for _, st := range ft.File.Steps {
		if st.UsesPos.Line == f.Line && st.Uses != "" {
			ct, raw = ft.StepCallees[""][st.Index], st.Uses
		}
	}
	if raw == "" {
		return generic(f)
	}
	c.Lines = append(c.Lines, fmt.Sprintf("reference      %-36s at :%d", raw, f.Line))
	c.Data["reference"] = raw
	if ct != nil {
		c.Lines = append(c.Lines, fmt.Sprintf("resolution     %s%s", ct.How, errSuffix(ct.Err)))
		c.Data["resolution"] = map[string]any{"how": ct.How, "error": ct.Err, "name": ct.Name}
	}
	ref, err := uses.Parse(raw)
	var calls []map[string]any
	if err == nil && ref.Kind == uses.External {
		slug := strings.ToLower(ref.Owner + "/" + ref.Repo)
		for _, l := range env.FetchLog {
			if !strings.Contains(strings.ToLower(l.What), slug) {
				continue
			}
			status := fmt.Sprint(l.Status)
			if l.Err != "" {
				status = "error: " + l.Err
			}
			rl := ""
			if l.RateLimited {
				rl = "  rate-limit headers present"
			}
			c.Lines = append(c.Lines, fmt.Sprintf("  GET %-60s %s%s", l.Path, status, rl))
			calls = append(calls, map[string]any{"path": l.Path, "status": l.Status, "error": l.Err, "rate_limited": l.RateLimited})
		}
	}
	if len(calls) == 0 {
		c.Lines = append(c.Lines, "api calls      none (served from cache, linked, skipped, or offline)")
	}
	c.Data["api_calls"] = calls
	c.Lines = append(c.Lines, "token source   "+orNone(env.TokenSource))
	c.Data["token_source"] = env.TokenSource
	c.Conclusion = f.Message
	return c
}

func errSuffix(err string) string {
	if err == "" {
		return ""
	}
	return ": " + err
}

// --- inputs ----------------------------------------------------------------------------------

func inputsContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "inputs", Data: map[string]any{}}
	byTrigger := map[string][]string{}
	var names []string
	for _, in := range ft.File.Inputs {
		t := in.Trigger
		if t == "" {
			t = "action inputs"
		}
		byTrigger[t] = append(byTrigger[t], fmt.Sprintf("%s (:%d)", in.Name, in.Pos.Line))
		names = append(names, in.Name)
	}
	var triggers []string
	for t := range byTrigger {
		triggers = append(triggers, t)
	}
	sort.Strings(triggers)
	for _, t := range triggers {
		c.Lines = append(c.Lines, fmt.Sprintf("declared       %-18s %s", t+":", strings.Join(byTrigger[t], ", ")))
	}
	if len(triggers) == 0 {
		c.Lines = append(c.Lines, "declared       none")
	}
	c.Data["declared"] = byTrigger
	switch f.Rule {
	case "input-undeclared":
		if e, ok := exprAt(ft, f.Line); ok {
			for _, name := range workflow.InputRefs(e.Text) {
				if contains(names, name) {
					continue
				}
				c.Lines = append(c.Lines, fmt.Sprintf("reference      inputs.%s   at :%d  (%s)", name, f.Line, site(ft, e)))
				c.Data["reference"] = name
				if near := strutil.Nearest(name, names, 1); len(near) > 0 {
					c.Lines = append(c.Lines, "nearest        "+near[0])
					c.Data["nearest"] = near[0]
					c.Conclusion = fmt.Sprintf("'%s' is not declared; nearest declared input is '%s'.", name, near[0])
				}
			}
		}
	case "input-unused":
		var scanned []string
		for _, e := range ft.File.Exprs {
			scanned = append(scanned, fmt.Sprintf(":%d %s", e.Pos.Line, truncateText(e.Text, 40)))
		}
		c.Lines = append(c.Lines, fmt.Sprintf("expressions scanned (%d): %s", len(scanned), listOrNone(scanned)))
		c.Lines = append(c.Lines, "note           github.event.inputs.X is not tracked by this rule")
		c.Data["scanned"] = scanned
	}
	if c.Conclusion == "" {
		c.Conclusion = f.Message
	}
	return c
}

// --- shapes ---------------------------------------------------------------------------------

func shapesContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "shape", Data: map[string]any{}}
	e, ok := exprAt(ft, f.Line)
	if ok {
		c.Lines = append(c.Lines, fmt.Sprintf("expression     %s   at :%d  (%s)", truncateText(e.Text, 60), f.Line, site(ft, e)))
		c.Data["expression"] = e.Text
	}
	if ok && ft.ShapeInputs != nil && f.Rule == "shape-mismatch" {
		// The shape command's why and accepted blocks, from the same
		// inference (spec-shape §7).
		a, err := shapeq.Query(ft, shapeq.Point{Job: e.Job, Step: e.Step}, e.Text)
		if err == nil {
			if a.Mismatch != nil {
				c.Lines = append(c.Lines, "chain          "+a.Mismatch.Chain)
				c.Lines = append(c.Lines, "failing at     "+a.Mismatch.At+": "+a.Mismatch.Msg)
				c.Lines = append(c.Lines, "shape there    "+shape.Render(a.Mismatch.Shape, 4))
				c.Data["chain"], c.Data["at"], c.Data["shape"] = a.Mismatch.Chain, a.Mismatch.At, shape.ToJSON(a.Mismatch.Shape)
				if acc := shapeq.AcceptedLines(a.Mismatch.Shape, 3); len(acc) > 0 {
					c.Lines = append(c.Lines, "accepted there:")
					for _, l := range acc {
						c.Lines = append(c.Lines, "  "+l)
					}
				}
			}
			if len(a.Chains) > 0 {
				c.Lines = append(c.Lines, "why:")
				var why []map[string]any
				for _, ch := range a.Chains {
					for _, l := range shapeq.WhyLines(ch, 3) {
						c.Lines = append(c.Lines, "  "+l)
					}
					if ch.Shape != nil {
						for _, p := range ch.Shape.Prov {
							why = append(why, map[string]any{"chain": ch.Text, "kind": p.Kind, "text": p.Text, "line": p.Line})
						}
					}
				}
				c.Data["why"] = why
			}
			c.Conclusion = f.Message
			return c
		}
	}
	// The message carries chain, failing step and provenance.
	msg := f.Message
	if i := strings.Index(msg, ": at step "); i >= 0 {
		c.Lines = append(c.Lines, "chain          "+msg[:i])
		rest := msg[i+2:]
		if j := strings.Index(rest, " (from "); j >= 0 {
			c.Lines = append(c.Lines, "failing step   "+rest[:j])
			c.Lines = append(c.Lines, "shape from     "+strings.TrimSuffix(rest[j+7:], ")"))
			c.Data["chain"], c.Data["step"], c.Data["from"] = msg[:i], rest[:j], strings.TrimSuffix(rest[j+7:], ")")
		} else {
			c.Lines = append(c.Lines, "failing step   "+rest)
			c.Data["chain"], c.Data["step"] = msg[:i], rest
		}
	} else {
		c.Lines = append(c.Lines, "detail         "+msg)
	}
	c.Lines = append(c.Lines, "accepted       anything unknown is 'any' and never reported; open objects accept any field")
	c.Conclusion = f.Message
	return c
}

// --- suppress ---------------------------------------------------------------------------------

func suppressContext(f finding.Finding, ft *check.FileTrace) Context {
	c := Context{Kind: "suppress", Data: map[string]any{}}
	if ft.Suppressions == nil {
		return generic(f)
	}
	var ds []map[string]any
	for _, d := range ft.Suppressions.Directives {
		if d.Line != f.Line && d.Anchor != f.Line {
			continue
		}
		where := fmt.Sprintf("line %d", d.Anchor)
		if d.Kind != "line" {
			where = fmt.Sprintf("lines %d-%d", d.From, d.To)
		}
		names := strings.Join(d.Rules, ", ")
		if d.Wildcard {
			names = "*"
		}
		if names == "" {
			names = "(no rule)"
		}
		c.Lines = append(c.Lines, fmt.Sprintf("directive      jobsworth-ignore%s %s   at :%d   applies to %s", kindSuffix(d.Kind), names, d.Line, where))
		if d.Reason != "" {
			c.Lines = append(c.Lines, "reason         "+d.Reason)
		}
		ds = append(ds, map[string]any{"line": d.Line, "kind": d.Kind, "rules": d.Rules, "wildcard": d.Wildcard, "reason": d.Reason, "applies": where})
	}
	if len(ds) == 0 {
		c.Lines = append(c.Lines, "directive      none parsed on this line")
	}
	c.Data["directives"] = ds
	c.Conclusion = f.Message
	return c
}

func kindSuffix(kind string) string {
	switch kind {
	case "file":
		return "-file"
	case "region":
		return "-start"
	}
	return ""
}
