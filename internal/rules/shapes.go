package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/contextschema"
	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/jq"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// ShapeInputs is what the walker knows about a file's neighbours (medium
// §6.2.4, §6.2.11): shapes lifted from callees and supplied by callers.
type ShapeInputs struct {
	// JobCallee maps a uses: job to its callee's workflow_call output shapes.
	JobCallee map[string]map[string]*shape.Shape
	// StepCallee maps job -> step index -> the action's output shapes.
	StepCallee map[string]map[int]map[string]*shape.Shape
	// CallerInputs maps an input name to the intersected shape of the JSON
	// every caller passes for it (nil when no caller supplied JSON).
	CallerInputs map[string]*shape.Shape
	// CallerSites lists, per input, where callers supplied it
	// ("file:line (job j): {...}"), for provenance.
	CallerSites map[string][]string
	// CallerLiterals maps an input to every literal a walked caller passed
	// (advanced §7.2 value sets); CallerEvents are the triggers of the
	// walked callers (the caller's github.event under workflow_call).
	CallerLiterals map[string][]string
	CallerEvents   []string
	// GitHubEventOpen is the shapes.github_event_open config.
	GitHubEventOpen bool
	// ReadFile reads a repo-relative path for cat/yq/jq inputs (§6.2.8);
	// nil when the file is not local. ok is false when absent.
	ReadFile func(path string) (data []byte, ok bool)
}

// shapeEnv evaluates expressions to shapes within one file.
type shapeEnv struct {
	f      *workflow.File
	scopes map[string]*Scope
	in     ShapeInputs
	// problems collects findings; chains collects each failing chain once.
	problems []finding.Finding
	seen     map[string]bool
	depth    int
	// Registries are pure per context and memoised (medium review D5).
	memoInputs *shape.Shape
	memoNeeds  *shape.Shape
	memoSteps  map[string]*shape.Shape
	memoMatrix map[string]*shape.Shape
	memoEnv    map[shapeCtx]*shape.Shape
	// envBuilding marks a step env block under construction: a nested
	// env.X inside it resolves from the enclosing registry, which is also
	// the runtime semantics of `K: ${{ env.K }}`.
	envBuilding map[shapeCtx]*shape.Shape
	// why switches provenance recording on (spec-shape §7); mismatch keeps
	// the first failing chain step for the shape command.
	why      bool
	mismatch *Mismatch
}

// Mismatch is one failing chain step (spec-shape §4.2, exit 1).
type Mismatch struct {
	Chain string
	// At is the path the failing segment was applied to: the segments
	// before it, or the chain root when it is the first.
	At string
	// Msg is the access error; Message is the finding text check reports.
	Msg     string
	Message string
	// Shape is the shape the failing segment was applied to.
	Shape *shape.Shape
}

// wrap and then record provenance only when asked to explain.
func (e *shapeEnv) wrap(s *shape.Shape, p shape.Prov) *shape.Shape {
	if !e.why {
		return s
	}
	return shape.Wrap(s, p)
}

func (e *shapeEnv) then(s *shape.Shape, p shape.Prov) *shape.Shape {
	if !e.why {
		return s
	}
	p.Transform = true
	return shape.Then(s, p)
}

// giveUp is Any carrying s's provenance plus the reason.
func (e *shapeEnv) giveUp(s *shape.Shape, reason string) *shape.Shape {
	if !e.why {
		return shape.AnyShape()
	}
	out := shape.Opaque(reason)
	if s != nil {
		out.Prov = append(append([]shape.Prov{}, s.Prov...), out.Prov...)
	}
	return out
}

func (e *shapeEnv) opaque(reason string) *shape.Shape {
	if !e.why {
		return shape.AnyShape()
	}
	return shape.Opaque(reason)
}

// ShapeResult is what EvalShape reports (spec-shape §4).
type ShapeResult struct {
	// Expr is the expression as parsed, with canonical function casing.
	Expr  string
	Shape *shape.Shape
	// Chains lists each rooted chain in the expression with its shape and
	// provenance (one per operand of a boolean expression).
	Chains []ChainShape
	// Mismatch is set when a chain failed (exit 1); Problems are the other
	// findings evaluation raised (fromjson-invalid-literal, ...).
	Mismatch *Mismatch
	Problems []finding.Finding
}

// ChainShape is one chain's shape.
type ChainShape struct {
	Text  string
	Shape *shape.Shape
}

// EvalShape evaluates one expression at (job, step) with provenance
// (spec-shape §3, §4). step -1 is "after the last step". The text may
// carry the ${{ }} wrapper.
func EvalShape(f *workflow.File, scopes map[string]*Scope, in ShapeInputs, job string, step int, text string) (ShapeResult, error) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "${{") && strings.HasSuffix(t, "}}") {
		t = strings.TrimSpace(t[3 : len(t)-2])
	}
	node, err := expr.Parse(t)
	if err != nil {
		return ShapeResult{}, err
	}
	e := &shapeEnv{f: f, scopes: scopes, in: in, seen: map[string]bool{}, why: true}
	ctx := shapeCtx{job: job, step: step}
	pos := workflow.Pos{Line: 1, Column: 1}
	res := ShapeResult{Expr: canonicalExpr(t)}
	res.Shape = e.evalAt(node, ctx, pos)
	for _, c := range expr.Chains(node) {
		res.Chains = append(res.Chains, ChainShape{Text: innermost(c), Shape: e.chain(c, ctx, workflow.Pos{})})
	}
	res.Mismatch = e.mismatch
	res.Problems = e.problems
	return res, nil
}

// innermost is the rooted chain a wrapped chain starts from
// (fromJSON(x).a → x), the header of its why block.
func innermost(c expr.Chain) string {
	for c.Inner != nil {
		c = *c.Inner
	}
	c.Wrapped = ""
	return c.String()
}

// canonicalExpr normalises spacing and function casing (spec-shape §4.2):
// the parsed expression rendered back, or a casing-only pass when the text
// does not parse.
func canonicalExpr(t string) string {
	if n, err := expr.Parse(strings.TrimSpace(t)); err == nil {
		return expr.Format(n)
	}
	for _, fn := range []string{"fromJSON", "toJSON", "hashFiles", "startsWith", "endsWith"} {
		lower := strings.ToLower(fn)
		i := 0
		for {
			j := strings.Index(strings.ToLower(t[i:]), lower+"(")
			if j < 0 {
				break
			}
			t = t[:i+j] + fn + t[i+j+len(fn):]
			i += j + len(fn)
		}
	}
	return t
}

// ctx is where an expression sits.
type shapeCtx struct {
	job  string
	step int
}

// CheckShapes implements medium §6.3: every chain in every expression is
// checked against the registry its root selects. scopes are the per-job
// scopes already built for families 1–2 (bash analyses are reused). jq
// filters in run scripts are parsed here too (jq-syntax), and literal file
// reads that miss are shape-file-missing.
func CheckShapes(f *workflow.File, scopes map[string]*Scope, in ShapeInputs) []finding.Finding {
	e := &shapeEnv{f: f, scopes: scopes, in: in, seen: map[string]bool{}}
	for _, x := range f.Exprs {
		node, err := expr.Parse(x.Text)
		if err != nil {
			if fd, ok := stringEscapeFinding(x, err); ok && x.Pos.Line > 0 {
				e.problems = append(e.problems, fd)
			}
			continue
		}
		e.checkExpression(node, x, shapeCtx{job: x.Job, step: x.Step})
		e.checkConditionTypes(node, x, shapeCtx{job: x.Job, step: x.Step})
		e.check(node, x, shapeCtx{job: x.Job, step: x.Step})
	}
	for _, sc := range scopes {
		for _, st := range sc.Steps {
			a := sc.Bash[st.Index]
			if a == nil || a.ParseErr != nil {
				continue
			}
			for _, call := range a.JQ {
				if _, err := jq.Parse(call.Filter); err != nil {
					e.report("jq-syntax", scriptPos(st, call.Line), fmt.Sprintf("step %s: jq filter %q does not parse: %v", StepLabel(st), truncate(call.Filter), err))
				}
			}
			ts := a.Target(bash.Output)
			for _, w := range ts.Writes {
				e.checkFiles(w.Source, st, w.Line)
			}
			for _, v := range a.Vars {
				e.checkFiles(v, st, 0)
			}
		}
	}
	return e.problems
}

