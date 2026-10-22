package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/strutil"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family expressions (advanced §6): the functions GitHub actually has,
// their arities, format placeholders, quoting, comparison types, and
// which contexts are available where.

// exprFunctions maps the lower-cased name to its canonical spelling and
// the allowed argument counts (min, max; -1 = unbounded).
var exprFunctions = map[string]struct {
	name     string
	min, max int
}{
	"contains":   {"contains", 2, 2},
	"startswith": {"startsWith", 2, 2},
	"endswith":   {"endsWith", 2, 2},
	"format":     {"format", 1, -1},
	"join":       {"join", 1, 2},
	"tojson":     {"toJSON", 1, 1},
	"fromjson":   {"fromJSON", 1, 1},
	"hashfiles":  {"hashFiles", 1, -1},
	"success":    {"success", 0, 0},
	"always":     {"always", 0, 0},
	"cancelled":  {"cancelled", 0, 0},
	"failure":    {"failure", 0, 0},
}

var exprFunctionNames = func() []string {
	var out []string
	for _, f := range exprFunctions {
		out = append(out, f.name)
	}
	return out
}()

// contextUnavailable lists, per context root, the locations where the
// context is empty (advanced §6 expr-context-unavailable). A location is
// matched by loc(): "job-if", "runs-on", "job-env", "job-concurrency",
// "workflow" (any top-level key outside jobs), "on", "concurrency",
// "run-name", "permissions", "if" (any if).
var contextUnavailable = map[string][]string{
	"steps":   {"job-if", "runs-on", "job-env", "job-concurrency", "workflow"},
	"env":     {"job-if", "runs-on", "on", "concurrency"},
	"secrets": {"if", "runs-on", "on", "concurrency"},
	"matrix":  {"workflow"},
	"runner":  {"job-if", "runs-on", "job-env", "workflow"},
	"needs":   {"workflow"},
	"inputs":  {"on"}, // concurrency: does see inputs (medium review 6 D12)
	"job":     {"workflow", "runs-on", "job-if"},
}

// loc classifies where an expression sits for the availability table.
func loc(x workflow.Expr) []string {
	var out []string
	jobLevel := x.Job != "" && x.Step < 0
	switch {
	case x.Section == "on":
		out = append(out, "on", "workflow")
	case x.Job == "" && x.Section != "runs" && x.Section != "jobs":
		out = append(out, "workflow")
		if x.Section == "concurrency" {
			out = append(out, "concurrency")
		}
		if x.Section == "run-name" {
			out = append(out, "run-name")
		}
		if x.Section == "permissions" {
			out = append(out, "permissions")
		}
	}
	if x.Key == "if" {
		out = append(out, "if")
		if jobLevel && x.Parent == "<job>" {
			out = append(out, "job-if")
		}
	}
	if x.Key == "runs-on" || (x.Parent == "runs-on" && jobLevel) {
		out = append(out, "runs-on")
	}
	if jobLevel && x.Parent == "env" {
		out = append(out, "job-env")
	}
	if jobLevel && (x.Key == "concurrency" || x.Parent == "concurrency") {
		out = append(out, "job-concurrency", "concurrency")
	}
	return out
}

