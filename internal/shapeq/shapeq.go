// Package shapeq answers "what shape is this expression, and why"
// (spec-shape): it resolves the evaluation point, runs the inference
// engine with provenance on, and renders the answer as text or json. The
// same renderer serves `jobsworth shape` and the explain context block.
package shapeq

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Point is where an expression is evaluated (spec-shape §3).
type Point struct {
	Job string
	// Step is the 0-based step index, or -1 for "after the last step".
	Step int
	// StepLabel names the step for output ("" when Step < 0).
	StepLabel string
}

// PointError is a bad --at or an expression that needs one (exit 1).
type PointError struct{ Msg string }

func (e *PointError) Error() string { return e.Msg }

// ResolvePoint turns `--at <job>[/<step>]` (or `--at <step>` for an
// action) into a Point, applying the §3.2 defaults from the expression's
// roots when at is empty.
func ResolvePoint(ft *check.FileTrace, at string, exprText string) (Point, error) {
	f := ft.File
	isAction := f.Kind != workflow.KindWorkflow
	p := Point{Step: -1}
	if at == "" {
		roots := rootsOf(exprText)
		needsJob := false
		for _, r := range roots {
			switch r {
			case "needs", "steps", "env", "matrix", "job", "strategy":
				needsJob = true
			}
		}
		if !needsJob || isAction {
			return p, nil
		}
		if len(f.Jobs) == 1 {
			p.Job = f.Jobs[0].Name
			return p, nil
		}
		var names []string
		for _, j := range f.Jobs {
			names = append(names, j.Name)
		}
		return p, &PointError{Msg: fmt.Sprintf("the expression depends on the job; pass --at <job>[/<step>] (jobs: %s)", strings.Join(names, ", "))}
	}
	jobPart, stepPart := at, ""
	if i := strings.Index(at, "/"); i >= 0 {
		jobPart, stepPart = at[:i], at[i+1:]
	}
	if isAction {
		if stepPart != "" {
			return p, &PointError{Msg: "actions have no jobs; use --at <step>"}
		}
		stepPart, jobPart = jobPart, ""
	} else {
		if workflow.JobByName(f, jobPart) == nil {
			var names []string
			for _, j := range f.Jobs {
				names = append(names, j.Name)
			}
			return p, &PointError{Msg: fmt.Sprintf("no job named '%s' (jobs: %s)", jobPart, strings.Join(names, ", "))}
		}
		p.Job = jobPart
	}
	if stepPart == "" {
		return p, nil
	}
	steps := workflow.StepsOf(f, p.Job)
	if strings.HasPrefix(stepPart, "#") {
		n, err := strconv.Atoi(stepPart[1:])
		if err != nil || n < 1 || n > len(steps) {
			return p, &PointError{Msg: fmt.Sprintf("no step %s (the scope has %d steps)", stepPart, len(steps))}
		}
		p.Step = n - 1
		p.StepLabel = stepPart
		return p, nil
	}
	for _, st := range steps {
		if st.ID == stepPart {
			p.Step = st.Index
			p.StepLabel = st.ID
			return p, nil
		}
	}
	var ids []string
	for _, st := range steps {
		if st.ID != "" {
			ids = append(ids, st.ID)
		}
	}
	return p, &PointError{Msg: fmt.Sprintf("no step with id '%s' (declared ids: %s)", stepPart, listOrNone(ids))}
}

// rootsOf lists the context roots an expression touches.
func rootsOf(text string) []string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "${{") && strings.HasSuffix(t, "}}") {
		t = strings.TrimSpace(t[3 : len(t)-2])
	}
	node, err := expr.Parse(t)
	if err != nil {
		return nil
	}
	var out []string
	var walk func(c expr.Chain)
	walk = func(c expr.Chain) {
		out = append(out, strings.ToLower(c.Root))
		if c.Inner != nil {
			walk(*c.Inner)
		}
	}
	for _, c := range expr.Chains(node) {
		walk(c)
	}
	return out
}

// Error is why a query failed (exit 1).
type Error struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
	At      string `json:"at,omitempty"`
}

// Answer is one evaluated expression (spec-shape §4).
type Answer struct {
	File       string
	Point      Point
	Expression string
	Shape      *shape.Shape
	Chains     []rules.ChainShape
	Error      *Error
	// Mismatch keeps the failing step's shape for the mismatch block.
	Mismatch *rules.Mismatch
}

// OK reports whether the query succeeded.
func (a Answer) OK() bool { return a.Error == nil }

