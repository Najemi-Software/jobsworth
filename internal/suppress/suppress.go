// Package suppress parses inline `jobsworth-ignore` comments out of YAML
// source and applies them to findings (spec-rules §5). It works on lines,
// not the YAML tree: a comment is `#` at the start of a line or after
// whitespace, outside quotes and outside block scalars (a `run: |` body is
// script, not YAML, and its comments are ignored).
package suppress

import (
	"regexp"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
)

// Directive is one parsed suppression.
type Directive struct {
	// Kind is "line", "file", or "region".
	Kind   string
	Reason string
	// Rules are the ids (or aliases) named; Wildcard means `*`.
	Rules []string
	// Line is the source line of the comment (1-based).
	Line int
	// Anchor is the line a "line" directive applies to; From/To bound a
	// region (To is the last line of the file when unclosed).
	Anchor   int
	From, To int
	Wildcard bool
	Unclosed bool
	// used records whether the directive suppressed at least one finding.
	used bool
}

// Set is the parsed suppressions of one file.
type Set struct {
	Directives []*Directive
	// Problems are findings about the directives themselves: unknown rules
	// and unclosed regions. Wildcard and unused notes are produced by Apply.
	Problems []finding.Finding
}

var (
	reIgnore = regexp.MustCompile(`^jobsworth-ignore(-file|-start|-end)?\b(.*)$`)
	reBlock  = regexp.MustCompile(`(?:^|\s)(?:[^\s#]+:|-)\s*[|>][-+0-9]*\s*$`)
)

// Parse scans the source for directives. knownRule validates ids; nil
// accepts every id. resolve maps an alias to its canonical id (nil = identity).
func Parse(src []byte, knownRule func(string) bool, resolve func(string) string) *Set {
	if resolve == nil {
		resolve = func(s string) string { return s }
	}
	lines := strings.Split(string(src), "\n")
	set := &Set{}
	var pending []*Directive // own-line directives awaiting their anchor
	var open []*Directive    // unclosed region starts
	inBlock, blockIndent := false, 0

	for i, raw := range lines {
		lineNo := i + 1
		if inBlock {
			if strings.TrimSpace(raw) == "" || indent(raw) > blockIndent {
				continue
			}
			inBlock = false
		}
		code, comment := splitComment(raw)
		hasCode := strings.TrimSpace(code) != ""
		if hasCode && reBlock.MatchString(code) {
			inBlock, blockIndent = true, indent(raw)
		}
		if hasCode {
			for _, d := range pending {
				d.Anchor = lineNo
			}
			pending = nil
		}
		if comment == "" {
			continue
		}
		m := reIgnore.FindStringSubmatch(strings.TrimSpace(comment))
		if m == nil {
			continue
		}
		d := &Directive{Line: lineNo}
		body := m[2]
		if idx := strings.Index(body, " -- "); idx >= 0 {
			d.Reason = strings.TrimSpace(body[idx+4:])
			body = body[:idx]
		} else if strings.HasSuffix(body, " --") {
			body = strings.TrimSuffix(body, " --")
		}
		tokens := 0
		for _, tok := range strings.FieldsFunc(body, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			tokens++
			if tok == "*" {
				d.Wildcard = true
				continue
			}
			if knownRule != nil && !knownRule(tok) {
				set.Problems = append(set.Problems, finding.Finding{
					Severity: finding.Error, Rule: "suppress-unknown-rule", Line: lineNo, Column: 1,
					Message: "suppression names unknown rule '" + tok + "'",
				})
				continue
			}
			d.Rules = append(d.Rules, resolve(tok))
		}
		switch m[1] {
		case "-file":
			d.Kind = "file"
			d.From, d.To = 1, len(lines)
		case "-start":
			d.Kind = "region"
			d.From = lineNo
			open = append(open, d)
		case "-end":
			if len(open) == 0 {
				set.Problems = append(set.Problems, finding.Finding{
					Severity: finding.Warning, Rule: "suppress-unclosed", Line: lineNo, Column: 1,
					Message: "jobsworth-ignore-end without a matching jobsworth-ignore-start",
				})
				continue
			}
			last := open[len(open)-1]
			open = open[:len(open)-1]
			last.To = lineNo
			continue
		default:
			d.Kind = "line"
			if hasCode {
				d.Anchor = lineNo
			} else {
				pending = append(pending, d)
			}
		}
		if tokens == 0 && m[1] != "-end" {
			set.Problems = append(set.Problems, finding.Finding{
				Severity: finding.Error, Rule: "suppress-unknown-rule", Line: lineNo, Column: 1,
				Message: "suppression names no rule; list rule ids or use * to suppress everything on the line",
			})
		}
		set.Directives = append(set.Directives, d)
	}
	for _, d := range open {
		d.To = len(lines)
		d.Unclosed = true
		set.Problems = append(set.Problems, finding.Finding{
			Severity: finding.Warning, Rule: "suppress-unclosed", Line: d.Line, Column: 1,
			Message: "jobsworth-ignore-start has no matching jobsworth-ignore-end; runs to end of file",
		})
	}
	return set
}

// Apply splits findings into kept and suppressed. Suppressed findings carry
// the directive's reason. Meta-rule findings (suppress family) are never
// suppressed. Afterwards the set's Problems, plus suppress-wildcard and
// suppress-unused notes, are appended to kept.
func Apply(set *Set, fs []finding.Finding) (kept, suppressed []finding.Finding) {
	if set == nil {
		return fs, nil
	}
	for _, f := range fs {
		if d := match(set, f); d != nil {
			d.used = true
			f.Reason = d.Reason
			suppressed = append(suppressed, f)
			continue
		}
		kept = append(kept, f)
	}
	kept = append(kept, set.Problems...)
	for _, d := range set.Directives {
		if d.Wildcard {
			kept = append(kept, finding.Finding{
				Severity: finding.Note, Rule: "suppress-wildcard", Line: d.Line, Column: 1,
				Message: "jobsworth-ignore * hides every rule on this line, including future ones",
			})
		}
		if !d.used && (len(d.Rules) > 0 || d.Wildcard) {
			kept = append(kept, finding.Finding{
				Severity: finding.Note, Rule: "suppress-unused", Line: d.Line, Column: 1,
				Message: "suppression of " + describe(d) + " suppressed nothing",
			})
		}
	}
	return kept, suppressed
}

func match(set *Set, f finding.Finding) *Directive {
	if strings.HasPrefix(f.Rule, "suppress-") {
		return nil
	}
	for _, d := range set.Directives {
		if !d.Wildcard && !contains(d.Rules, f.Rule) {
			continue
		}
		switch d.Kind {
		case "line":
			if f.Line == d.Anchor && d.Anchor > 0 {
				return d
			}
		case "file", "region":
			if f.Line >= d.From && f.Line <= d.To {
				return d
			}
		}
	}
	return nil
}

func describe(d *Directive) string {
	if d.Wildcard {
		return "*"
	}
	names := append([]string(nil), d.Rules...)
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func indent(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
		} else if r == '\t' {
			n += 8
		} else {
			break
		}
	}
	return n
}

// splitComment separates a YAML line into code and comment text, honouring
// single and double quotes. The comment excludes its leading '#'.
func splitComment(line string) (code, comment string) {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			if i == 0 || line[i-1] != '\\' {
				inDouble = !inDouble
			}
		case c == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return line[:i], line[i+1:]
			}
		}
	}
	return line, ""
}