// scriptPos anchors script findings at the run: key (spec-rules §5.2).
func scriptPos(st workflow.Step, _ int) workflow.Pos {
	return st.RunPos
}

// checkFiles reports literal .json/.yml reads that do not exist.
func (e *shapeEnv) checkFiles(src *bash.ValueSource, st workflow.Step, line int) {
	if src == nil || src.Kind != bash.ValueCommand || e.in.ReadFile == nil {
		return
	}
	for c := src.Cmd; c != nil; c = c.Input {
		for _, p := range filesRead(c) {
			if !isDataFile(p) {
				continue
			}
			if _, ok := e.in.ReadFile(p); !ok {
				l := line
				if l == 0 {
					l = c.Line
				}
				e.report("shape-file-missing", scriptPos(st, l), fmt.Sprintf("step %s reads %s, which does not exist in the repository (it may be generated at runtime)", StepLabel(st), p))
			}
		}
	}
}

func isDataFile(p string) bool {
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".json") || strings.HasSuffix(l, ".yml") || strings.HasSuffix(l, ".yaml")
}

// filesRead lists the literal file paths a command reads.
func filesRead(c *bash.CommandInfo) []string {
	var out []string
	if c.InFile != "" {
		out = append(out, c.InFile)
	}
	switch c.Name {
	case "cat":
		for _, a := range c.Args {
			if a.Literal && !strings.HasPrefix(a.Text, "-") {
				out = append(out, a.Text)
			}
		}
	case "jq", "yq":
		positional := 0
		skip := 0
		for i, a := range c.Args {
			if skip > 0 {
				skip--
				continue
			}
			if strings.HasPrefix(a.Text, "-") {
				if a.Text == "--arg" || a.Text == "--argjson" {
					skip = 2
				}
				continue
			}
			if c.Name == "yq" && i == 0 && (a.Text == "e" || a.Text == "eval" || a.Text == "ea" || a.Text == "eval-all") {
				continue // yq v4 subcommand, not the filter
			}
			positional++
			if positional >= 2 && a.Literal {
				out = append(out, a.Text)
			}
		}
	}
	return out
}

// sourceShape resolves a write's value source to a shape (§6.2.8).
func (e *shapeEnv) sourceShape(src *bash.ValueSource, st workflow.Step, a *bash.Analysis, depth int) *shape.Shape {
	if src == nil || depth > 6 {
		return shape.StringShape()
	}
	switch src.Kind {
	case bash.ValueLiteral:
		return shape.FromLiteral(src.Text)
	case bash.ValueVar:
		for _, env := range st.Env {
			if env.Name == src.Text {
				if env.Value.IsExpr() {
					return e.wrap(shape.StringShape(), shape.Prov{Kind: "env_block", Name: src.Text, Text: "step env." + src.Text + " is an expression", Line: env.Value.Pos.Line})
				}
				return e.wrap(shape.FromLiteral(env.Value.Value), shape.Prov{Kind: "env_block", Name: src.Text, Text: "step env." + src.Text + ": " + truncate(env.Value.Value), Line: env.Value.Pos.Line})
			}
		}
		if v, ok := a.Vars[src.Text]; ok {
			return e.wrap(e.sourceShape(v, st, a, depth+1), shape.Prov{Kind: "bash_var", Name: src.Text, Text: sourceText(v), Line: sourceFileLine(st, v)})
		}
		return e.wrap(shape.StringShape(), shape.Prov{Kind: "opaque", Name: src.Text, Text: "$" + src.Text + " is not assigned in the script → String"})
	case bash.ValueCommand:
		return e.commandShape(src.Cmd, st, a, depth+1)
	}
	return shape.StringShape()
}

// sourceText renders a value source for provenance.
func sourceText(v *bash.ValueSource) string {
	if v == nil {
		return ""
	}
	switch v.Kind {
	case bash.ValueLiteral:
		return truncate(v.Text)
	case bash.ValueVar:
		return "$" + v.Text
	case bash.ValueCommand:
		return "$(" + commandText(v.Cmd) + ")"
	}
	return v.Text
}

func commandText(c *bash.CommandInfo) string {
	if c == nil {
		return ""
	}
	parts := []string{c.Name}
	for _, a := range c.Args {
		parts = append(parts, a.Text)
	}
	s := strings.Join(parts, " ")
	if c.InFile != "" {
		s += " < " + c.InFile
	}
	if c.Input != nil {
		s = commandText(c.Input) + " | " + s
	}
	return truncateTo(s, 70)
}