// Query evaluates an expression at a point over a traced file.
func Query(ft *check.FileTrace, p Point, text string) (Answer, error) {
	in := rules.ShapeInputs{}
	if ft.ShapeInputs != nil {
		in = *ft.ShapeInputs
	}
	a := Answer{Point: p}
	if err := checkPoint(ft, p, text); err != nil {
		a.Error = err
		a.Expression = strings.TrimSpace(text)
		return a, nil
	}
	res, err := rules.EvalShape(ft.File, ft.Scopes, in, p.Job, p.Step, text)
	if err != nil {
		return a, err
	}
	a.Expression = res.Expr
	a.Shape = res.Shape
	a.Chains = res.Chains
	if res.Mismatch != nil {
		a.Mismatch = res.Mismatch
		a.Error = &Error{Rule: "shape-mismatch", Message: res.Mismatch.Message, At: res.Mismatch.At}
		return a, nil
	}
	for _, f := range res.Problems {
		if f.Rule != "shape-mismatch" {
			a.Error = &Error{Rule: f.Rule, Message: f.Message}
			return a, nil
		}
	}
	// A value-set comparison against a literal outside the set is the
	// same error check reports (§5).
	tmp := &workflow.File{Events: ft.File.Events, Exprs: []workflow.Expr{{Text: res.Expr, Pos: workflow.Pos{Line: 1, Column: 1}}}}
	for _, f := range rules.CheckValueSets(tmp) {
		a.Error = &Error{Rule: f.Rule, Message: f.Message}
		return a, nil
	}
	return a, nil
}

// checkPoint applies the §3.2 and §8 point rules: steps.ID must exist
// and be earlier than the point; needs.J must be needed by the job.
func checkPoint(ft *check.FileTrace, p Point, text string) *Error {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "${{") && strings.HasSuffix(t, "}}") {
		t = strings.TrimSpace(t[3 : len(t)-2])
	}
	node, err := expr.Parse(t)
	if err != nil {
		return nil // reported by Query as a parse error
	}
	for _, c := range expr.Chains(node) {
		for ch := &c; ch != nil; ch = ch.Inner {
			if len(ch.Segs) == 0 || ch.Segs[0].Dynamic || ch.Segs[0].IsIndex || ch.Segs[0].Wildcard {
				continue
			}
			name := ch.Segs[0].Name
			switch strings.ToLower(ch.Root) {
			case "steps":
				steps := workflow.StepsOf(ft.File, p.Job)
				idx := -1
				var ids []string
				for _, st := range steps {
					if st.ID != "" {
						ids = append(ids, st.ID)
					}
					if st.ID == name {
						idx = st.Index
					}
				}
				if idx < 0 {
					scope := "the action"
					if p.Job != "" {
						scope = "job '" + p.Job + "'"
					}
					return &Error{Rule: "step-id-unknown", Message: fmt.Sprintf("steps.%s is referenced but no step has id %q in %s (declared ids: %s)", name, name, scope, listOrNone(ids))}
				}
				if p.Step >= 0 && idx >= p.Step {
					return &Error{Rule: "step-forward-ref", Message: fmt.Sprintf("steps.%s is unknown at this point: step '%s' (line %d) is the step itself or a later one", name, name, steps[idx].Pos.Line)}
				}
			case "needs":
				if p.Job == "" {
					continue
				}
				j := workflow.JobByName(ft.File, p.Job)
				if j == nil {
					continue
				}
				needed := false
				for _, n := range j.Needs {
					if n.Name == name {
						needed = true
					}
				}
				if !needed {
					return &Error{Rule: "needs-output-not-needed", Message: fmt.Sprintf("job '%s' references needs.%s, but '%s' is not in its needs", p.Job, name, name)}
				}
			}
		}
	}
	return nil
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// --- text rendering (spec-shape §4.2) ----------------------------------------------

// Options tunes rendering.
type Options struct {
	Depth int
	Why   bool
}

// Text renders an answer.
func Text(a Answer, o Options) string {
	if o.Depth <= 0 {
		o.Depth = 4
	}
	var b strings.Builder
	b.WriteString(a.Expression + "\n")
	switch {
	case a.Error != nil && a.Mismatch != nil:
		fmt.Fprintf(&b, "  ✗ %s\n", a.Mismatch.Msg)
		fmt.Fprintf(&b, "    at: %s\n", a.Mismatch.At)
		fmt.Fprintf(&b, "    shape there: %s\n", shape.Render(a.Mismatch.Shape, o.Depth))
	case a.Error != nil:
		fmt.Fprintf(&b, "  ✗ %s: %s\n", a.Error.Rule, a.Error.Message)
	default:
		fmt.Fprintf(&b, "  = %s\n", shape.Render(a.Shape, o.Depth))
	}
	if o.Why && len(a.Chains) > 0 {
		b.WriteString("\nwhy:\n")
		for _, c := range a.Chains {
			for _, line := range WhyLines(c, o.Depth) {
				b.WriteString(line + "\n")
			}
		}
	}
	if a.Error == nil {
		if acc := AcceptedLines(a.Shape, o.Depth); len(acc) > 0 {
			b.WriteString("\naccepted:\n")
			for _, line := range acc {
				b.WriteString(line + "\n")
			}
		}
	}
	return b.String()
}

