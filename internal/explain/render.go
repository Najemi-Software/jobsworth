package explain

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Style selects terminal highlighting.
type Style struct {
	Terminal bool
}

const (
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiReset = "\x1b[0m"
)

func (s Style) bold(t string) string {
	if s.Terminal {
		return ansiBold + t + ansiReset
	}
	return t
}

func (s Style) dim(t string) string {
	if s.Terminal {
		return ansiDim + t + ansiReset
	}
	return t
}

// display shortens a local path relative to cwd.
func display(name, cwd string) string {
	if cwd == "" || !filepath.IsAbs(name) {
		return name
	}
	if rel, err := filepath.Rel(cwd, name); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return name
}

// Text renders a result as the §3 blocks.
func Text(w io.Writer, r Result, cwd string, st Style) {
	file := display(r.File, cwd)
	if r.Redirected {
		fmt.Fprintf(w, "line %d is inside the run: block starting at :%d; explaining findings anchored there\n\n", r.RedirectedFrom, r.RedirectedTo)
	}
	if len(r.Explanations) == 0 {
		if r.Total == 0 {
			fmt.Fprintf(w, "no findings in %s\n", file)
		} else if r.RedirectedTo > 0 || len(r.Nearby) > 0 || len(r.WhyPassed) > 0 {
			fmt.Fprintf(w, "no finding at :%d in %s.\n", lineOf(r), file)
		} else {
			fmt.Fprintf(w, "no finding selected in %s.\n", file)
		}
		for _, wp := range r.WhyPassed {
			fmt.Fprintln(w, wp)
		}
		if len(r.Nearby) > 0 {
			fmt.Fprintln(w, "nearest findings:")
			for _, f := range r.Nearby {
				fmt.Fprintf(w, "  %s:%d:%d  %s  %s  %s\n", file, f.Line, f.Column, f.Severity, f.Rule, f.Message)
			}
		}
		writeCallees(w, r, cwd)
		return
	}
	docsShown := map[string]bool{}
	for i, ex := range r.Explanations {
		if i > 0 {
			fmt.Fprintln(w)
		}
		f := ex.Finding
		where := display(f.File, cwd)
		if f.Line > 0 {
			where = fmt.Sprintf("%s:%d:%d", where, f.Line, f.Column)
		}
		fmt.Fprintf(w, "%s\n", st.bold(fmt.Sprintf("%s  %s  %s", where, f.Severity, f.Rule)))
		fmt.Fprintf(w, "  %s\n", f.Message)
		if ex.Suppressed {
			reason := ex.SuppressReason
			if reason == "" {
				reason = "no reason given"
			}
			fmt.Fprintf(w, "  (suppressed on :%d — %q)\n", ex.SuppressedBy, reason)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "context:")
		for _, l := range ex.Context.Lines {
			if strings.Contains(l, "←") {
				parts := strings.SplitN(l, "←", 2)
				fmt.Fprintf(w, "  %s%s\n", parts[0], st.bold("←"+parts[1]))
				continue
			}
			fmt.Fprintf(w, "  %s\n", l)
		}
		if ex.Context.Conclusion != "" {
			fmt.Fprintf(w, "  %s\n", st.bold("conclusion: "+ex.Context.Conclusion))
		}
		for _, sa := range ex.Context.SeeAlso {
			fmt.Fprintf(w, "  see also: %s fired on :%d for the same key.\n", sa.Rule, sa.Line)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "suppress:")
		if f.Line > 0 {
			fmt.Fprintf(w, "  add on line %d:    # jobsworth-ignore %s\n", f.Line, ex.Rule.ID)
		}
		fmt.Fprintf(w, "  or in .jobsworth.yml:   rules: { %s: off }\n", ex.Rule.ID)
		if ex.Doc != nil && !docsShown[ex.Rule.ID] {
			docsShown[ex.Rule.ID] = true
			fmt.Fprintln(w)
			fmt.Fprintln(w, st.bold("rule: "+ex.Rule.ID))
			for _, h := range ex.Doc.Order {
				if strings.HasPrefix(h, "Example") || h == "Suppress" {
					continue
				}
				fmt.Fprintln(w)
				fmt.Fprintln(w, st.bold(h))
				fmt.Fprintln(w)
				fmt.Fprint(w, strings.TrimRight(renderBody(ex.Doc.Sections[h], 80), "\n"))
				fmt.Fprintln(w)
			}
		}
	}
	if len(r.Others) > 0 {
		fmt.Fprintf(w, "\nalso on this line (not selected by column): ")
		var names []string
		for _, f := range r.Others {
			names = append(names, fmt.Sprintf("%s at column %d", f.Rule, f.Column))
		}
		fmt.Fprintln(w, strings.Join(names, ", "))
	}
	writeCallees(w, r, cwd)
}

func lineOf(r Result) int {
	return r.RequestedLine
}

func writeCallees(w io.Writer, r Result, cwd string) {
	for _, cf := range r.CalleeFindings {
		fmt.Fprintf(w, "\nthe callee at this line has %d finding(s); explain them with:\n", len(cf.Findings))
		seen := map[int]bool{}
		for _, f := range cf.Findings {
			if seen[f.Line] {
				continue
			}
			seen[f.Line] = true
			fmt.Fprintf(w, "  jobsworth explain %s:%d\n", display(cf.Callee, cwd), f.Line)
		}
	}
}