func truncateTo(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

// sourceFileLine is the file line of a command-valued source.
func sourceFileLine(st workflow.Step, v *bash.ValueSource) int {
	if v != nil && v.Cmd != nil && v.Cmd.Line > 0 {
		return absScriptLine(st, v.Cmd.Line)
	}
	return 0
}

// commandShape is the shape of a command's stdout as a string value.
func (e *shapeEnv) commandShape(c *bash.CommandInfo, st workflow.Step, a *bash.Analysis, depth int) *shape.Shape {
	if c == nil || depth > 6 {
		return shape.StringShape()
	}
	c = resolveArgs(c, st, a)
	switch c.Name {
	case "cat":
		files := filesRead(c)
		if len(files) == 1 {
			return e.fileShape(files[0])
		}
		if c.Input != nil {
			return e.commandShape(c.Input, st, a, depth+1)
		}
		return e.opaqueString("cat with no single literal file")
	case "echo", "printf":
		if len(c.Args) == 1 && c.Args[0].Literal {
			return shape.FromLiteral(c.Args[0].Text)
		}
		if len(c.Args) == 1 {
			v := strings.TrimPrefix(c.Args[0].Text, "$")
			if src, ok := a.Vars[v]; ok {
				return e.wrap(e.sourceShape(src, st, a, depth+1), shape.Prov{Kind: "bash_var", Name: v, Text: sourceText(src), Line: sourceFileLine(st, src)})
			}
		}
		return e.opaqueString(c.Name + " of a dynamic value")
	case "yq":
		files := filesRead(c)
		if len(files) == 1 {
			return e.yqShape(c, files[0])
		}
		return e.opaqueString("yq with no single literal file")
	case "jq":
		return e.jqShape(c, st, a, depth)
	}
	if c.InFile != "" {
		return e.fileShape(c.InFile)
	}
	return e.opaqueString("output of command " + c.Name)
}

// opaqueString is a String whose content is unknown, with the reason.
func (e *shapeEnv) opaqueString(reason string) *shape.Shape {
	return e.wrap(shape.StringShape(), shape.Prov{Kind: "opaque", Text: reason + " → String"})
}

// yqFilter is the literal filter of a yq command ("" when absent).
func yqFilter(c *bash.CommandInfo) string {
	positional := 0
	for i, a := range c.Args {
		if strings.HasPrefix(a.Text, "-") {
			continue
		}
		if i == 0 && (a.Text == "e" || a.Text == "eval" || a.Text == "ea" || a.Text == "eval-all") {
			continue
		}
		positional++
		if positional == 1 && a.Literal {
			return a.Text
		}
	}
	return ""
}

// yqShape reads the file and applies a simple path filter (.a.b[0]);
// any other filter leaves the file's shape and says so.
func (e *shapeEnv) yqShape(c *bash.CommandInfo, file string) *shape.Shape {
	in := e.fileShape(file)
	filter := yqFilter(c)
	if filter == "" || filter == "." {
		return in
	}
	segs, ok := simplePath(filter)
	if !ok {
		return e.wrap(e.opaqueString("yq filter "+truncate(filter)+" is not a simple path"), shape.Prov{Kind: "builtin", Text: "yq '" + truncate(filter) + "' over file " + file})
	}
	cur := unwrapJSON(in)
	for _, seg := range segs {
		next, err := shape.Access(cur, seg)
		if err != nil {
			return e.wrap(e.opaqueString("yq path "+filter+" does not fit the file: "+err.Msg), shape.Prov{Kind: "builtin", Text: "yq '" + filter + "' over file " + file})
		}
		cur = next
	}
	out := shape.WithSource(shape.StringOfJSON(cur), "yq '"+filter+"' over file "+file)
	if e.why {
		out.Prov = append(append([]shape.Prov{}, in.Prov...), shape.Prov{Kind: "builtin", Text: fmt.Sprintf("yq '%s': %s → %s", filter, shape.Render(unwrapJSON(in), 2), shape.Render(cur, 2))})
	}
	return out
}

// simplePath parses .a.b[0].c into access segments.
func simplePath(filter string) ([]shape.Seg, bool) {
	var segs []shape.Seg
	s := filter
	for s != "" {
		switch {
		case strings.HasPrefix(s, "."):
			s = s[1:]
			i := 0
			for i < len(s) && (s[i] == '_' || s[i] == '-' || (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= '0' && s[i] <= '9')) {
				i++
			}
			if i == 0 {
				return nil, false
			}
			segs = append(segs, shape.Seg{Field: s[:i]})
			s = s[i:]
		case strings.HasPrefix(s, "["):
			j := strings.IndexByte(s, ']')
			if j < 0 {
				return nil, false
			}
			n := 0
			if _, err := fmt.Sscanf(s[1:j], "%d", &n); err != nil {
				return nil, false
			}
			segs = append(segs, shape.Seg{IsIndex: true, Index: n})
			s = s[j+1:]
		default:
			return nil, false
		}
	}
	return segs, len(segs) > 0
}

func (e *shapeEnv) fileShape(p string) *shape.Shape {
	if e.in.ReadFile == nil {
		return e.opaqueString("file " + p + " is not readable here (fetched file)")
	}
	data, ok := e.in.ReadFile(p)
	if !ok {
		return e.opaqueString("file " + p + " not found in the repository")
	}
	var s *shape.Shape
	var err error
	format := "yaml"
	lower := strings.ToLower(p)
	switch {
	case strings.HasSuffix(lower, ".json"):
		format = "json"
		s, err = shape.FromJSON(data)
	case plainTextFile(lower):
		format = "text"
	default:
		s, err = shape.FromYAML(data)
		if err == nil && (s == nil || shape.IsPrimitive(s)) {
			// `a = 1`, `A=1`, a line of CSV: a YAML scalar is just text
			// (advanced §7.4, review 1 N5).
			format, s = "text", nil
		}
	}
	if err != nil {
		return e.opaqueString("file " + p + " does not parse as " + format)
	}
	if format == "text" {
		out := shape.WithSource(shape.StringShape(), "file "+p)
		return e.wrap(out, shape.Prov{Kind: "file_read", Path: p, Format: format, Shape: shape.StringShape()})
	}
	out := shape.WithSource(shape.StringOfJSON(s), "file "+p)
	return e.wrap(out, shape.Prov{Kind: "file_read", Path: p, Format: format, Shape: s})
}

// plainTextFile reports extensions that are never structured data.
func plainTextFile(lower string) bool {
	for _, ext := range []string{".toml", ".env", ".properties", ".csv", ".txt", ".ini", ".cfg", ".md"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return strings.HasSuffix(lower, "/.env") || lower == ".env"
}

// jqShape applies a jq filter to its input shape (§6.2.10).
func (e *shapeEnv) jqShape(c *bash.CommandInfo, st workflow.Step, a *bash.Analysis, depth int) *shape.Shape {
	filter, ok := bashJQFilter(c)
	if !ok {
		return e.opaqueString("jq filter is not a literal")
	}
	node, err := jq.Parse(filter)
	if err != nil {
		return e.opaqueString("jq filter does not parse")
	}
	opts := jq.Options{Vars: map[string]*shape.Shape{}}
	var flags []string
	var input *shape.Shape
	var inputStr *shape.Shape // the input as a string value, for provenance
	files := filesRead(c)
	skip := 0
	for i, arg := range c.Args {
		if skip > 0 {
			skip--
			continue
		}
		if strings.HasPrefix(arg.Text, "-") && arg.Text != "--arg" && arg.Text != "--argjson" {
			flags = append(flags, arg.Text)
		}
		switch arg.Text {
		case "-r", "--raw-output":
			opts.Raw = true
		case "-n", "--null-input":
			opts.NullInput = true
		case "-s", "--slurp":
			opts.Slurp = true
		case "--arg":
			if i+1 < len(c.Args) {
				opts.Vars[c.Args[i+1].Text] = shape.StringShape()
			}
			skip = 2
		case "--argjson":
			if i+2 < len(c.Args) {
				v := shape.AnyShape()
				if c.Args[i+2].Literal {
					if s, err := shape.FromJSON([]byte(c.Args[i+2].Text)); err == nil {
						v = s
					}
				}
				opts.Vars[c.Args[i+1].Text] = v
			}
			skip = 2
		}
	}
	switch {
	case len(files) >= 1:
		inputStr = e.fileShape(files[0])
	case c.Heredoc != "":
		inputStr = shape.FromLiteral(c.Heredoc)
	case c.Input != nil:
		inputStr = e.commandShape(c.Input, st, a, depth+1)
	default:
		inputStr = e.opaqueString("jq with no input")
	}
	input = unwrapJSON(inputStr)
	out := jq.Apply(node, input, opts)
	entry := shape.Prov{Kind: "jq", Filter: filter, Flags: flags, Input: input, Output: out}
	var res *shape.Shape
	switch {
	case out == nil || out.Kind == shape.Any:
		res = shape.StringShape()
		entry.Text = "result unknown → String"
	case out.Kind == shape.String:
		res = out
	default:
		// jq prints its result: the step receives JSON text.
		res = shape.WithSource(shape.StringOfJSON(out), "jq '"+truncate(filter)+"'")
	}
	if e.why {
		res = shape.Then(res, entry) // data flow: the input's provenance, then the filter
		res.Prov = append(append([]shape.Prov{}, inputStr.Prov...), entry)
	}
	return res
}

func unwrapJSON(s *shape.Shape) *shape.Shape {
	if s != nil && s.Kind == shape.String {
		if s.JSON != nil {
			return s.JSON
		}
		return shape.AnyShape()
	}
	return s
}

// bashJQFilter finds the literal filter of a jq command (first positional).
func bashJQFilter(c *bash.CommandInfo) (string, bool) {
	skip := 0
	for _, a := range c.Args {
		if skip > 0 {
			skip--
			continue
		}
		if strings.HasPrefix(a.Text, "-") {
			if a.Text == "--arg" || a.Text == "--argjson" {
				skip = 2
			}
			continue
		}
		return a.Text, a.Literal
	}
	return ".", true // jq with no filter runs `.` (shape review 1 D1)
}

// OutputShapes evaluates a file's own output values (workflow_call outputs
// for workflows, outputs.*.value for actions) for lift-up at callsites.
func OutputShapes(f *workflow.File, scopes map[string]*Scope, in ShapeInputs) map[string]*shape.Shape {
	e := &shapeEnv{f: f, scopes: scopes, in: in, seen: map[string]bool{}}
	out := map[string]*shape.Shape{}
	for _, o := range f.Outputs {
		out[o.Name] = e.valueShape(o.Value, shapeCtx{job: "", step: -1})
	}
	return out
}

// WithShapes evaluates a job's with: values that are single expressions, for
// caller-side typing (§5.2) and caller-supplied shapes (§6.2.4). Literal
// JSON values are returned as strings carrying their JSON shape.
func WithShapes(f *workflow.File, scopes map[string]*Scope, in ShapeInputs, job string, with []workflow.With) map[string]*shape.Shape {
	e := &shapeEnv{f: f, scopes: scopes, in: in, seen: map[string]bool{}}
	out := map[string]*shape.Shape{}
	for _, w := range with {
		if w.Value.IsExpr() {
			out[w.Name] = e.valueShape(w.Value.Value, shapeCtx{job: job, step: -1})
			continue
		}
		switch w.Value.Tag {
		case "!!int", "!!float":
			out[w.Name] = shape.NumberShape()
		case "!!bool":
			out[w.Name] = shape.BoolShape()
		case "":
		default:
			out[w.Name] = shape.FromLiteral(w.Value.Value)
		}
	}
	return out
}

// valueShape is the shape of a YAML value that may be `${{ expr }}` (one
// expression: its shape), a mix (string), or a literal.
func (e *shapeEnv) valueShape(value string, ctx shapeCtx) *shape.Shape {
	t := strings.TrimSpace(value)
	if strings.HasPrefix(t, "${{") && strings.HasSuffix(t, "}}") && strings.Count(t, "${{") == 1 {
		node, err := expr.Parse(strings.TrimSpace(t[3 : len(t)-2]))
		if err != nil {
			return shape.AnyShape()
		}
		return e.eval(node, ctx)
	}
	if strings.Contains(t, "${{") {
		return shape.StringShape()
	}
	return shape.FromLiteral(value)
}

// check evaluates a top-level expression, recording chain problems.
func (e *shapeEnv) check(n expr.Node, x workflow.Expr, ctx shapeCtx) {
	e.evalAt(n, ctx, x.Pos)
}

func (e *shapeEnv) eval(n expr.Node, ctx shapeCtx) *shape.Shape {
	return e.evalAt(n, ctx, workflow.Pos{})
}

// evalAt computes a node's shape; pos is where findings are attributed (a
// zero pos means "evaluating for lift-up", so nothing is reported).
func (e *shapeEnv) evalAt(n expr.Node, ctx shapeCtx, pos workflow.Pos) *shape.Shape {
	if e.depth > 16 {
		return shape.AnyShape()
	}
	e.depth++
	defer func() { e.depth-- }()
	switch x := n.(type) {
	case *expr.Literal:
		switch x.Kind {
		case expr.LitString:
			return shape.FromLiteral(x.Str)
		case expr.LitNumber:
			return shape.NumberShape()
		case expr.LitBool:
			return shape.BoolShape()
		}
		return shape.NullShape()
	case *expr.Unary:
		e.evalAt(x.X, ctx, pos)
		return shape.BoolShape()
	case *expr.Binary:
		a := e.evalAt(x.X, ctx, pos)
		b := e.evalAt(x.Y, ctx, pos)
		switch x.Op {
		case "&&", "||":
			return shape.Union(a, b)
		}
		return shape.BoolShape()
	case *expr.Call:
		return e.call(x, ctx, pos)
	case *expr.Ident, *expr.Member, *expr.Index, *expr.Wildcard:
		chains := expr.Chains(n)
		if len(chains) == 0 {
			return shape.AnyShape()
		}
		var result *shape.Shape
		for i, c := range chains {
			s := e.chain(c, ctx, pos)
			if i == 0 {
				result = s
			}
		}
		return result
	}
	return shape.AnyShape()
}

func (e *shapeEnv) call(x *expr.Call, ctx shapeCtx, pos workflow.Pos) *shape.Shape {
	name := strings.ToLower(x.Name)
	switch name {
	case "fromjson":
		if len(x.Args) != 1 {
			return shape.AnyShape()
		}
		if lit, ok := x.Args[0].(*expr.Literal); ok && lit.Kind == expr.LitString {
			s, err := shape.FromJSON([]byte(lit.Str))
			if err != nil {
				e.report("fromjson-invalid-literal", pos, fmt.Sprintf("fromJSON('%s') is not valid JSON: %v", truncate(lit.Str), err))
				return e.opaque("fromJSON of a literal that is not valid JSON")
			}
			return e.then(shape.WithSource(s, "fromJSON literal"), shape.Prov{Kind: "fromjson", Text: "fromJSON('…') parsed as JSON"})
		}
		arg := e.evalAt(x.Args[0], ctx, pos)
		switch {
		case arg == nil || arg.Kind == shape.Any:
			if arg != nil && len(arg.Prov) > 0 {
				return arg
			}
			return e.opaque("fromJSON of an unknown value")
		case arg.Kind == shape.String:
			if arg.JSON != nil {
				out := shape.WithSource(arg.JSON, arg.Source)
				if e.why {
					out = shape.Then(out, shape.Prov{Kind: "fromjson", Text: "fromJSON(String ⊂ JSON → X) = X", Transform: true})
					out.Prov = append(append([]shape.Prov{}, arg.Prov...), out.Prov[len(out.Prov)-1])
				}
				return out
			}
			return e.giveUp(arg, "fromJSON of a String whose content is unknown")
		case arg.Kind == shape.Object || arg.Kind == shape.Array || arg.Kind == shape.UnknownObject || arg.Kind == shape.UnknownArray:
			e.report("fromjson-non-string", pos, fmt.Sprintf("fromJSON of a value that is already %s%s", arg, from(arg)))
			return e.giveUp(arg, "fromJSON of a value that is already "+arg.String())
		}
		return e.giveUp(arg, "fromJSON of a "+arg.Kind.String())
	case "tojson":
		if len(x.Args) == 1 {
			inner := e.evalAt(x.Args[0], ctx, pos)
			if inner != nil && inner.Kind == shape.String && inner.JSON != nil {
				return e.then(inner, shape.Prov{Kind: "builtin", Text: "toJSON(String ⊂ JSON → X) → the same String"})
			}
			out := shape.StringOfJSON(inner)
			if e.why {
				out.Prov = append(append([]shape.Prov{}, inner.Prov...), shape.Prov{Kind: "builtin", Text: "toJSON(...) → String ⊂ JSON", Transform: true})
			}
			return out
		}
		return shape.StringShape()
	case "format", "join", "hashfiles":
		for _, a := range x.Args {
			e.evalAt(a, ctx, pos)
		}
		return e.then(shape.StringShape(), shape.Prov{Kind: "builtin", Text: x.Name + "(...) → String"})
	case "contains", "startswith", "endswith", "success", "failure", "cancelled", "always":
		for _, a := range x.Args {
			e.evalAt(a, ctx, pos)
		}
		return e.then(shape.BoolShape(), shape.Prov{Kind: "builtin", Text: x.Name + "(...) → Bool"})
	}
	for _, a := range x.Args {
		e.evalAt(a, ctx, pos)
	}
	return e.opaque("unknown function " + x.Name + "()")
}

// chain resolves a rooted chain, reporting the first failing step.
func (e *shapeEnv) chain(c expr.Chain, ctx shapeCtx, pos workflow.Pos) *shape.Shape {
	var cur *shape.Shape
	root := strings.ToLower(c.Root)
	switch root {
	case "fromjson", "tojson":
		// Chain continuing after a call: evaluate the call, then the segs.
		var argNode expr.Node
		if c.Inner != nil {
			if strings.HasPrefix(c.Inner.Root, "'") {
				argNode = &expr.Literal{Kind: expr.LitString, Str: strings.Trim(c.Inner.Root, "'")}
			} else {
				argNode = chainNode(*c.Inner)
			}
		}
		if argNode == nil {
			return shape.AnyShape()
		}
		cur = e.call(&expr.Call{Name: c.Root, Args: []expr.Node{argNode}, P: c.Pos}, ctx, pos)
	case "secrets", "vars":
		if len(c.Segs) == 0 {
			return shape.UnknownObj()
		}
		cur = shape.WithSource(shape.StringShape(), root+" context")
		c.Segs = c.Segs[1:]
	default:
		cur = e.root(root, ctx)
	}
	if cur == nil {
		return shape.AnyShape()
	}
	chainText := c.String()
	mapped := false // after .*, segments apply to the elements (medium review D3)
	prefix := ""    // the segment path before the current one
	for i, seg := range c.Segs {
		container := prefix
		if container == "" {
			container = rootText(c)
		}
		prefix += segText(seg)
		if seg.Dynamic {
			return e.giveUp(cur, "dynamic index")
		}
		if mapped && !seg.Wildcard {
			elem, err := shape.Access(cur.Elem, shape.Seg{Field: seg.Name, IsIndex: seg.IsIndex, Index: seg.Index})
			if err != nil {
				msg := fmt.Sprintf("%s: at step %d (%s): %s on the elements of the array%s", chainText, i+1, segText(seg), err.Msg, from(cur.Elem))
				e.fail(chainText, container, err.Msg+" on the elements of the array", msg, cur, pos)
				return e.failed(cur)
			}
			next := shape.ArrayOf(elem)
			cur = e.access(cur, next, segText(seg)+" (over the elements)")
			continue
		}
		next, err := shape.Access(cur, shape.Seg{Field: seg.Name, IsIndex: seg.IsIndex, Index: seg.Index, Wildcard: seg.Wildcard})
		if seg.Wildcard && err == nil && next != nil && next.Kind == shape.Array {
			mapped = true
		}
		if err != nil {
			msg := fmt.Sprintf("%s: at step %d (%s): %s%s", chainText, i+1, segText(seg), err.Msg, from(cur))
			e.fail(chainText, container, err.Msg, msg, cur, pos)
			return e.failed(cur)
		}
		if e.why && next != nil && next.Kind == shape.Any && !seg.Wildcard && cur.Kind != shape.Any {
			reason := "unknown"
			switch cur.Kind {
			case shape.Object:
				reason = fmt.Sprintf("'%s' is not among the known fields of an open object", seg.Name)
			case shape.UnknownObject, shape.UnknownArray:
				reason = "member of a container whose schema is unknown"
			}
			cur = e.giveUp(cur, segText(seg)+": "+reason)
			continue
		}
		cur = e.access(cur, next, segText(seg))
	}
	return cur
}

// failed is Any after a mismatch, keeping the provenance up to it.
func (e *shapeEnv) failed(cur *shape.Shape) *shape.Shape {
	if !e.why || cur == nil {
		return shape.AnyShape()
	}
	return &shape.Shape{Kind: shape.Any, Prov: cur.Prov}
}

// fail records the first mismatch and reports it as a finding.
func (e *shapeEnv) fail(chainText, at, errMsg, msg string, cur *shape.Shape, pos workflow.Pos) {
	if e.mismatch == nil {
		e.mismatch = &Mismatch{Chain: chainText, At: at, Msg: errMsg, Message: msg, Shape: cur}
	}
	key := fmt.Sprintf("%d:%d:%s", pos.Line, pos.Column, chainText)
	if pos.Line > 0 && !e.seen[key] {
		e.seen[key] = true
		e.report("shape-mismatch", pos, msg)
	}
}

// access records one chain step in the provenance. Navigating a registry
// (steps → id → outputs) is silent; a field that carries its own sources
// (a step write, a declaration) starts the chain's provenance; accesses
// after that (past a fromJSON, into a value) are recorded.
func (e *shapeEnv) access(cur, next *shape.Shape, seg string) *shape.Shape {
	if !e.why {
		return next
	}
	switch {
	case next != nil && len(next.Prov) > 0:
		return next
	case cur == nil || len(cur.Prov) == 0:
		return next
	}
	out := shape.Then(next, shape.Prov{Kind: "access", Text: seg, Shape: next, Transform: true})
	out.Prov = append(append([]shape.Prov{}, cur.Prov...), out.Prov[len(out.Prov)-1])
	return out
}

// resolveArgs returns a copy of the command whose $VAR arguments are
// replaced by the step env or script literal they name, so a file passed
// through a variable (yq "$DATA_FILE") is still a literal read.
func resolveArgs(c *bash.CommandInfo, st workflow.Step, a *bash.Analysis) *bash.CommandInfo {
	if c == nil {
		return nil
	}
	lookup := func(name string) (string, bool) {
		for _, env := range st.Env {
			if env.Name == name && !env.Value.IsExpr() {
				return env.Value.Value, true
			}
		}
		if v, ok := a.Vars[name]; ok && v.Kind == bash.ValueLiteral {
			return v.Text, true
		}
		return "", false
	}
	out := *c
	out.Args = append([]bash.CommandArg{}, c.Args...)
	for i, arg := range out.Args {
		if arg.Literal {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(arg.Text, "$"), "{"), "}")
		if name == "" || strings.ContainsAny(name, "$ /") {
			continue
		}
		if v, ok := lookup(name); ok {
			out.Args[i] = bash.CommandArg{Text: v, Literal: true}
		}
	}
	return &out
}

func rootText(c expr.Chain) string {
	if c.Inner != nil {
		return c.Root + "(" + c.Inner.String() + ")"
	}
	return c.Root
}

func chainNode(c expr.Chain) expr.Node {
	var n expr.Node = &expr.Ident{Name: c.Root, P: c.Pos}
	if strings.EqualFold(c.Root, "fromjson") || strings.EqualFold(c.Root, "tojson") {
		var arg expr.Node
		if c.Inner != nil {
			if strings.HasPrefix(c.Inner.Root, "'") {
				arg = &expr.Literal{Kind: expr.LitString, Str: strings.Trim(c.Inner.Root, "'")}
			} else {
				arg = chainNode(*c.Inner)
			}
		}
		if arg == nil {
			arg = &expr.Literal{Kind: expr.LitNull}
		}
		n = &expr.Call{Name: c.Root, Args: []expr.Node{arg}, P: c.Pos}
	}
	for _, s := range c.Segs {
		switch {
		case s.Wildcard:
			n = &expr.Wildcard{X: n, P: c.Pos}
		case s.Dynamic:
			n = &expr.Index{X: n, Idx: &expr.Ident{Name: "dynamic"}, P: c.Pos}
		case s.IsIndex:
			n = &expr.Index{X: n, Idx: &expr.Literal{Kind: expr.LitNumber, Num: float64(s.Index)}, P: c.Pos}
		default:
			n = &expr.Member{X: n, Name: s.Name, P: c.Pos}
		}
	}
	return n
}

func segText(s expr.Seg) string {
	switch {
	case s.Wildcard:
		return ".*"
	case s.IsIndex:
		return fmt.Sprintf("[%d]", s.Index)
	}
	return "." + s.Name
}

func from(s *shape.Shape) string {
	if s == nil || s.Source == "" {
		return ""
	}
	return " (from " + s.Source + ")"
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:37] + "..."
	}
	return s
}