// WhyLines renders one chain's provenance block: the chain, its sources
// nested by depth, then the expression-level transforms.
func WhyLines(c rules.ChainShape, depth int) []string {
	lines := []string{"  " + c.Text}
	if c.Shape == nil {
		return lines
	}
	var transforms []string
	for _, p := range c.Shape.Prov {
		if p.Transform {
			transforms = append(transforms, "  "+ProvLine(p, depth))
			continue
		}
		lines = append(lines, strings.Repeat("  ", 2+p.Depth)+ProvLine(p, depth))
	}
	if len(c.Shape.Prov) == 0 || !hasSource(c.Shape.Prov) {
		lines = append(lines, "    "+shape.Render(c.Shape, depth))
	}
	return append(lines, transforms...)
}

func hasSource(ps []shape.Prov) bool {
	for _, p := range ps {
		if !p.Transform {
			return true
		}
	}
	return false
}

// ProvLine is the one-line form of a provenance entry (§4.2 table).
func ProvLine(p shape.Prov, depth int) string {
	line := func() string {
		if p.Line > 0 {
			return fmt.Sprintf(" (line %d)", p.Line)
		}
		return ""
	}
	switch p.Kind {
	case "declared_input", "input_default":
		return p.Text + line()
	case "caller_with":
		return p.Text
	case "intersection":
		return "intersection → " + shape.Render(p.Shape, depth)
	case "no_callers":
		return p.Text
	case "static_schema":
		return p.Text
	case "matrix":
		return p.Text + line()
	case "env_block":
		return p.Text + line()
	case "github_env_write":
		return fmt.Sprintf("written to $GITHUB_ENV by step '%s'%s:  %s", p.Step, line(), p.Text)
	case "github_output_write":
		return fmt.Sprintf("written by step '%s'%s:  %s", p.Step, line(), p.Text)
	case "bash_var":
		return fmt.Sprintf("$%s%s:  %s", p.Name, line(), p.Text)
	case "file_read":
		return fmt.Sprintf("file %s parsed as %s: %s", p.Path, strings.ToUpper(p.Format), shape.Render(p.Shape, depth))
	case "jq":
		flags := ""
		if len(p.Flags) > 0 {
			flags = " " + strings.Join(p.Flags, " ")
		}
		if p.Text != "" {
			return fmt.Sprintf("jq '%s'%s: %s", p.Filter, flags, p.Text)
		}
		return fmt.Sprintf("jq '%s'%s: %s → %s", p.Filter, flags, shape.Render(p.Input, depth), shape.Render(p.Output, depth))
	case "builtin", "fromjson":
		return p.Text
	case "lift":
		return p.Text + line()
	case "opaque":
		return "opaque: " + p.Text
	case "access":
		return p.Text + " → " + shape.Render(p.Shape, depth)
	}
	return p.Kind + ": " + p.Text
}

// AcceptedLines renders the accepted block (§4.2).
func AcceptedLines(s *shape.Shape, depth int) []string {
	if s == nil || s.Kind == shape.Any {
		return nil
	}
	if s.Kind == shape.String && len(s.Values) > 0 {
		var out []string
		for _, v := range s.Values {
			out = append(out, "  "+v)
		}
		return out
	}
	acc := shape.Accepted(s)
	if len(acc) == 0 {
		return []string{fmt.Sprintf("  no member access accepted on %s", shape.Render(s, 1))}
	}
	width := 0
	for _, a := range acc {
		if len(a.Access) > width {
			width = len(a.Access)
		}
	}
	var out []string
	for _, a := range acc {
		out = append(out, fmt.Sprintf("  %-*s  → %s", width, a.Access, shape.Render(a.Shape, depth)))
	}
	return out
}

// --- json (spec-shape §4.3) -----------------------------------------------------

// Document is the json form of an answer.
type Document struct {
	Schema     int              `json:"schema"`
	File       string           `json:"file"`
	At         At               `json:"at"`
	Expression string           `json:"expression"`
	OK         bool             `json:"ok"`
	Shape      map[string]any   `json:"shape"`
	Render     string           `json:"render"`
	Why        []map[string]any `json:"why,omitempty"`
	Accepted   []AcceptedJSON   `json:"accepted,omitempty"`
	Error      *Error           `json:"error"`
	// Line and Site are set for --all rows.
	Line int    `json:"line,omitempty"`
	Site string `json:"site,omitempty"`
}

// At is the evaluation point in json.
type At struct {
	Job  *string `json:"job"`
	Step *string `json:"step"`
}

