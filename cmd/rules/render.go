package rules

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/term"

	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/scripts"
)

// --- table (spec-rules §4.1) ------------------------------------------------

const (
	ansiDim   = "\x1b[2m"
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

func idLabel(e entry) string {
	label := e.ID
	switch e.Status {
	case rules.Experimental:
		label += " (exp)"
	case rules.Deprecated:
		label += " (deprecated → " + e.Replacement + ")"
	}
	if e.Enrich {
		label += " (enrich)"
	}
	if !available(e) {
		label += " (unavailable)"
	}
	return label
}

// ruleTools names the executable each script rule needs; a rule whose tool
// is not on PATH never fires (advanced §5, review 1 N6).
var ruleTools = map[string]string{
	"shellcheck-codes": "shellcheck",
	"python-syntax":    "python3",
	"pwsh-syntax":      "pwsh",
	"node-syntax":      "node",
	"ruby-syntax":      "ruby",
}

// available reports whether the rule can fire on this machine.
func available(e entry) bool {
	tool, ok := ruleTools[e.ID]
	return !ok || scripts.Available(tool)
}

func writeListTable(w io.Writer, sel []entry, families []string, cfgPath string, noConfig, terminal bool) {
	header := []string{"ID", "DEFAULT", "EFFECTIVE", "SINCE", "SUMMARY"}
	if noConfig {
		header = []string{"ID", "DEFAULT", "SINCE", "SUMMARY"}
	}
	var rows [][]string
	counts := map[string]int{}
	anyChanged := false
	for _, e := range sel {
		eff := e.effectiveName()
		if e.changed {
			eff += " *"
			anyChanged = true
		}
		counts[e.effectiveName()]++
		row := []string{idLabel(e), e.defaultName(), eff, e.Since, e.Summary}
		if noConfig {
			row = []string{idLabel(e), e.defaultName(), e.Since, e.Summary}
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < len(row)-1 && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	line := func(cells []string, dim bool) {
		var b strings.Builder
		for i, c := range cells {
			if i == len(cells)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(fmt.Sprintf("%-*s  ", widths[i], c))
		}
		s := strings.TrimRight(b.String(), " ")
		if terminal && dim {
			s = ansiDim + s + ansiReset
		} else if terminal && strings.Contains(s, " *") {
			s = strings.Replace(s, " *", " "+ansiBold+"*"+ansiReset, 1)
		}
		fmt.Fprintln(w, s)
	}
	line(header, false)
	for i, row := range rows {
		line(row, sel[i].off)
	}
	scope := ""
	if len(families) == 1 {
		scope = " in family " + families[0]
	} else if len(families) > 1 {
		scope = " in families " + strings.Join(families, ", ")
	}
	fmt.Fprintf(w, "\n%d rules%s: %d error, %d warning, %d note, %d off",
		len(sel), scope, counts["error"], counts["warning"], counts["note"], counts["off"])
	if anyChanged && cfgPath != "" {
		fmt.Fprintf(w, " (* changed by %s)", filepath.Base(cfgPath))
	}
	fmt.Fprintln(w)
}

// --- explain table (spec-rules §4.2) ---------------------------------------

func headerLine(e entry, noConfig bool) string {
	s := fmt.Sprintf("%s  family: %s  default: %s", e.ID, e.Family, e.defaultName())
	if !noConfig {
		s += "  effective: " + e.effectiveName()
		if e.changed {
			s += " *"
		}
	}
	s += fmt.Sprintf("  since: %s  %s", e.Since, e.Status)
	if e.Status == rules.Deprecated {
		s += " → " + e.Replacement
	}
	return s
}

func writeExplainTable(w io.Writer, e entry, doc rules.Doc, noConfig, terminal bool) {
	width := 80
	if terminal {
		if f, ok := w.(*os.File); ok {
			if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 20 {
				width = cols
			}
		}
	}
	bold := func(s string) string {
		if terminal {
			return ansiBold + s + ansiReset
		}
		return s
	}
	fmt.Fprintln(w, bold(headerLine(e, noConfig)))
	fmt.Fprintln(w)
	for _, h := range doc.Order {
		fmt.Fprintln(w, bold(h))
		fmt.Fprintln(w)
		fmt.Fprint(w, strings.TrimRight(renderBody(doc.Sections[h], width), "\n"))
		fmt.Fprint(w, "\n\n")
	}
	if len(e.Related) > 0 {
		fmt.Fprintln(w, bold("See also"))
		fmt.Fprintln(w)
		for _, rel := range e.Related {
			fmt.Fprintf(w, "  jobsworth explain %s\n", rel)
		}
		fmt.Fprintln(w)
	}
}

var reFence = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")

// renderBody wraps prose at width and indents code blocks by four spaces.
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

// wrap re-flows paragraphs (blank-line separated) at width; empty input
// yields nothing.
func wrap(text string, width int) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		words := strings.Fields(para)
		col := 0
		for _, wd := range words {
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

// --- json (spec-rules §4.3) -------------------------------------------------

type jsonRule struct {
	ID          string   `json:"id"`
	Family      string   `json:"family"`
	Category    string   `json:"category"`
	Status      string   `json:"status"`
	Default     string   `json:"default"`
	Effective   string   `json:"effective"`
	Since       string   `json:"since"`
	Summary     string   `json:"summary"`
	Replacement string   `json:"replacement,omitempty"`
	Aliases     []string `json:"aliases"`
	Related     []string `json:"related"`
	Enrich      bool     `json:"enrich"`
	Available   bool     `json:"available"`
	Changed     bool     `json:"changed"`
}

func toJSON(e entry) jsonRule {
	j := jsonRule{
		ID: e.ID, Family: e.Family, Category: rules.CategoryOf(e.Rule), Enrich: e.Enrich, Available: available(e), Status: string(e.Status),
		Default: e.defaultName(), Effective: e.effectiveName(), Changed: e.changed,
		Since: e.Since, Summary: e.Summary, Aliases: []string{}, Related: []string{},
		Replacement: e.Replacement,
	}
	j.Aliases = append(j.Aliases, e.Aliases...)
	j.Related = append(j.Related, e.Related...)
	return j
}

func writeListJSON(w io.Writer, sel []entry, cfgPath string) error {
	doc := struct {
		Schema int        `json:"schema"`
		Config *string    `json:"config"`
		Rules  []jsonRule `json:"rules"`
	}{Schema: 1, Rules: []jsonRule{}}
	if cfgPath != "" {
		doc.Config = &cfgPath
	}
	for _, e := range sel {
		doc.Rules = append(doc.Rules, toJSON(e))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func writeExplainJSON(w io.Writer, e entry, doc rules.Doc, cfgPath string) error {
	out := struct {
		Schema int `json:"schema"`
		jsonRule
		Config   *string           `json:"config"`
		Doc      string            `json:"doc"`
		Sections map[string]string `json:"sections"`
	}{Schema: 1, jsonRule: toJSON(e), Doc: doc.Markdown, Sections: map[string]string{}}
	if cfgPath != "" {
		out.Config = &cfgPath
	}
	for heading, key := range rules.SectionKeys {
		if key == "related" {
			continue
		}
		out.Sections[key] = doc.Sections[heading]
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// --- markdown (spec-rules §4.4) --------------------------------------------

func writeListMarkdown(w io.Writer, sel []entry, noConfig bool) {
	byFamily := map[string][]entry{}
	for _, e := range sel {
		byFamily[e.Family] = append(byFamily[e.Family], e)
	}
	for _, fam := range rules.Families {
		es := byFamily[fam]
		if len(es) == 0 {
			continue
		}
		fmt.Fprintf(w, "## %s\n\n", fam)
		if noConfig {
			fmt.Fprintln(w, "| ID | Default | Since | Summary |")
			fmt.Fprintln(w, "|----|---------|-------|---------|")
		} else {
			fmt.Fprintln(w, "| ID | Default | Effective | Since | Summary |")
			fmt.Fprintln(w, "|----|---------|-----------|-------|---------|")
		}
		for _, e := range es {
			if noConfig {
				fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n", idLabel(e), e.defaultName(), e.Since, e.Summary)
			} else {
				eff := e.effectiveName()
				if e.changed {
					eff += " \\*"
				}
				fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s |\n", idLabel(e), e.defaultName(), eff, e.Since, e.Summary)
			}
		}
		fmt.Fprintln(w)
	}
}

func writeHeaderTable(w io.Writer, e entry, noConfig bool) {
	if noConfig {
		fmt.Fprintln(w, "| Family | Default | Since | Status |")
		fmt.Fprintln(w, "|--------|---------|-------|--------|")
		fmt.Fprintf(w, "| %s | %s | %s | %s |\n", e.Family, e.defaultName(), e.Since, e.Status)
		return
	}
	fmt.Fprintln(w, "| Family | Default | Effective | Since | Status |")
	fmt.Fprintln(w, "|--------|---------|-----------|-------|--------|")
	eff := e.effectiveName()
	if e.changed {
		eff += " \\*"
	}
	fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n", e.Family, e.defaultName(), eff, e.Since, e.Status)
}

// stripTitle drops the "# <id>" title and any test hints, since the
// reference page supplies its own heading.
func stripTitle(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	for i, l := range lines {
		if i == 0 && strings.HasPrefix(l, "# ") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(l), "<!-- jobsworth-test:") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimLeft(strings.Join(out, "\n"), "\n")
}

// writeDoc emits the full reference (spec-rules §4.4): contents, then every
// rule's doc under its family heading.
func writeDoc(w io.Writer, sel []entry, noConfig bool) error {
	fmt.Fprintln(w, "# jobsworth rules reference")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Generated by `jobsworth rules --doc --no-config`; regenerate with `go test ./cmd/rules -update`. Do not edit by hand.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Contents")
	fmt.Fprintln(w)
	byFamily := map[string][]entry{}
	for _, e := range sel {
		byFamily[e.Family] = append(byFamily[e.Family], e)
	}
	for _, fam := range rules.Families {
		es := byFamily[fam]
		if len(es) == 0 {
			continue
		}
		fmt.Fprintf(w, "- **%s**:", fam)
		for i, e := range es {
			sep := " "
			if i > 0 {
				sep = ", "
			}
			fmt.Fprintf(w, "%s[`%s`](#%s)", sep, e.ID, e.ID)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
	for _, fam := range rules.Families {
		es := byFamily[fam]
		if len(es) == 0 {
			continue
		}
		fmt.Fprintf(w, "## Family: %s\n\n", fam)
		for _, e := range es {
			doc, err := rules.LoadDoc(e.ID)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "### %s\n\n", e.ID)
			writeHeaderTable(w, e, noConfig)
			fmt.Fprintln(w)
			body := stripTitle(doc.Markdown)
			body = regexp.MustCompile(`(?m)^## `).ReplaceAllString(body, "#### ")
			fmt.Fprint(w, strings.TrimRight(body, "\n"))
			fmt.Fprintln(w)
			fmt.Fprintln(w)
		}
	}
	return nil
}