func (e *shapeEnv) report(rule string, pos workflow.Pos, msg string) {
	if pos.Line == 0 {
		return
	}
	e.problems = append(e.problems, at(rule, pos, msg))
}

// root selects the registry for a chain root (§6.3).
func (e *shapeEnv) root(name string, ctx shapeCtx) *shape.Shape {
	switch name {
	case "inputs":
		if e.memoInputs == nil {
			e.memoInputs = e.inputs()
		}
		return e.memoInputs
	case "steps":
		if e.memoSteps == nil {
			e.memoSteps = map[string]*shape.Shape{}
		}
		if s, ok := e.memoSteps[ctx.job]; ok {
			return s
		}
		s := e.steps(ctx)
		e.memoSteps[ctx.job] = s
		return s
	case "needs", "jobs":
		if e.memoNeeds == nil {
			e.memoNeeds = e.needs()
		}
		return e.memoNeeds
	case "env":
		return e.envMemo(ctx)
	case "matrix":
		if e.memoMatrix == nil {
			e.memoMatrix = map[string]*shape.Shape{}
		}
		if s, ok := e.memoMatrix[ctx.job]; ok {
			return s
		}
		s := e.matrix(ctx)
		e.memoMatrix[ctx.job] = s
		return s
	case "github":
		open := "closed"
		if e.in.GitHubEventOpen {
			open = "open"
		}
		events := e.f.Events
		note := ""
		if callOnly(e.f) && len(e.in.CallerEvents) > 0 {
			events = dedupeSorted(e.in.CallerEvents) // the caller's event (advanced §7.2)
			note = " from the callers"
		}
		g := contextschema.GitHub(events, e.in.GitHubEventOpen)
		if e.f.HasWorkflowDispatch {
			g = withEventInputs(g, e.dispatchInputs())
		}
		return e.wrap(g, shape.Prov{Kind: "static_schema", Text: fmt.Sprintf("static schema github for on: [%s]%s (event payload %s)", strings.Join(events, ", "), note, open)})
	case "runner":
		return e.wrap(contextschema.Runner(), shape.Prov{Kind: "static_schema", Text: "static schema runner"})
	case "job":
		return e.wrap(contextschema.Job(), shape.Prov{Kind: "static_schema", Text: "static schema job"})
	case "strategy":
		return e.wrap(shape.OpenObject(map[string]*shape.Shape{"fail-fast": shape.BoolShape(), "job-index": shape.NumberShape(), "job-total": shape.NumberShape(), "max-parallel": shape.NumberShape()}), shape.Prov{Kind: "static_schema", Text: "static schema strategy"})
	}
	return e.opaque("unknown context '" + name + "'")
}