// checkExpression runs the expressions family over one parsed
// expression; it is called from CheckShapes so inferred shapes are at
// hand for the type rules.
func (e *shapeEnv) checkExpression(node expr.Node, x workflow.Expr, ctx shapeCtx) {
	pos := x.Pos
	var visit func(n expr.Node)
	visit = func(n expr.Node) {
		switch t := n.(type) {
		case *expr.Call:
			e.checkCall(t, x, ctx)
			for _, a := range t.Args {
				visit(a)
			}
		case *expr.Binary:
			if t.Op == "==" || t.Op == "!=" {
				e.checkComparison(t, x, ctx)
			}
			visit(t.X)
			visit(t.Y)
		case *expr.Unary:
			visit(t.X)
		case *expr.Member:
			visit(t.X)
		case *expr.Index:
			visit(t.X)
			visit(t.Idx)
		case *expr.Wildcard:
			visit(t.X)
		}
	}
	visit(node)

	// Context availability (expr-env-in-if-job, expr-secrets-in-if, and
	// the general table).
	where := loc(x)
	if e.f.Kind != workflow.KindWorkflow {
		where = dropLoc(where, "workflow") // actions have no workflow level
	}
	if x.Section == "on" && x.Key == "default" {
		// on.workflow_call.inputs.<id>.default sees github, inputs, vars.
		where = dropLoc(where, "on")
	}
	seen := map[string]bool{}
	for _, c := range expr.Chains(node) {
		root := strings.ToLower(c.Root)
		if root == "github" && len(c.Segs) >= 2 && c.Segs[0].Name == "event" && c.Segs[1].Name == "inputs" && e.f.Kind == workflow.KindWorkflow && !e.f.HasWorkflowDispatch && !seen["github.event.inputs"] {
			seen["github.event.inputs"] = true
			e.report("expr-context-unavailable", pos, "github.event.inputs only exists under workflow_dispatch; this workflow has no such trigger, so the value is empty")
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		switch {
		case root == "env" && contains(where, "job-if"):
			e.report("expr-env-in-if-job", pos, "env.* is not available in a job-level if:; the env context is empty there (it works in step-level if:)")
			continue
		case root == "secrets" && contains(where, "if"):
			e.report("expr-secrets-in-if", pos, "secrets.* is not available in an if: condition; pass the secret through env: and test the env value in a step")
			continue
		}
		for _, bad := range contextUnavailable[root] {
			if contains(where, bad) {
				e.report("expr-context-unavailable", pos, fmt.Sprintf("the %s context is not available %s; the expression is always empty here", root, locName(bad)))
				break
			}
		}
	}
}

func dropLoc(where []string, drop string) []string {
	var out []string
	for _, w := range where {
		if w != drop {
			out = append(out, w)
		}
	}
	return out
}

func locName(l string) string {
	switch l {
	case "job-if":
		return "in a job-level if:"
	case "runs-on":
		return "in runs-on:"
	case "job-env":
		return "in a job-level env:"
	case "job-concurrency", "concurrency":
		return "in concurrency:"
	case "workflow":
		return "at workflow level (outside jobs:)"
	case "on":
		return "in on:"
	case "if":
		return "in if:"
	case "run-name":
		return "in run-name:"
	case "permissions":
		return "in permissions:"
	}
	return "here"
}

// checkCall is expr-unknown-function, expr-arity, expr-format-placeholders,
// expr-contains-types and expr-hashfiles-absolute.
func (e *shapeEnv) checkCall(c *expr.Call, x workflow.Expr, ctx shapeCtx) {
	pos := x.Pos
	fn, ok := exprFunctions[strings.ToLower(c.Name)]
	if !ok {
		near := strutil.Nearest(strings.ToLower(c.Name), lowerAll(exprFunctionNames), 1)
		hint := ""
		if len(near) > 0 {
			hint = fmt.Sprintf("; did you mean %s()?", canonicalFunction(near[0]))
		}
		e.report("expr-unknown-function", pos, fmt.Sprintf("%s() is not a GitHub Actions expression function%s", c.Name, hint))
		return
	}
	n := len(c.Args)
	if n < fn.min || (fn.max >= 0 && n > fn.max) {
		want := fmt.Sprintf("%d", fn.min)
		switch {
		case fn.max < 0:
			want = fmt.Sprintf("at least %d", fn.min)
		case fn.max != fn.min:
			want = fmt.Sprintf("%d or %d", fn.min, fn.max)
		}
		e.report("expr-arity", pos, fmt.Sprintf("%s() takes %s argument(s), got %d", fn.name, want, n))
		return
	}
	switch fn.name {
	case "format":
		if lit, ok := c.Args[0].(*expr.Literal); ok && lit.Kind == expr.LitString {
			e.checkFormat(lit.Str, n-1, pos)
		}
	case "contains":
		s := e.evalAt(c.Args[0], ctx, workflow.Pos{})
		if s != nil && (s.Kind == shape.Object || s.Kind == shape.UnknownObject) {
			e.report("expr-contains-types", pos, fmt.Sprintf("contains() searches strings and arrays, but its first argument is %s%s; the result is always false", s.String(), from(s)))
		}
	case "hashFiles":
		for _, a := range c.Args {
			if lit, ok := a.(*expr.Literal); ok && lit.Kind == expr.LitString {
				if strings.HasPrefix(lit.Str, "/") || strings.HasPrefix(lit.Str, "..") || strings.Contains(lit.Str, "/../") {
					e.report("expr-hashfiles-absolute", pos, fmt.Sprintf("hashFiles(%q) names a path outside the workspace; hashFiles only matches under GITHUB_WORKSPACE and returns an empty string", lit.Str))
				}
			}
		}
	}
}

var rePlaceholder = regexp.MustCompile(`\{(\d+)\}`)

// checkFormat is expr-format-placeholders over a literal format string.
func (e *shapeEnv) checkFormat(format string, args int, pos workflow.Pos) {
	unescaped := strings.ReplaceAll(strings.ReplaceAll(format, "{{", ""), "}}", "")
	used := map[int]bool{}
	for _, m := range rePlaceholder.FindAllStringSubmatch(unescaped, -1) {
		i, _ := strconv.Atoi(m[1])
		used[i] = true
		if i >= args {
			e.report("expr-format-placeholders", pos, fmt.Sprintf("format(): placeholder {%d} has no argument (%d argument(s) given)", i, args))
			return
		}
	}
	for i := 0; i < args; i++ {
		if !used[i] {
			fd := at("expr-format-placeholders", pos, fmt.Sprintf("format(): argument %d is never used by a placeholder", i+1))
			fd.Severity = finding.Warning
			if pos.Line > 0 {
				e.problems = append(e.problems, fd)
			}
			return
		}
	}
}

var reNumeric = regexp.MustCompile(`^-?(0x[0-9a-fA-F]+|\d+(\.\d+)?([eE][-+]?\d+)?|\.\d+)$`)

// checkComparison is expr-comparison-type: a number compared with a
// non-numeric literal, or a boolean with a literal other than true/false.
func (e *shapeEnv) checkComparison(b *expr.Binary, x workflow.Expr, ctx shapeCtx) {
	pairs := [][2]expr.Node{{b.X, b.Y}, {b.Y, b.X}}
	for _, p := range pairs {
		lit, ok := p[1].(*expr.Literal)
		if !ok || lit.Kind != expr.LitString {
			continue
		}
		if _, isLit := p[0].(*expr.Literal); isLit {
			continue
		}
		s := e.evalAt(p[0], ctx, workflow.Pos{})
		if s == nil {
			continue
		}
		switch s.Kind {
		case shape.Number:
			if !reNumeric.MatchString(strings.TrimSpace(lit.Str)) {
				e.report("expr-comparison-type", x.Pos, fmt.Sprintf("comparing a number%s with the non-numeric string '%s'; GitHub coerces the string to NaN and the comparison is never true", from(s), lit.Str))
			}
		case shape.Bool:
			// '' is the "not provided" check and is fine.
			if v := strings.ToLower(strings.TrimSpace(lit.Str)); v != "true" && v != "false" && v != "" {
				e.report("expr-comparison-type", x.Pos, fmt.Sprintf("comparing a boolean%s with the string '%s'; only 'true' and 'false' can match", from(s), lit.Str))
			}
		}
		return
	}
}

// stringEscapeFinding is expr-string-escape for an expression whose text
// does not tokenise because of its quoting.
func stringEscapeFinding(x workflow.Expr, err error) (finding.Finding, bool) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, `unexpected character '"'`):
		return at("expr-string-escape", x.Pos, "double-quoted strings are not valid in GitHub expressions; use single quotes ('...')"), true
	case strings.Contains(msg, "unterminated string"):
		return at("expr-string-escape", x.Pos, "unterminated string literal: a literal ' inside a string must be written as ''"), true
	}
	return finding.Finding{}, false
}

func lowerAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.ToLower(n)
	}
	return out
}

func canonicalFunction(lower string) string {
	if f, ok := exprFunctions[lower]; ok {
		return f.name
	}
	return lower
}
