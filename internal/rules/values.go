package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// valueSets are the documented value sets (medium §5.1).
var valueSets = []struct {
	re     *regexp.Regexp
	values []string
	events bool // compare against the file's on: events instead
}{
	{regexp.MustCompile(`^steps\.[A-Za-z0-9_-]+\.(outcome|conclusion)$`), []string{"success", "failure", "cancelled", "skipped"}, false},
	{regexp.MustCompile(`^(needs|jobs)\.[A-Za-z0-9_-]+\.result$`), []string{"success", "failure", "cancelled", "skipped"}, false},
	{regexp.MustCompile(`^job\.status$`), []string{"success", "failure", "cancelled"}, false},
	{regexp.MustCompile(`^runner\.os$`), []string{"Linux", "Windows", "macOS"}, false},
	{regexp.MustCompile(`^runner\.arch$`), []string{"X86", "X64", "ARM", "ARM64"}, false},
	{regexp.MustCompile(`^github\.ref_type$`), []string{"branch", "tag"}, false},
	{regexp.MustCompile(`^github\.event_name$`), nil, true},
}

// reComparison matches `path op 'literal'` and `'literal' op path`.
var (
	reCmpPathLit = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.-]*)\s*(==|!=)\s*'([^']*)'`)
	reCmpLitPath = regexp.MustCompile(`'([^']*)'\s*(==|!=)\s*([A-Za-z_][A-Za-z0-9_.-]*)`)
)

// CheckValueSets implements §5.1 over every expression in a file.
func CheckValueSets(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	for _, e := range f.Exprs {
		var pairs [][2]string
		for _, m := range reCmpPathLit.FindAllStringSubmatch(e.Text, -1) {
			pairs = append(pairs, [2]string{m[1], m[3]})
		}
		for _, m := range reCmpLitPath.FindAllStringSubmatch(e.Text, -1) {
			pairs = append(pairs, [2]string{m[3], m[1]})
		}
		for _, p := range pairs {
			path, lit := p[0], p[1]
			for _, vs := range valueSets {
				if !vs.re.MatchString(path) {
					continue
				}
				values := vs.values
				sev := finding.Error
				if vs.events {
					// Inside a reusable workflow github.* is the caller's
					// context, so workflow_call never bounds event_name; with
					// no other trigger the set is unknowable (review 3 D9).
					values = ownEvents(f)
					sev = finding.Warning
					if len(values) == 0 {
						break
					}
				}
				if lit == "" && contains(values, "success") {
					// '' is what a status reads for a step or job that was
					// never reached (medium review 2 N8).
					continue
				}
				if !contains(values, lit) {
					fd := at("value-set-mismatch", e.Pos, fmt.Sprintf("%s is compared with %q, which is not one of its values (%s)", path, lit, strings.Join(values, ", ")))
					fd.Severity = sev
					if vs.events {
						fd.Message = fmt.Sprintf("%s is compared with %q, but this workflow's on: only declares %s; the branch is dead", path, lit, strings.Join(values, ", "))
					}
					out = append(out, fd)
				}
				break
			}
		}
	}
	return out
}

// CheckChoices implements §5.3 over a file's declared inputs.
func CheckChoices(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	for _, in := range f.Inputs {
		if in.Type != "choice" {
			continue
		}
		if len(in.Options) == 0 {
			out = append(out, at("choice-no-options", in.Pos, fmt.Sprintf("input '%s' is type choice but declares no options", in.Name)))
			continue
		}
		if in.HasDefault && !in.Default.IsExpr() && !contains(in.Options, in.Default.Value) {
			out = append(out, at("choice-default-not-option", in.Default.Pos, fmt.Sprintf("input '%s' defaults to %q, which is not one of its options (%s)", in.Name, in.Default.Value, strings.Join(in.Options, ", "))))
		}
	}
	return out
}

var reBareWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// CheckIfs implements §5.4 over every if: in the file.
func CheckIfs(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	seen := map[workflow.Pos]bool{}
	for _, e := range f.Exprs {
		if e.Key != "if" || seen[e.Pos] {
			continue
		}
		seen[e.Pos] = true
		text := e.Raw
		if strings.Count(text, "${{") >= 2 {
			out = append(out, at("if-double-wrapped", e.Pos, "if: joins two ${{ }} blocks into one string, which is always truthy; use a single ${{ ... }} around the whole condition"))
			continue
		}
		if e.IsIf {
			v := strings.TrimSpace(text)
			if reBareWord.MatchString(v) && v != "true" && v != "false" {
				out = append(out, at("if-always-true-string", e.Pos, fmt.Sprintf("if: %s is a non-empty string, which is always truthy; did you mean %s()?", v, v)))
				continue
			}
		}
		cond := strings.TrimSpace(e.Text)
		// A condition that already tests cancelled() has applied the remedy
		// the rule suggests (medium review 2 N6).
		if strings.Contains(cond, "always()") && strings.Contains(cond, "&&") && !strings.Contains(cond, "cancelled()") && !onlyStatusChecks(cond) {
			out = append(out, at("if-status-with-other", e.Pos, "if: combines always() with && so the step still runs on cancel; use !cancelled() instead of always()"))
		}
	}
	return out
}

var reStatusTerm = regexp.MustCompile(`^(needs\.[A-Za-z0-9_-]+\.result|steps\.[A-Za-z0-9_-]+\.(outcome|conclusion)|job\.status)\s*(==|!=)\s*'[a-z]+'$`)

// onlyStatusChecks reports whether every && operand besides always() is a
// status comparison: `always() && needs.x.result == 'success'` is the
// documented idiom for "run after failures, but require x" (review N4).
func onlyStatusChecks(cond string) bool {
	cond = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(cond), "${{"), "}}"))
	return statusOnly(cond)
}

// statusOnly reports whether every leaf of a boolean expression is
// always() or a status comparison, seeing through &&, || and ! groups
// (medium review 3 N10). The check is per leaf, so a group mixing a
// status with anything else still fails.
func statusOnly(expr string) bool {
	expr = strings.TrimSpace(expr)
	for {
		switch {
		case strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!="):
			expr = strings.TrimSpace(expr[1:])
			continue
		case wrappedInParens(expr):
			expr = strings.TrimSpace(expr[1 : len(expr)-1])
			continue
		}
		break
	}
	if expr == "" || expr == "always()" {
		return true
	}
	for _, op := range []string{"&&", "||"} {
		if parts := splitTopLevel(expr, op); len(parts) > 1 {
			for _, p := range parts {
				if !statusOnly(p) {
					return false
				}
			}
			return true
		}
	}
	return reStatusTerm.MatchString(expr)
}

// wrappedInParens reports whether the whole expression is one
// parenthesised group.
func wrappedInParens(s string) bool {
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return false
	}
	depth := 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// splitTopLevel splits at op outside parentheses and single quotes.
func splitTopLevel(s, op string) []string {
	var parts []string
	depth, inQuote, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'':
			inQuote = !inQuote
		case inQuote:
		case s[i] == '(':
			depth++
		case s[i] == ')':
			depth--
		case depth == 0 && strings.HasPrefix(s[i:], op):
			parts = append(parts, s[start:i])
			i += len(op) - 1
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// ownEvents is the file's triggers that bound github.event_name: every
// event except workflow_call, which carries the caller's event.
func ownEvents(f *workflow.File) []string {
	var out []string
	for _, ev := range f.Events {
		if ev != "workflow_call" {
			out = append(out, ev)
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