// Markdown renders the text form with headings and fenced blocks (§4.3).
func Markdown(w io.Writer, r Result, cwd string) {
	file := display(r.File, cwd)
	if len(r.Explanations) == 0 {
		fmt.Fprintf(w, "**No finding at %s:%d.**\n\n", file, lineOf(r))
		for _, wp := range r.WhyPassed {
			fmt.Fprintln(w, wp)
			fmt.Fprintln(w)
		}
		if len(r.Nearby) > 0 {
			fmt.Fprintln(w, "Nearest findings:")
			fmt.Fprintln(w)
			for _, f := range r.Nearby {
				fmt.Fprintf(w, "- `%s:%d:%d` %s **%s** %s\n", file, f.Line, f.Column, f.Severity, f.Rule, f.Message)
			}
		}
		return
	}
	for _, ex := range r.Explanations {
		f := ex.Finding
		fmt.Fprintf(w, "### `%s:%d:%d` %s `%s`\n\n%s\n\n", file, f.Line, f.Column, f.Severity, f.Rule, f.Message)
		if ex.Suppressed {
			fmt.Fprintf(w, "_Suppressed on line %d: %s_\n\n", ex.SuppressedBy, ex.SuppressReason)
		}
		fmt.Fprintln(w, "<details><summary>Context</summary>")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```")
		for _, l := range ex.Context.Lines {
			fmt.Fprintln(w, l)
		}
		if ex.Context.Conclusion != "" {
			fmt.Fprintln(w, "conclusion: "+ex.Context.Conclusion)
		}
		fmt.Fprintln(w, "```")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "</details>")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "**Suppress:** add `# jobsworth-ignore %s` on line %d, or `rules: { %s: off }` in `.jobsworth.yml`.\n\n", ex.Rule.ID, f.Line, ex.Rule.ID)
		if ex.Doc != nil {
			fmt.Fprintf(w, "<details><summary>Rule: %s</summary>\n\n", ex.Rule.ID)
			for _, h := range ex.Doc.Order {
				if strings.HasPrefix(h, "Example") || h == "Suppress" {
					continue
				}
				fmt.Fprintf(w, "#### %s\n\n%s\n\n", h, ex.Doc.Sections[h])
			}
			fmt.Fprintln(w, "</details>")
			fmt.Fprintln(w)
		}
	}
}

// JSON renders the §4.2 document.
func JSON(w io.Writer, r Result) error {
	type suppressBlock struct {
		Line    int               `json:"line"`
		Comment string            `json:"comment"`
		Config  map[string]string `json:"config"`
	}
	type docBlock struct {
		ID       string            `json:"id"`
		Sections map[string]string `json:"sections"`
	}
	type item struct {
		Finding    finding.Finding `json:"finding"`
		Suppressed *struct {
			Line   int    `json:"line"`
			Reason string `json:"reason"`
		} `json:"suppressed,omitempty"`
		Context  map[string]any `json:"context"`
		Doc      *docBlock      `json:"doc,omitempty"`
		Suppress suppressBlock  `json:"suppress"`
	}
	doc := struct {
		Schema         int               `json:"schema"`
		File           string            `json:"file"`
		Redirected     *int              `json:"redirected_from,omitempty"`
		Findings       []item            `json:"findings"`
		Nearby         []finding.Finding `json:"nearby,omitempty"`
		WhyPassed      []string          `json:"why_passed,omitempty"`
		CalleeFindings []CalleeFindings  `json:"callee_findings,omitempty"`
	}{Schema: 1, File: r.File, Findings: []item{}, Nearby: r.Nearby, WhyPassed: r.WhyPassed, CalleeFindings: r.CalleeFindings}
	if r.Redirected {
		from := r.RedirectedFrom
		doc.Redirected = &from
	}
	for _, ex := range r.Explanations {
		ctx := map[string]any{"kind": ex.Context.Kind}
		for k, v := range ex.Context.Data {
			ctx[k] = v
		}
		if ex.Context.Conclusion != "" {
			ctx["conclusion"] = ex.Context.Conclusion
		}
		if len(ex.Context.SeeAlso) > 0 {
			ctx["see_also"] = ex.Context.SeeAlso
		}
		it := item{Finding: ex.Finding, Context: ctx, Suppress: suppressBlock{
			Line: ex.Finding.Line, Comment: "# jobsworth-ignore " + ex.Rule.ID, Config: map[string]string{ex.Rule.ID: "off"},
		}}
		if ex.Suppressed {
			it.Suppressed = &struct {
				Line   int    `json:"line"`
				Reason string `json:"reason"`
			}{ex.SuppressedBy, ex.SuppressReason}
		}
		if ex.Doc != nil {
			sections := map[string]string{}
			for heading, key := range rules.SectionKeys {
				if key == "flagged" || key == "passes" || key == "related" {
					continue
				}
				sections[key] = ex.Doc.Sections[heading]
			}
			it.Doc = &docBlock{ID: ex.Rule.ID, Sections: sections}
		}
		doc.Findings = append(doc.Findings, it)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

var reFence = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")

// renderBody wraps prose at width and indents code blocks.
func renderBody(body string, width int) string {
	var b strings.Builder
	last := 0
	for _, loc := range reFence.FindAllStringSubmatchIndex(body, -1) {
		b.WriteString(wrap(strings.TrimSpace(body[last:loc[0]]), width))
		for _, l := range strings.Split(strings.TrimRight(body[loc[2]:loc[3]], "\n"), "\n") {
			b.WriteString("    " + l + "\n")
		}
		b.WriteString("\n")
		last = loc[1]
	}
	b.WriteString(wrap(strings.TrimSpace(body[last:]), width))
	return b.String()
}

func wrap(text string, width int) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		col := 0
		for _, wd := range strings.Fields(para) {
			if col > 0 && col+1+len(wd) > width {
				b.WriteString("\n")
				col = 0
			}
			if col > 0 {
				b.WriteString(" ")
				col++
			}
			b.WriteString(wd)
			col += len(wd)
		}
		b.WriteString("\n\n")
	}
	return b.String()
}