func callOnly(f *workflow.File) bool {
	if !f.HasWorkflowCall {
		return false
	}
	for _, ev := range f.Events {
		if ev != "workflow_call" {
			return false
		}
	}
	return true
}

func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// withEventInputs copies the github shape with event.inputs set to the
// workflow_dispatch inputs (advanced §7.2).
func withEventInputs(g, inputs *shape.Shape) *shape.Shape {
	ev := g.Fields["event"]
	if ev == nil || inputs == nil {
		return g
	}
	evc := *ev
	evc.Fields = copyFields(ev.Fields)
	evc.Fields["inputs"] = inputs
	gc := *g
	gc.Fields = copyFields(g.Fields)
	gc.Fields["event"] = &evc
	return &gc
}

// dispatchInputs is the inputs registry restricted to workflow_dispatch.
func (e *shapeEnv) dispatchInputs() *shape.Shape {
	all := e.root("inputs", shapeCtx{})
	fields := map[string]*shape.Shape{}
	for _, in := range e.f.Inputs {
		if in.Trigger == "workflow_dispatch" {
			if s, ok := all.Fields[in.Name]; ok {
				fields[in.Name] = s
			}
		}
	}
	return shape.OpenObject(fields)
}

// inputs: declared types, defaults, and caller-supplied JSON (§6.2.2–4).
func (e *shapeEnv) inputs() *shape.Shape {
	fields := map[string]*shape.Shape{}
	for _, in := range e.f.Inputs {
		var s *shape.Shape
		var prov []shape.Prov
		declared := fmt.Sprintf("declared on.%s.inputs.%s", in.Trigger, in.Name)
		if in.Type != "" {
			declared += ": type: " + in.Type
		}
		prov = append(prov, shape.Prov{Kind: "declared_input", Name: in.Name, Line: in.Pos.Line, Text: declared})
		switch in.Type {
		case "number":
			s = shape.NumberShape()
		case "boolean":
			s = shape.BoolShape()
		default:
			s = shape.StringShape()
			if in.Type == "choice" && len(in.Options) > 0 {
				s = shape.WithValues(s, in.Options...)
			}
			if in.HasDefault && !in.Default.IsExpr() {
				s = shape.FromLiteral(in.Default.Value)
				how := "a plain string"
				if s.JSON != nil {
					how = "parsed as JSON"
				}
				prov = append(prov, shape.Prov{Kind: "input_default", Name: in.Name, Line: in.Default.Pos.Line, Text: fmt.Sprintf("default on inputs.%s: %s (%s)", in.Name, truncate(in.Default.Value), how)})
			}
			if lits := e.in.CallerLiterals[in.Name]; len(lits) > 0 && s.Kind == shape.String && s.JSON == nil {
				s = shape.WithValues(s, dedupeSorted(lits)...)
			}
			if caller, ok := e.in.CallerInputs[in.Name]; ok && caller != nil {
				s = shape.StringOfJSON(shape.WithSource(caller, "callers' with."+in.Name))
				for _, site := range e.in.CallerSites[in.Name] {
					prov = append(prov, shape.Prov{Kind: "caller_with", Name: in.Name, Text: "supplied by caller " + site})
				}
				prov = append(prov, shape.Prov{Kind: "intersection", Name: in.Name, Shape: caller, Text: "intersection of the callers' values"})
			} else if in.Type == "" || in.Type == "string" {
				prov = append(prov, shape.Prov{Kind: "no_callers", Name: in.Name, Text: "no callers found in entry or linked repos supplying JSON"})
			}
		}
		s = shape.WithSource(s, fmt.Sprintf("input '%s'%s", in.Name, typeNote(in)))
		if prev, ok := fields[in.Name]; ok {
			// Declared under two triggers (spec-shape §8): the union, with
			// both declarations in the provenance.
			u := shape.Union(prev, s)
			s = shape.WithSource(u, fmt.Sprintf("input '%s' (declared under more than one trigger)", in.Name))
			prov = append(append([]shape.Prov{}, prev.Prov...), prov...)
		}
		if e.why {
			s = &shape.Shape{Kind: s.Kind, Fields: s.Fields, Open: s.Open, Elem: s.Elem, Source: s.Source, JSON: s.JSON, Values: s.Values, Prov: prov}
		}
		fields[in.Name] = s
	}
	return shape.OpenObject(fields) // undeclared names are input-undeclared's business
}