// AcceptedJSON is one accepted access in json.
type AcceptedJSON struct {
	Access string         `json:"access"`
	Shape  map[string]any `json:"shape"`
}

// JSON builds the document; why is included when wanted.
func JSON(a Answer, why bool, depth int) Document {
	if depth <= 0 {
		depth = 4
	}
	d := Document{Schema: 1, File: a.File, Expression: a.Expression, OK: a.Error == nil, Error: a.Error}
	if a.Point.Job != "" {
		j := a.Point.Job
		d.At.Job = &j
	}
	if a.Point.Step >= 0 {
		s := a.Point.StepLabel
		d.At.Step = &s
	}
	s := a.Shape
	if a.Mismatch != nil {
		s = a.Mismatch.Shape
	}
	d.Shape = shape.ToJSON(s)
	d.Render = shape.Render(s, depth)
	if why {
		d.Why = []map[string]any{}
		for _, c := range a.Chains {
			if c.Shape == nil {
				continue
			}
			for _, p := range c.Shape.Prov {
				d.Why = append(d.Why, provJSON(c.Text, p))
			}
		}
	}
	if a.Error == nil {
		d.Accepted = []AcceptedJSON{}
		for _, acc := range shape.Accepted(s) {
			d.Accepted = append(d.Accepted, AcceptedJSON{Access: acc.Access, Shape: shape.ToJSON(acc.Shape)})
		}
	}
	return d
}

func provJSON(chain string, p shape.Prov) map[string]any {
	m := map[string]any{"kind": p.Kind, "chain": chain}
	set := func(k string, v any) {
		switch x := v.(type) {
		case string:
			if x != "" {
				m[k] = x
			}
		case int:
			if x != 0 {
				m[k] = x
			}
		case []string:
			if len(x) > 0 {
				m[k] = x
			}
		case *shape.Shape:
			if x != nil {
				m[k] = shape.ToJSON(x)
			}
		}
	}
	set("text", p.Text)
	set("file", p.File)
	set("line", p.Line)
	set("step", p.Step)
	set("name", p.Name)
	set("path", p.Path)
	set("format", p.Format)
	set("filter", p.Filter)
	set("flags", p.Flags)
	set("input", p.Input)
	set("output", p.Output)
	set("shape", p.Shape)
	return m
}

// --- --all (spec-shape §6) ------------------------------------------------------

// Row is one expression of a file for --all.
type Row struct {
	Expr   workflow.Expr
	Site   string
	Answer Answer
}

// All evaluates every expression of the file at its own point, in
// document order; job restricts to one job.
func All(ft *check.FileTrace, job string) []Row {
	var rows []Row
	Each(ft, job, func(r Row) { rows = append(rows, r) })
	return rows
}

// OpaqueReason is the first opaque provenance reason of an Any answer, or
// "".
func OpaqueReason(a Answer) string {
	for _, c := range a.Chains {
		if c.Shape == nil {
			continue
		}
		for _, p := range c.Shape.Prov {
			if p.Kind == "opaque" {
				return p.Text
			}
		}
	}
	return ""
}

// Each evaluates every expression of the file (or of one job) in document
// order, handing each row to fn as soon as it is known (§6, streamed).
func Each(ft *check.FileTrace, job string, fn func(Row)) {
	for _, x := range ft.File.Exprs {
		if job != "" && x.Job != job {
			continue
		}
		p := Point{Job: x.Job, Step: x.Step}
		if x.Step >= 0 {
			steps := workflow.StepsOf(ft.File, x.Job)
			if x.Step < len(steps) {
				p.StepLabel = stepLabel(steps[x.Step])
			}
		}
		a, err := Query(ft, p, x.Text)
		if err != nil {
			a = Answer{Point: p, Expression: x.Text, Error: &Error{Rule: "expression-syntax", Message: err.Error()}}
		}
		fn(Row{Expr: x, Site: Site(ft.File, x), Answer: a})
	}
}

// Site names where an expression sits: "job build / step #2 / with.x".
func Site(f *workflow.File, x workflow.Expr) string {
	var parts []string
	if x.Job != "" {
		parts = append(parts, "job "+x.Job)
	}
	if x.Step >= 0 {
		steps := workflow.StepsOf(f, x.Job)
		if x.Step < len(steps) {
			parts = append(parts, "step "+stepLabel(steps[x.Step]))
		}
	}
	switch {
	case x.InOutputs:
		parts = append(parts, "outputs")
	case x.Key != "":
		parts = append(parts, x.Key)
	}
	if len(parts) == 0 {
		return "file"
	}
	return strings.Join(parts, " / ")
}

func stepLabel(st workflow.Step) string {
	if st.ID != "" {
		return st.ID
	}
	return "#" + strconv.Itoa(st.Index+1)
}
