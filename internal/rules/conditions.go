package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/expr"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family conditions (advanced §4): the if: expression as a condition.

// CallerValue is one literal a caller passed for an input (§4.3).
type CallerValue struct {
	Site   string
	Value  string
	IsExpr bool
}

// ConditionInputs is what CheckConditions knows beyond the file.
type ConditionInputs struct {
	// CallerValues maps an input name to what every walked caller passed.
	CallerValues map[string][]CallerValue
	// KnownCallers is how many callers exist (walked or found by the
	// repository scan); WalkedCallers how many contributed values. The
	// sets are complete only when they agree and Complete is set: a
	// fetched callee has callers this walk cannot see (medium review 7
	// D15).
	KnownCallers, WalkedCallers int
	Complete                    bool
	// Seeded are `check --input k=v` values for the entry's own
	// workflow_dispatch inputs; complete by definition.
	Seeded map[string]string
}

// tri is a three-valued boolean.
type tri int

const (
	triUnknown tri = iota
	triFalse
	triTrue
)

func triOf(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

func (t tri) not() tri {
	switch t {
	case triTrue:
		return triFalse
	case triFalse:
		return triTrue
	}
	return triUnknown
}

func (t tri) and(u tri) tri {
	if t == triFalse || u == triFalse {
		return triFalse
	}
	if t == triTrue && u == triTrue {
		return triTrue
	}
	return triUnknown
}

func (t tri) or(u tri) tri {
	if t == triTrue || u == triTrue {
		return triTrue
	}
	if t == triFalse && u == triFalse {
		return triFalse
	}
	return triUnknown
}

// otherValue stands for "any value not among the compared literals".
const otherValue = "\x00other"

// condEval evaluates an expression under one assignment of path values
// and one status.
type condEval struct {
	assign map[string]string
	status string
}

// value resolves a node to a concrete literal when possible.
func (ev condEval) value(n expr.Node) (string, bool) {
	switch x := n.(type) {
	case *expr.Literal:
		switch x.Kind {
		case expr.LitString:
			return x.Str, true
		case expr.LitNumber:
			return strconv.FormatFloat(x.Num, 'f', -1, 64), true
		case expr.LitBool:
			return strconv.FormatBool(x.Bool), true
		case expr.LitNull:
			return "", true
		}
	case *expr.Ident, *expr.Member, *expr.Index, *expr.Wildcard:
		if v, ok := ev.assign[chainText(n)]; ok && v != otherValue {
			return v, true
		}
	}
	return "", false
}

func chainText(n expr.Node) string {
	cs := expr.Chains(n)
	if len(cs) == 1 {
		return strings.ToLower(cs[0].String())
	}
	return ""
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "", "false", "0", "null":
		return false
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f == 0 {
		return false
	}
	return true
}

func (ev condEval) eval(n expr.Node) tri {
	switch x := n.(type) {
	case *expr.Literal:
		v, _ := ev.value(x)
		return triOf(truthy(v))
	case *expr.Unary:
		return ev.eval(x.X).not()
	case *expr.Binary:
		switch x.Op {
		case "&&":
			return ev.eval(x.X).and(ev.eval(x.Y))
		case "||":
			return ev.eval(x.X).or(ev.eval(x.Y))
		case "==", "!=":
			a, ok1 := ev.value(x.X)
			b, ok2 := ev.value(x.Y)
			if !ok1 || !ok2 {
				// A path compared against a literal it is not assigned to.
				if ta := chainText(x.X); ta != "" && ev.assign[ta] == otherValue {
					if _, lit := x.Y.(*expr.Literal); lit {
						return triOf(x.Op == "!=")
					}
				}
				if tb := chainText(x.Y); tb != "" && ev.assign[tb] == otherValue {
					if _, lit := x.X.(*expr.Literal); lit {
						return triOf(x.Op == "!=")
					}
				}
				return triUnknown
			}
			eq := strings.EqualFold(a, b)
			if fa, err1 := strconv.ParseFloat(a, 64); err1 == nil {
				if fb, err2 := strconv.ParseFloat(b, 64); err2 == nil {
					eq = fa == fb
				}
			}
			return triOf(eq == (x.Op == "=="))
		case "<", ">", "<=", ">=":
			a, ok1 := ev.value(x.X)
			b, ok2 := ev.value(x.Y)
			if !ok1 || !ok2 {
				return triUnknown
			}
			fa, err1 := strconv.ParseFloat(a, 64)
			fb, err2 := strconv.ParseFloat(b, 64)
			if err1 != nil || err2 != nil {
				return triUnknown
			}
			switch x.Op {
			case "<":
				return triOf(fa < fb)
			case ">":
				return triOf(fa > fb)
			case "<=":
				return triOf(fa <= fb)
			}
			return triOf(fa >= fb)
		}
	case *expr.Call:
		switch strings.ToLower(x.Name) {
		case "always":
			return triTrue
		case "success":
			return triOf(ev.status == "success")
		case "failure":
			return triOf(ev.status == "failure")
		case "cancelled":
			return triOf(ev.status == "cancelled")
		}
		return triUnknown
	case *expr.Ident, *expr.Member, *expr.Index, *expr.Wildcard:
		if v, ok := ev.value(x); ok {
			return triOf(truthy(v))
		}
	}
	return triUnknown
}

// atoms collects the paths compared against literals and the literals
// they are compared with, plus whether status functions appear.
func atoms(n expr.Node) (paths map[string]map[string]bool, status bool) {
	paths = map[string]map[string]bool{}
	var visit func(n expr.Node)
	visit = func(n expr.Node) {
		switch x := n.(type) {
		case *expr.Binary:
			if x.Op == "==" || x.Op == "!=" {
				for _, pair := range [][2]expr.Node{{x.X, x.Y}, {x.Y, x.X}} {
					if lit, ok := pair[1].(*expr.Literal); ok {
						if t := chainText(pair[0]); t != "" {
							if paths[t] == nil {
								paths[t] = map[string]bool{}
							}
							v, _ := (condEval{}).value(lit)
							paths[t][v] = true
						}
					}
				}
			}
			visit(x.X)
			visit(x.Y)
		case *expr.Unary:
			visit(x.X)
		case *expr.Call:
			switch strings.ToLower(x.Name) {
			case "success", "failure", "cancelled", "always":
				status = true
			}
			for _, a := range x.Args {
				visit(a)
			}
		}
	}
	visit(n)
	return paths, status
}

const combinationCap = 256

// satisfiable reports whether some assignment makes the expression true
// or unknown; fixed pins given paths to given values (§4.3 seeds).
func satisfiable(n expr.Node, fixed map[string][]string) (sat bool, decided bool) {
	paths, status := atoms(n)
	names := make([]string, 0, len(paths))
	for p := range paths {
		if _, pinned := fixed[p]; !pinned {
			names = append(names, p)
		}
	}
	sort.Strings(names)
	for p := range fixed {
		names = append(names, p)
	}
	values := make([][]string, len(names))
	total := 1
	for i, p := range names {
		if vs, pinned := fixed[p]; pinned {
			values[i] = vs
		} else {
			for v := range paths[p] {
				values[i] = append(values[i], v)
			}
			sort.Strings(values[i])
			values[i] = append(values[i], otherValue)
		}
		total *= len(values[i])
		if total > combinationCap {
			return true, false
		}
	}
	statuses := []string{"success"}
	if status {
		statuses = []string{"success", "failure", "cancelled"}
	}
	idx := make([]int, len(names))
	for {
		assign := map[string]string{}
		for i, p := range names {
			assign[p] = values[i][idx[i]]
		}
		for _, st := range statuses {
			if (condEval{assign: assign, status: st}).eval(n) != triFalse {
				return true, true
			}
		}
		i := 0
		for i < len(idx) {
			idx[i]++
			if idx[i] < len(values[i]) {
				break
			}
			idx[i] = 0
			i++
		}
		if i == len(idx) {
			break
		}
	}
	return false, true
}

// dispatchDomain is the value set a workflow_dispatch input can take:
// {true, false} for booleans, the options for a choice, unbounded for
// string and number. declared is false when the file does not declare
// the input under workflow_dispatch.
func dispatchDomain(f *workflow.File, name string) (domain []string, bounded, declared bool) {
	for _, in := range f.Inputs {
		if in.Trigger != "workflow_dispatch" || !strings.EqualFold(in.Name, name) {
			continue
		}
		switch in.Type {
		case "boolean":
			return []string{"true", "false"}, true, true
		case "choice":
			return in.Options, len(in.Options) > 0, true
		}
		return nil, false, true
	}
	return nil, false, false
}

// CheckConditions implements §4.2 and §4.3 over every if: in a file.
func CheckConditions(f *workflow.File, in ConditionInputs) []finding.Finding {
	var out []finding.Finding
	complete := in.Complete && in.KnownCallers > 0 && in.WalkedCallers >= in.KnownCallers
	for _, x := range f.Exprs {
		if x.Key != "if" {
			continue
		}
		if !x.IsIf && strings.TrimSpace(x.Raw) != "${{ "+strings.TrimSpace(x.Text)+" }}" && strings.TrimSpace(x.Raw) != "${{"+strings.TrimSpace(x.Text)+"}}" {
			continue // mixed text: medium §5.4 owns it
		}
		node, err := expr.Parse(x.Text)
		if err != nil {
			continue
		}
		where := "step"
		if x.Step < 0 {
			where = "job '" + x.Job + "'"
		} else {
			where = fmt.Sprintf("job '%s' step #%d", x.Job, x.Step+1)
			if x.Job == "" {
				where = fmt.Sprintf("step #%d", x.Step+1)
			}
		}
		if lit, ok := node.(*expr.Literal); ok && lit.Kind == expr.LitBool && !lit.Bool {
			out = append(out, at("if-literal-false", x.Pos, where+" has if: false; it never runs (deliberate disablement)"))
			continue
		}
		if s, ok := node.(*expr.Call); ok && strings.EqualFold(s.Name, "success") && len(s.Args) == 0 {
			out = append(out, at("if-redundant-success", x.Pos, where+" has if: success(), which is the default"))
			continue
		}
		if msg := statusContradiction(f, node, x); msg != "" {
			out = append(out, at("if-status-contradiction", x.Pos, where+": "+msg))
			continue
		}
		if sat, decided := satisfiable(node, nil); decided && !sat {
			out = append(out, at("if-always-false", x.Pos, where+" has a condition that is false for every value ("+strings.TrimSpace(x.Text)+"); it never runs"))
			continue
		}
		// Caller value sets (§4.3) and --input seeds.
		refs := workflow.InputRefs(x.Text)
		if len(refs) == 0 {
			continue
		}
		fixed := map[string][]string{}
		var callers []string
		ok := true
		for _, name := range refs {
			if v, seeded := in.Seeded[name]; seeded {
				fixed["inputs."+strings.ToLower(name)] = []string{v}
				continue
			}
			vals := in.CallerValues[name]
			if !complete || len(vals) == 0 {
				ok = false
				break
			}
			set := map[string]bool{}
			// A workflow_dispatch declaration adds the human's domain: the
			// type's values, or an unbounded set for free-form types
			// (medium review 8 D17).
			if domain, bounded, declared := dispatchDomain(f, name); declared {
				if !bounded {
					ok = false
					break
				}
				for _, v := range domain {
					set[v] = true
				}
			}
			for _, cv := range vals {
				if cv.IsExpr {
					ok = false
					break
				}
				set[cv.Value] = true
				callers = append(callers, cv.Site+"="+cv.Value)
			}
			var list []string
			for v := range set {
				list = append(list, v)
			}
			sort.Strings(list)
			fixed["inputs."+strings.ToLower(name)] = list
		}
		if ok && len(fixed) > 0 {
			if sat, decided := satisfiable(node, fixed); decided && !sat {
				sort.Strings(callers)
				src := "the seeded inputs"
				if len(callers) > 0 {
					src = "every known caller (" + strings.Join(dedupeStrings(callers), ", ") + ")"
				}
				out = append(out, at("if-dead-for-all-callers", x.Pos, fmt.Sprintf("%s has a condition that is false for %s; it never runs", where, src)))
				continue
			}
		}
		// Per caller (off by default).
		bySite := map[string]map[string][]string{}
		for _, name := range refs {
			for _, cv := range in.CallerValues[name] {
				if cv.IsExpr {
					continue
				}
				if bySite[cv.Site] == nil {
					bySite[cv.Site] = map[string][]string{}
				}
				bySite[cv.Site]["inputs."+strings.ToLower(name)] = []string{cv.Value}
			}
		}
		var sites []string
		for s := range bySite {
			sites = append(sites, s)
		}
		sort.Strings(sites)
		for _, s := range sites {
			if len(bySite[s]) != len(refs) {
				continue
			}
			if sat, decided := satisfiable(node, bySite[s]); decided && !sat {
				out = append(out, at("if-dead-for-caller", x.Pos, fmt.Sprintf("%s has a condition that is false for the values %s passes", where, s)))
			}
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// statusContradiction is if-status-contradiction: two positive status
// functions that exclude each other, !always(), or failure() on a job
// with nothing before it.
func statusContradiction(f *workflow.File, node expr.Node, x workflow.Expr) string {
	positive := map[string]bool{}
	negAlways := false
	var visit func(n expr.Node, neg bool)
	visit = func(n expr.Node, neg bool) {
		switch t := n.(type) {
		case *expr.Call:
			name := strings.ToLower(t.Name)
			switch name {
			case "success", "failure", "cancelled":
				if !neg {
					positive[name] = true
				}
			case "always":
				if neg {
					negAlways = true
				}
			}
		case *expr.Unary:
			visit(t.X, !neg)
		case *expr.Binary:
			if t.Op == "&&" {
				visit(t.X, neg)
				visit(t.Y, neg)
			}
		}
	}
	visit(node, false)
	if negAlways {
		return "!always() is never true"
	}
	if len(positive) > 1 {
		var names []string
		for n := range positive {
			names = append(names, n+"()")
		}
		sort.Strings(names)
		return strings.Join(names, " && ") + " cannot both hold"
	}
	if positive["failure"] && x.Step < 0 && x.Job != "" {
		if j := workflow.JobByName(f, x.Job); j != nil && len(j.Needs) == 0 {
			return "failure() on a job with no needs:; nothing before it can have failed"
		}
	}
	return ""
}

// --- §4.1 type and §4.4 dead branches, run with shapes at hand -----------------

// checkConditionTypes is if-not-boolean, if-string-compare-bool,
// expr-or-default-boolean and expr-and-or-ternary-falsy for one
// expression.
func (e *shapeEnv) checkConditionTypes(node expr.Node, x workflow.Expr, ctx shapeCtx) {
	if x.Key == "if" && (x.IsIf || isWholeExpr(x)) {
		s := e.evalAt(node, ctx, workflow.Pos{})
		if s != nil && s.Kind != shape.Any && s.Kind != shape.Bool {
			if _, isBinary := node.(*expr.Binary); !isBinary {
				kind := s.Kind.String()
				if s.Kind == shape.String {
					kind = "a string"
				}
				e.report("if-not-boolean", x.Pos, fmt.Sprintf("if: is %s%s, not a boolean; any non-empty string is truthy, including 'false'; compare it, e.g. == 'true'", kind, from(s)))
			}
		}
	}
	var visit func(n expr.Node)
	visit = func(n expr.Node) {
		switch t := n.(type) {
		case *expr.Binary:
			switch t.Op {
			case "==", "!=":
				for _, pair := range [][2]expr.Node{{t.X, t.Y}, {t.Y, t.X}} {
					lit, ok := pair[1].(*expr.Literal)
					if !ok || lit.Kind != expr.LitBool {
						continue
					}
					if txt := chainText(pair[0]); strings.HasPrefix(txt, "steps.") && strings.Contains(txt, ".outputs.") {
						e.report("if-string-compare-bool", x.Pos, fmt.Sprintf("%s is a string, and 'true' == true is false; compare with the string literal '%v'", txt, lit.Bool))
					}
				}
			case "||":
				if cx := chainText(t.X); strings.HasPrefix(cx, "inputs.") {
					if lit, ok := t.Y.(*expr.Literal); ok && lit.Kind == expr.LitString {
						if s := e.evalAt(t.X, ctx, workflow.Pos{}); s != nil && s.Kind == shape.Bool {
							e.report("expr-or-default-boolean", x.Pos, fmt.Sprintf("%s || '%s': the input is a boolean, so false falls through to the default; use %s != '' && %s || '%s'", cx, lit.Str, cx, cx, lit.Str))
						}
					}
				}
				if l, ok := t.X.(*expr.Binary); ok && l.Op == "&&" && canBeFalsy(e, l.Y, ctx) && !guardsSame(l) {
					e.report("expr-and-or-ternary-falsy", x.Pos, fmt.Sprintf("cond && x || y yields y when x is falsy; %s can be '', 0 or false", exprText(l.Y)))
				}
			}
			visit(t.X)
			visit(t.Y)
		case *expr.Unary:
			visit(t.X)
		case *expr.Call:
			for _, a := range t.Args {
				visit(a)
			}
		}
	}
	visit(node)
}

// guardsSame reports the `x != ” && x` form, where the guard already
// handles the empty case.
func guardsSame(l *expr.Binary) bool {
	g, ok := l.X.(*expr.Binary)
	if !ok || g.Op != "!=" {
		return false
	}
	lit, ok := g.Y.(*expr.Literal)
	return ok && lit.Kind == expr.LitString && lit.Str == "" && chainText(g.X) != "" && chainText(g.X) == chainText(l.Y)
}

func isWholeExpr(x workflow.Expr) bool {
	raw := strings.TrimSpace(x.Raw)
	return strings.HasPrefix(raw, "${{") && strings.HasSuffix(raw, "}}") && strings.Count(raw, "${{") == 1
}

func canBeFalsy(e *shapeEnv, n expr.Node, ctx shapeCtx) bool {
	switch t := n.(type) {
	case *expr.Literal:
		v, _ := (condEval{}).value(t)
		return !truthy(v)
	case *expr.Ident, *expr.Member, *expr.Index:
		s := e.evalAt(n, ctx, workflow.Pos{})
		return s != nil && s.Kind == shape.Bool
	}
	return false
}

func exprText(n expr.Node) string {
	if t := chainText(n); t != "" {
		return t
	}
	if lit, ok := n.(*expr.Literal); ok {
		v, _ := (condEval{}).value(lit)
		return "'" + v + "'"
	}
	return "the middle operand"
}