func typeNote(in workflow.Input) string {
	if in.Type != "" {
		return " (type " + in.Type + ")"
	}
	return ""
}

// steps: outputs of each step in the scope, from bash writes or action
// declarations (§6.2.8, §6.2.11). Output objects are open: missing keys are
// the step rules' business.
func (e *shapeEnv) steps(ctx shapeCtx) *shape.Shape {
	sc := e.scopes[ctx.job]
	if sc == nil {
		return shape.UnknownObj()
	}
	fields := map[string]*shape.Shape{}
	for _, st := range sc.Steps {
		if st.ID == "" {
			continue
		}
		outputs := map[string]*shape.Shape{}
		if a := sc.Bash[st.Index]; a != nil && a.ParseErr == nil {
			ts := a.Target(bash.Output)
			if ts.Open && len(ts.Writes) == 0 {
				outputs = nil
			}
			for k, w := range ts.Writes {
				var s *shape.Shape
				switch {
				case w.Multiline:
					s = shape.FromLiteral(strings.Join(w.Lines, "\n"))
				case w.Source != nil:
					s = e.sourceShape(w.Source, st, a, 0)
					if w.Source.Kind == bash.ValueLiteral && !w.ValueDynamic && s != nil && s.Kind == shape.String && s.JSON == nil && !strings.Contains(w.Source.Text, bash.Sentinel) {
						s = shape.WithValues(s, w.Source.Text) // every write is this literal (advanced §7.2)
					}
				case w.ValueDynamic:
					s = shape.StringShape()
				default:
					s = shape.FromLiteral(w.Value)
					if s.JSON == nil && !w.ValueDynamic {
						s = shape.WithValues(s, w.Value) // every write is this literal (advanced §7.2)
					}
				}
				line := absScriptLine(st, w.Line)
				s = shape.WithSource(s, fmt.Sprintf("step '%s' line %d writes %s", st.ID, line, k))
				if w.ValueDynamic && w.Source == nil {
					s = e.wrap(s, shape.Prov{Kind: "opaque", Text: "the value is dynamic → String"})
				}
				outputs[k] = e.wrap(s, shape.Prov{Kind: "github_output_write", Step: st.ID, Line: line, Name: k, Text: statementAt(a, w.Line)})
			}
		} else if st.Uses != "" {
			if lifted := e.in.StepCallee[ctx.job][st.Index]; lifted != nil {
				for k, s := range lifted {
					outputs[k] = e.wrap(shape.WithSource(s, fmt.Sprintf("output '%s' of %s", k, st.Uses)), shape.Prov{Kind: "lift", Name: k, Step: st.ID, Line: st.UsesPos.Line, Text: fmt.Sprintf("lifted from %s outputs.%s.value", st.Uses, k)})
				}
			} else {
				outputs = nil
			}
		}
		var outObj *shape.Shape
		if outputs == nil {
			outObj = e.wrap(shape.UnknownObj(), shape.Prov{Kind: "opaque", Text: fmt.Sprintf("outputs of step '%s' are unknown (action not loaded, non-bash shell, or unparsed script)", st.ID)})
		} else {
			outObj = shape.OpenObject(outputs)
		}
		fields[st.ID] = shape.OpenObject(map[string]*shape.Shape{
			"outputs":    outObj,
			"outcome":    e.wrap(shape.WithValues(shape.StringShape(), "success", "failure", "cancelled", "skipped"), shape.Prov{Kind: "static_schema", Text: "steps." + st.ID + ".outcome: the step's status before continue-on-error"}),
			"conclusion": e.wrap(shape.WithValues(shape.StringShape(), "success", "failure", "cancelled", "skipped"), shape.Prov{Kind: "static_schema", Text: "steps." + st.ID + ".conclusion: the step's status after continue-on-error"}),
		})
	}
	return shape.OpenObject(fields)
}

