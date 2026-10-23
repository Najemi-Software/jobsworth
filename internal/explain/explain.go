// Package explain renders findings in context (spec-explain): for a
// finding at a location it shows what the analyser had in hand when it
// fired, then the rule doc with the actual anchor line substituted.
package explain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Location is a parsed <file>:<line>[:<column>] argument.
type Location struct {
	// File is the absolute path, or a fetched display name
	// (owner/repo/path@ref) when Remote is set.
	File   string
	Remote bool
	Line   int
	Column int
}

var reRemoteLoc = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[^@:]+)?@[^:]+):(\d+)(?::(\d+))?$`)

// ParseLocation reads file:line[:col]. Relative files resolve against cwd.
// A registered rule id is not a location; the caller checks that first.
func ParseLocation(arg, cwd string) (Location, error) {
	if m := reRemoteLoc.FindStringSubmatch(arg); m != nil && !strings.HasPrefix(arg, ".") && !strings.HasPrefix(arg, "/") {
		if _, err := os.Stat(strings.SplitN(arg, ":", 2)[0]); err != nil {
			line, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			return Location{File: m[1], Remote: true, Line: line, Column: col}, nil
		}
	}
	parts := strings.Split(arg, ":")
	file := parts[0]
	loc := Location{}
	if len(parts) > 3 || len(parts) < 1 || file == "" {
		return loc, fmt.Errorf("%q is not a rule id or a file:line location", arg)
	}
	if len(parts) >= 2 {
		n, err := strconv.Atoi(parts[1])
		if err != nil || n < 1 {
			return loc, fmt.Errorf("%q: line must be a positive number", arg)
		}
		loc.Line = n
	}
	if len(parts) == 3 {
		n, err := strconv.Atoi(parts[2])
		if err != nil || n < 1 {
			return loc, fmt.Errorf("%q: column must be a positive number", arg)
		}
		loc.Column = n
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	st, err := os.Stat(file)
	if err != nil {
		return loc, fmt.Errorf("%q is not a rule id or a file", parts[0])
	}
	if st.IsDir() {
		return loc, fmt.Errorf("%s is a directory", parts[0])
	}
	loc.File = filepath.Clean(file)
	return loc, nil
}

// Explanation is one finding in context.
type Explanation struct {
	Finding    finding.Finding
	Rule       rules.Rule
	Suppressed bool
	// SuppressedBy is the directive line and reason when suppressed.
	SuppressedBy   int
	SuppressReason string
	Context        Context
	Doc            *rules.Doc
}

// Context is a family-specific block: text lines (already relative to the
// "context:" header) and the JSON object (spec-explain §4.2).
type Context struct {
	Kind  string
	Lines []string
	Data  map[string]any
	// Conclusion is highlighted in text output.
	Conclusion string
	// SeeAlso lists related findings on other lines.
	SeeAlso []SeeAlso
}

// SeeAlso is a related finding.
type SeeAlso struct {
	Rule string `json:"rule"`
	Line int    `json:"line"`
}

// Env is what the renderers may consult beyond the trace.
type Env struct {
	// TokenSource names where the token came from, for fetch context.
	TokenSource string
	// FetchLog is the fetch client's request log.
	FetchLog []FetchLogEntry
	// Cwd shortens local paths in text.
	Cwd string
	// Trace is the whole run's trace, for findings on files that were
	// never validated (an unparseable callee) whose context lives in
	// their callers.
	Trace *check.Trace
}

// FetchLogEntry mirrors fetch.LogEntry without importing fetch here.
type FetchLogEntry struct {
	What        string
	Path        string
	Status      int
	RateLimited bool
	Err         string
}

// Result is the outcome of explaining a location.
type Result struct {
	File string
	// Explanations for findings at the location, severity order.
	Explanations []Explanation
	// Redirected is set when the line fell inside a run: block.
	Redirected     bool
	RedirectedFrom int
	RedirectedTo   int
	// Nearby lists findings above and below when none matched.
	Nearby []finding.Finding
	// WhyPassed explains an accepted steps.* reference on the line.
	WhyPassed []string
	// CalleeFindings lists findings attributed to the callee referenced on
	// the line (spec-explain §7).
	CalleeFindings []CalleeFindings
	// Others are findings on the same line not selected by a column.
	Others []finding.Finding
	// Total is the number of findings in the file.
	Total int
	// RequestedLine is the line asked for (after redirect).
	RequestedLine int
}

// CalleeFindings points at findings in a callee.
type CalleeFindings struct {
	Callee   string
	Findings []finding.Finding
}

// Options control what Explain produces.
type Options struct {
	Rule  string // restrict to one rule
	Brief bool   // omit the doc
	Env   Env
}

// AtLocation explains the findings of res (a check of the file) anchored at
// loc, using the trace captured during the run.
func AtLocation(res check.Result, tr *check.Trace, loc Location, opts Options) Result {
	out := Result{File: loc.File}
	ft := tr.Get(loc.File)
	all := findingsOf(res, loc.File)
	if loc.Line == 0 && opts.Rule != "" {
		// `<file> --rule <id>`: every finding of the rule the walk from this
		// file produced, including those attributed to callees.
		for _, fr := range res.Files {
			if fr.Name != loc.File {
				all = append(all, findingsOf(res, fr.Name)...)
			}
		}
	}
	out.Total = len(all)
	line := loc.Line
	if ft != nil && line > 0 {
		if runLine, ok := runBlockStart(ft, line); ok && runLine != line {
			out.Redirected, out.RedirectedFrom, out.RedirectedTo = true, line, runLine
			line = runLine
		}
	}
	out.RequestedLine = line
	var at []finding.Finding
	for _, f := range all {
		if opts.Rule != "" && f.Rule != opts.Rule {
			continue
		}
		if line == 0 || f.Line == line {
			at = append(at, f)
		}
	}
	if loc.Column > 0 && len(at) > 1 {
		var picked, others []finding.Finding
		for _, f := range at {
			if f.Column == loc.Column {
				picked = append(picked, f)
			} else {
				others = append(others, f)
			}
		}
		if len(picked) > 0 {
			at, out.Others = picked, others
		}
	}
	finding.Sort(at)
	for _, f := range at {
		ftf := ft
		if f.File != loc.File {
			ftf = tr.Get(f.File)
		}
		opts.Env.Trace = tr
		out.Explanations = append(out.Explanations, One(f, ftf, res, opts))
	}
	if len(at) == 0 && line > 0 {
		out.Nearby = nearby(all, line)
		if ft != nil {
			out.WhyPassed = whyPassed(ft, line)
		}
	}
	if ft != nil && line > 0 {
		out.CalleeFindings = calleeFindings(ft, res, line)
	}
	return out
}

// One explains a single finding. res is used for see-also lookups.
func One(f finding.Finding, ft *check.FileTrace, res check.Result, opts Options) Explanation {
	r, _, _ := rules.Lookup(f.Rule)
	ex := Explanation{Finding: f, Rule: r}
	if f.Reason != "" || isSuppressed(res, f) {
		ex.Suppressed = true
		ex.SuppressReason = f.Reason
		if ft != nil && ft.Suppressions != nil {
			for _, d := range ft.Suppressions.Directives {
				if contains(d.Rules, f.Rule) || d.Wildcard {
					if (d.Kind == "line" && d.Anchor == f.Line) || (d.Kind != "line" && f.Line >= d.From && f.Line <= d.To) {
						ex.SuppressedBy = d.Line
						if ex.SuppressReason == "" {
							ex.SuppressReason = d.Reason
						}
					}
				}
			}
		}
	}
	ex.Context = render(f, ft, res, opts.Env)
	if ex.Context.Conclusion == f.Message {
		ex.Context.Conclusion = ""
	}
	if !opts.Brief {
		if doc, err := rules.LoadDoc(r.ID); err == nil {
			ex.Doc = &doc
		}
	}
	return ex
}

func isSuppressed(res check.Result, f finding.Finding) bool {
	for _, fr := range res.Files {
		for _, s := range fr.Suppressed {
			if s.Rule == f.Rule && s.Line == f.Line && s.Message == f.Message {
				return true
			}
		}
	}
	return false
}

// findingsOf returns kept and suppressed findings for one file name.
func findingsOf(res check.Result, name string) []finding.Finding {
	var out []finding.Finding
	for _, fr := range res.Files {
		if fr.Name != name {
			continue
		}
		out = append(out, fr.Findings...)
		for _, s := range fr.Suppressed {
			if s.Reason == "" {
				s.Reason = "(no reason given)"
			}
			out = append(out, s)
		}
	}
	finding.Sort(out)
	return out
}

// runBlockStart reports the run: line of the step whose script contains
// line (spec-explain §7).
func runBlockStart(ft *check.FileTrace, line int) (int, bool) {
	scopes := []string{""}
	if ft.File.Kind == workflow.KindWorkflow {
		scopes = scopes[:0]
		for _, j := range ft.File.Jobs {
			scopes = append(scopes, j.Name)
		}
	}
	for _, job := range scopes {
		for _, st := range workflow.StepsOf(ft.File, job) {
			if !st.HasRun || !st.RunBlock {
				continue
			}
			first := st.RunPos.Line + 1
			last := st.RunPos.Line + strings.Count(strings.TrimRight(st.Run, "\n"), "\n") + 1
			if line >= first && line <= last {
				return st.RunPos.Line, true
			}
		}
	}
	return 0, false
}

// absLine maps a script line to the file line the editor shows.
func absLine(st workflow.Step, scriptLine int) int {
	if st.RunBlock {
		return st.RunPos.Line + scriptLine
	}
	return st.RunPos.Line + scriptLine - 1
}

// nearby lists the closest findings above and below a line.
func nearby(all []finding.Finding, line int) []finding.Finding {
	var above, below []finding.Finding
	for _, f := range all {
		if f.Line < line {
			above = append(above, f)
		} else if f.Line > line {
			below = append(below, f)
		}
	}
	var out []finding.Finding
	if n := len(above); n > 0 {
		start := n - 2
		if start < 0 {
			start = 0
		}
		out = append(out, above[start:]...)
	}
	if len(below) > 2 {
		below = below[:2]
	}
	out = append(out, below...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// whyPassed explains an accepted steps.X.outputs.Y reference on the line:
// an open target or a conditional write (spec-explain §3.1).
func whyPassed(ft *check.FileTrace, line int) []string {
	var out []string
	for _, e := range ft.File.Exprs {
		if e.Pos.Line != line {
			continue
		}
		sc := ft.Scopes[e.Job]
		if sc == nil {
			continue
		}
		for _, r := range workflow.StepRefs(e.Text) {
			if r.Field != "outputs" {
				continue
			}
			st, ok := sc.StepByID(r.ID)
			if !ok {
				continue
			}
			a := sc.Bash[st.Index]
			if a == nil || a.ParseErr != nil {
				if st.Uses != "" {
					out = append(out, fmt.Sprintf("The reference steps.%s.outputs.%s was accepted because step '%s' uses %s, whose outputs come from the action's declarations.", r.ID, r.Output, r.ID, st.Uses))
				} else if a != nil && a.ParseErr != nil {
					out = append(out, fmt.Sprintf("The reference steps.%s.outputs.%s was not checked because step '%s' failed to parse as bash (bash-syntax).", r.ID, r.Output, r.ID))
				} else {
					out = append(out, fmt.Sprintf("The reference steps.%s.outputs.%s was not checked because step '%s' does not run bash.", r.ID, r.Output, r.ID))
				}
				continue
			}
			ts := a.Targets[0]
			if _, written := ts.Writes[r.Output]; written {
				continue
			}
			if ts.Open {
				out = append(out, fmt.Sprintf("The reference steps.%s.outputs.%s was accepted because step '%s' has an OPEN GITHUB_OUTPUT target:", r.ID, r.Output, r.ID))
				for _, s := range a.Statements {
					if s.Open != "" {
						out = append(out, fmt.Sprintf("    :%d  %-44s opaque: %s", absLine(st, s.Line), s.Text, s.Open))
					}
				}
				out = append(out, "References to unknown keys on an open target are accepted. To validate them, inline the script or have it write a fixed key list.")
			}
		}
	}
	return out
}

// calleeFindings lists findings attributed to the callee referenced on the
// line.
func calleeFindings(ft *check.FileTrace, res check.Result, line int) []CalleeFindings {
	var out []CalleeFindings
	seen := map[string]bool{}
	add := func(ct *check.CalleeTrace) {
		if ct == nil || ct.Name == "" || seen[ct.Name] {
			return
		}
		seen[ct.Name] = true
		var fs []finding.Finding
		for _, fr := range res.Files {
			if fr.Name == ct.Name {
				fs = append(fs, fr.Findings...)
			}
		}
		if len(fs) > 0 {
			out = append(out, CalleeFindings{Callee: ct.Name, Findings: fs})
		}
	}
	for _, j := range ft.File.Jobs {
		if j.Uses != nil && j.Uses.Pos.Line == line {
			add(ft.JobCallees[j.Name])
		}
		for _, st := range j.Steps {
			if st.Uses != "" && st.UsesPos.Line == line {
				add(ft.StepCallees[j.Name][st.Index])
			}
		}
	}
	for _, st := range ft.File.Steps {
		if st.Uses != "" && st.UsesPos.Line == line {
			add(ft.StepCallees[""][st.Index])
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