// needs: lift-up from each job's outputs or its callee (§6.2.11).
func (e *shapeEnv) needs() *shape.Shape {
	fields := map[string]*shape.Shape{}
	for _, j := range e.f.Jobs {
		fields[j.Name] = shape.OpenObject(map[string]*shape.Shape{
			"outputs": shape.OpenObject(e.jobOutputs(j)),
			"result":  e.wrap(shape.WithValues(shape.StringShape(), "success", "failure", "cancelled", "skipped"), shape.Prov{Kind: "static_schema", Text: "needs." + j.Name + ".result: the job's status"}),
		})
	}
	return shape.OpenObject(fields)
}

func (e *shapeEnv) jobs() *shape.Shape {
	return e.needs()
}

func (e *shapeEnv) jobOutputs(j workflow.Job) map[string]*shape.Shape {
	out := map[string]*shape.Shape{}
	if j.Uses != nil {
		for k, s := range e.in.JobCallee[j.Name] {
			out[k] = e.wrap(shape.WithSource(s, fmt.Sprintf("output '%s' of %s", k, j.Uses.Raw)), shape.Prov{Kind: "lift", Name: k, Line: j.Uses.Pos.Line, Text: fmt.Sprintf("lifted from %s on.workflow_call.outputs.%s", j.Uses.Raw, k)})
		}
		return out
	}
	for _, o := range j.Outputs {
		s := e.valueShape(o.Value, shapeCtx{job: j.Name, step: -1})
		out[o.Name] = e.wrap(shape.WithSource(s, fmt.Sprintf("jobs.%s.outputs.%s (line %d)", j.Name, o.Name, o.Pos.Line)), shape.Prov{Kind: "lift", Name: o.Name, Line: o.Pos.Line, Text: fmt.Sprintf("lifted from jobs.%s.outputs.%s: %s", j.Name, o.Name, truncateTo(o.Value, 60))})
	}
	return out
}

// envMemo returns the env registry for ctx, building it once. While a
// step's env: block is being built, env.X inside it resolves from the
// enclosing registry (workflow, job, earlier writes).
func (e *shapeEnv) envMemo(ctx shapeCtx) *shape.Shape {
	if e.memoEnv == nil {
		e.memoEnv = map[shapeCtx]*shape.Shape{}
		e.envBuilding = map[shapeCtx]*shape.Shape{}
	}
	if s, ok := e.memoEnv[ctx]; ok {
		return s
	}
	if enclosing, building := e.envBuilding[ctx]; building {
		return enclosing
	}
	s := e.env(ctx)
	e.memoEnv[ctx] = s
	return s
}

// env: workflow, job and step env blocks plus earlier $GITHUB_ENV writes.
func (e *shapeEnv) env(ctx shapeCtx) *shape.Shape {
	fields := map[string]*shape.Shape{}
	add := func(ws []workflow.With, where string) {
		for _, w := range ws {
			var s *shape.Shape
			if w.Value.IsExpr() {
				s = e.valueShape(w.Value.Value, ctx)
				if s != nil && s.Kind != shape.String {
					s = shape.StringOfJSON(s) // env values are strings at runtime
					if s.JSON != nil && (s.JSON.Kind == shape.Number || s.JSON.Kind == shape.Bool || s.JSON.Kind == shape.Null || s.JSON.Kind == shape.Any) {
						s = shape.StringShape()
					}
				}
			} else {
				s = shape.FromLiteral(w.Value.Value)
			}
			fields[w.Name] = e.wrap(shape.WithSource(s, where+" env."+w.Name), shape.Prov{Kind: "env_block", Name: w.Name, Line: w.Value.Pos.Line, Text: fmt.Sprintf("env.%s set at %s: %s", w.Name, where, truncate(w.Value.Value))})
		}
	}
	add(e.f.Env, "workflow")
	if ctx.job != "" {
		if j := workflow.JobByName(e.f, ctx.job); j != nil {
			add(j.Env, "job '"+ctx.job+"'")
		}
	}
	if sc := e.scopes[ctx.job]; sc != nil {
		for _, st := range sc.Steps {
			if ctx.step >= 0 && st.Index >= ctx.step {
				break
			}
			if a := sc.Bash[st.Index]; a != nil && a.ParseErr == nil {
				for k, w := range a.Target(bash.Env).Writes {
					s := shape.StringShape()
					if !w.ValueDynamic {
						s = shape.FromLiteral(w.Value)
					}
					fields[k] = e.wrap(shape.WithSource(s, fmt.Sprintf("step %s writes %s to $GITHUB_ENV", StepLabel(st), k)), shape.Prov{Kind: "github_env_write", Name: k, Step: strings.Trim(StepLabel(st), "'"), Line: absScriptLine(st, w.Line), Text: statementAt(a, w.Line)})
				}
			}
		}
		if ctx.step >= 0 && ctx.step < len(sc.Steps) {
			// The step's own env: block sees the enclosing registry.
			enclosing := shape.OpenObject(copyFields(fields))
			e.envBuilding[ctx] = enclosing
			add(sc.Steps[ctx.step].Env, "step")
			delete(e.envBuilding, ctx)
		}
	}
	return shape.OpenObject(fields)
}

func copyFields(m map[string]*shape.Shape) map[string]*shape.Shape {
	out := make(map[string]*shape.Shape, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// matrix: axis unions and include fields (§6.2.6).
func (e *shapeEnv) matrix(ctx shapeCtx) *shape.Shape {
	if ctx.job == "" {
		return shape.UnknownObj()
	}
	j := workflow.JobByName(e.f, ctx.job)
	if j == nil || j.Matrix == nil {
		return shape.UnknownObj()
	}
	m := j.Matrix
	if m.Expr != "" {
		s := e.valueShape(m.Expr, shapeCtx{job: ctx.job, step: -1})
		if s != nil && s.Kind == shape.String && s.JSON != nil {
			s = s.JSON
		}
		if s == nil || (s.Kind != shape.Object && s.Kind != shape.UnknownObject) {
			return shape.UnknownObj()
		}
		return s
	}
	fields := map[string]*shape.Shape{}
	names := make([]string, 0, len(m.Axes))
	for k := range m.Axes {
		names = append(names, k)
	}
	sort.Strings(names)
	surviving := matrixSurviving(m)
	for _, k := range names {
		var u *shape.Shape
		var vals []string
		if rows := m.Objects[k]; len(rows) > 0 {
			// A matrix of objects: the union of the rows (advanced §7.3).
			u = e.objectAxis(rows)
			fields[k] = e.wrap(shape.WithSource(u, "matrix."+k), shape.Prov{Kind: "matrix", Name: k, Line: m.Pos.Line, Text: fmt.Sprintf("strategy.matrix.%s: %d object rows", k, len(rows))})
			continue
		}
		for _, v := range m.Axes[k] {
			if surviving != nil && !surviving[k][v.Value] {
				continue // every combination with this value is excluded
			}
			u = shape.Union(u, scalarShape(v))
			vals = append(vals, v.Value)
		}
		if u != nil && u.Kind == shape.String && len(vals) > 0 && len(m.Include) == 0 && m.IncludeExpr == "" {
			u = shape.WithValues(u, vals...)
		}
		fields[k] = e.wrap(shape.WithSource(u, "matrix."+k), shape.Prov{Kind: "matrix", Name: k, Line: m.Pos.Line, Text: fmt.Sprintf("strategy.matrix.%s: [%s]", k, strings.Join(vals, ", "))})
	}
	for _, row := range m.Include {
		for k, v := range row {
			fields[k] = e.wrap(shape.WithSource(shape.Union(fields[k], scalarShape(v)), "matrix include "+k), shape.Prov{Kind: "matrix", Name: k, Line: m.Pos.Line, Text: fmt.Sprintf("strategy.matrix.include adds %s: %s", k, v.Value)})
		}
	}
	if m.IncludeExpr != "" {
		// include: from an expression adds fields the file does not name
		// (medium review D4): use the resolved rows' fields when known, and
		// leave the object open either way.
		s := e.valueShape(m.IncludeExpr, shapeCtx{job: ctx.job, step: -1})
		if s != nil && s.Kind == shape.String && s.JSON != nil {
			s = s.JSON
		}
		if s != nil && s.Kind == shape.Array && s.Elem != nil && s.Elem.Kind == shape.Object {
			for k, v := range s.Elem.Fields {
				fields[k] = shape.WithSource(shape.Union(fields[k], v), "matrix include (expression)")
			}
		}
		return shape.OpenObject(fields)
	}
	return shape.ObjectShape(fields)
}

// absScriptLine maps a script line to the file line.
func absScriptLine(st workflow.Step, scriptLine int) int {
	if st.RunBlock {
		return st.RunPos.Line + scriptLine
	}
	return st.RunPos.Line + scriptLine - 1
}

// statementAt is the text of the statement on a script line.
func statementAt(a *bash.Analysis, line int) string {
	for _, s := range a.Statements {
		if s.Line == line {
			return truncateTo(s.Text, 80)
		}
	}
	return ""
}

// matrixSurviving lists, per axis, the values some non-excluded
// combination still carries; nil when exclude: is empty or any axis is
// non-literal.
func matrixSurviving(m *workflow.MatrixSpec) map[string]map[string]bool {
	if len(m.Exclude) == 0 || m.IncludeExpr != "" {
		return nil
	}
	var names []string
	for k := range m.Axes {
		if len(m.Objects[k]) > 0 {
			return nil
		}
		names = append(names, k)
	}
	sort.Strings(names)
	out := map[string]map[string]bool{}
	for _, k := range names {
		out[k] = map[string]bool{}
	}
	idx := make([]int, len(names))
	total := 1
	for _, k := range names {
		total *= len(m.Axes[k])
	}
	if total == 0 || total > 4096 {
		return nil
	}
	for {
		row := map[string]string{}
		for i, k := range names {
			row[k] = m.Axes[k][idx[i]].Value
		}
		if !excluded(row, m.Exclude) {
			for k, v := range row {
				out[k][v] = true
			}
		}
		i := 0
		for i < len(idx) {
			idx[i]++
			if idx[i] < len(m.Axes[names[i]]) {
				break
			}
			idx[i] = 0
			i++
		}
		if i == len(idx) {
			break
		}
	}
	return out
}

func excluded(row map[string]string, excludes []map[string]workflow.Scalar) bool {
	for _, ex := range excludes {
		match := len(ex) > 0
		for k, v := range ex {
			if row[k] != v.Value {
				match = false
			}
		}
		if match {
			return true
		}
	}
	return false
}

// objectAxis is the union shape of a matrix-of-objects axis with a value
// set per field whose rows are all literals.
func (e *shapeEnv) objectAxis(rows []map[string]workflow.Scalar) *shape.Shape {
	var u *shape.Shape
	values := map[string][]string{}
	for _, row := range rows {
		fields := map[string]*shape.Shape{}
		for k, v := range row {
			fields[k] = scalarShape(v)
			values[k] = append(values[k], v.Value)
		}
		u = shape.Union(u, shape.ObjectShape(fields))
	}
	if u != nil && u.Kind == shape.Object {
		for k, f := range u.Fields {
			if f.Kind == shape.String && len(values[k]) == len(rows) {
				u.Fields[k] = shape.WithValues(f, dedupeSorted(values[k])...)
			}
		}
	}
	return u
}

// CheckMatrix is matrix-empty, matrix-exclude-no-match and
// matrix-strategy-type (advanced §7.3).
func CheckMatrix(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	for _, j := range f.Jobs {
		if j.Strategy != nil {
			if mp := j.Strategy.MaxParallel; mp != nil && !mp.IsExpr() && mp.Tag != "!!int" {
				out = append(out, at("matrix-strategy-type", mp.Pos, fmt.Sprintf("job '%s' strategy.max-parallel is %q, not a number", j.Name, mp.Value)))
			}
			if ff := j.Strategy.FailFast; ff != nil && !ff.IsExpr() && ff.Tag != "!!bool" {
				out = append(out, at("matrix-strategy-type", ff.Pos, fmt.Sprintf("job '%s' strategy.fail-fast is %q, not true or false", j.Name, ff.Value)))
			}
		}
		m := j.Matrix
		if m == nil {
			continue
		}
		for _, axis := range m.Empty {
			out = append(out, at("matrix-empty", m.Pos, fmt.Sprintf("job '%s' matrix axis %q is an empty list; the job never runs", j.Name, axis)))
		}
		if len(m.Exclude) > 0 && m.IncludeExpr == "" {
			for i, ex := range m.Exclude {
				matches := false
				for _, row := range matrixRows(m) {
					all := true
					for k, v := range ex {
						if row[k] != v.Value {
							all = false
						}
					}
					if all {
						matches = true
					}
				}
				if !matches {
					var parts []string
					for k, v := range ex {
						parts = append(parts, k+": "+v.Value)
					}
					sort.Strings(parts)
					out = append(out, at("matrix-exclude-no-match", m.Pos, fmt.Sprintf("job '%s' matrix exclude #%d {%s} matches no combination", j.Name, i+1, strings.Join(parts, ", "))))
				}
			}
		}
	}
	return out
}

// matrixRows enumerates literal axis combinations (plus include rows).
func matrixRows(m *workflow.MatrixSpec) []map[string]string {
	var names []string
	for k := range m.Axes {
		names = append(names, k)
	}
	sort.Strings(names)
	rows := []map[string]string{{}}
	for _, k := range names {
		var next []map[string]string
		for _, r := range rows {
			for _, v := range m.Axes[k] {
				c := map[string]string{}
				for kk, vv := range r {
					c[kk] = vv
				}
				c[k] = v.Value
				next = append(next, c)
			}
		}
		rows = next
		if len(rows) > 4096 {
			return rows
		}
	}
	for _, inc := range m.Include {
		r := map[string]string{}
		for k, v := range inc {
			r[k] = v.Value
		}
		rows = append(rows, r)
	}
	return rows
}

func scalarShape(v workflow.Scalar) *shape.Shape {
	switch v.Tag {
	case "!!int", "!!float":
		return shape.NumberShape()
	case "!!bool":
		return shape.BoolShape()
	case "!!null":
		return shape.NullShape()
	case "":
		return shape.AnyShape()
	}
	if v.IsExpr() {
		return shape.AnyShape()
	}
	return shape.FromLiteral(v.Value)
}
