// Package report renders check results as text or JSON (spec §8).
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Summary is the trailing tally (§8.1 last line, §8.2 "summary").
type Summary struct {
	Entries  int `json:"entries"`
	Failed   int `json:"failed"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Notes    int `json:"notes"`
}

// Summarize tallies results against the fail threshold. A callee's
// finding reached from several entries counts once (medium review 3
// N11); the failed count keeps every entry that reached it.
func Summarize(results []check.Result, failOn finding.Severity) Summary {
	s := Summary{Entries: len(results)}
	d := NewDedupe()
	Seed(d, results)
	for _, r := range results {
		if check.Failed(r, failOn) {
			s.Failed++
		}
		var fresh []finding.Finding
		for _, f := range r.Files {
			kept, _ := d.Split(r.Entry, f)
			fresh = append(fresh, kept...)
		}
		c := finding.Tally(fresh)
		s.Errors += c.Errors
		s.Warnings += c.Warnings
		s.Notes += c.Notes
	}
	return s
}

// Dedupe remembers callee findings already reported earlier in a
// multi-entry run, so a reusable workflow or action reached from N
// entries has its findings printed once (medium review 3 N11). An
// entry's own findings are never deduplicated.
type Dedupe struct {
	seen map[string]string // finding key -> entry that reported it first
}

// NewDedupe returns an empty memory.
func NewDedupe() *Dedupe { return &Dedupe{seen: map[string]string{}} }

// Seed records every entry's own findings before anything is printed, so
// a callee that is itself an entry is reported under its own listing
// whatever the entry order (medium review 4 D11).
func Seed(d *Dedupe, results []check.Result) {
	if d == nil {
		return
	}
	for _, r := range results {
		for _, f := range r.Files {
			if f.Name != r.Entry {
				continue
			}
			for _, fd := range f.Findings {
				if _, ok := d.seen[findingKey(fd)]; !ok {
					d.seen[findingKey(fd)] = r.Entry
				}
			}
		}
	}
}

func findingKey(f finding.Finding) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s", f.File, f.Line, f.Column, f.Rule, f.Message)
}

// Split partitions a file's findings into those first reported here
// (recorded under entry) and those already reported, with the entry
// that reported them. A nil Dedupe keeps everything.
func (d *Dedupe) Split(entry string, f check.FileResult) (fresh []finding.Finding, repeatedUnder []string) {
	if d == nil {
		return f.Findings, nil
	}
	if f.Name == entry {
		// An entry's own findings always print, and seed the memory so a
		// later entry reaching this file as a callee does not repeat them.
		for _, fd := range f.Findings {
			if _, ok := d.seen[findingKey(fd)]; !ok {
				d.seen[findingKey(fd)] = entry
			}
		}
		return f.Findings, nil
	}
	for _, fd := range f.Findings {
		k := findingKey(fd)
		if first, ok := d.seen[k]; ok {
			repeatedUnder = append(repeatedUnder, first)
			continue
		}
		d.seen[k] = entry
		fresh = append(fresh, fd)
	}
	return fresh, repeatedUnder
}

// Fresh returns a result's findings in print order with callee findings
// already reported (under d) removed: what `--last` numbers.
func Fresh(d *Dedupe, r check.Result) []finding.Finding {
	var out []finding.Finding
	for _, f := range r.Files {
		kept, _ := d.Split(r.Entry, f)
		out = append(out, kept...)
	}
	return out
}

// DisplayPath renders a local absolute path relative to cwd when it is
// underneath it; other names (fetched display names, paths outside cwd) are
// returned unchanged.
func DisplayPath(name, cwd string) string {
	if !filepath.IsAbs(name) || cwd == "" {
		return name
	}
	rel, err := filepath.Rel(cwd, name)
	if err != nil || strings.HasPrefix(rel, "..") {
		return name
	}
	return filepath.ToSlash(rel)
}

// Numberer hands out the running finding numbers `check` prints on a
// terminal (spec-explain §5), so `explain --last <n>` can refer to them.
// A nil Numberer prints no numbers.
type Numberer struct{ n int }

func (n *Numberer) next() string {
	if n == nil {
		return ""
	}
	n.n++
	return fmt.Sprintf("[%d] ", n.n)
}

// Text writes one block per entry (§8.1). cwd shortens local paths.
// showSuppressed adds a section per file listing inline-suppressed findings
// with their reasons (spec-rules §5.3). num, when non-nil, numbers findings.
func Text(w io.Writer, r check.Result, failOn finding.Severity, cwd string, showSuppressed bool, num ...*Numberer) {
	var numberer *Numberer
	if len(num) > 0 {
		numberer = num[0]
	}
	TextDedupe(w, r, failOn, cwd, showSuppressed, numberer, nil)
}

// TextDedupe is Text with a cross-entry memory: callee findings already
// printed under an earlier entry are replaced by one line saying so.
func TextDedupe(w io.Writer, r check.Result, failOn finding.Severity, cwd string, showSuppressed bool, numberer *Numberer, d *Dedupe) {
	status := "OK  "
	if check.Failed(r, failOn) {
		status = "FAIL"
	}
	fmt.Fprintf(w, "%s %s\n", status, DisplayPath(r.Entry, cwd))
	for _, f := range r.Files {
		suppressed := f.Suppressed
		if !showSuppressed {
			suppressed = nil
		}
		fresh, repeated := d.Split(r.Entry, f) // seeds the memory for the entry's own findings too
		if len(f.Findings) == 0 && len(suppressed) == 0 {
			continue
		}
		name := DisplayPath(f.Name, cwd)
		if f.Name == r.Entry {
			for _, fd := range f.Findings {
				fmt.Fprintf(w, "  %s%s  %s  %s\n    %s\n", numberer.next(), where(name, fd), fd.Severity, fd.Rule, fd.Message)
			}
			if len(suppressed) > 0 {
				fmt.Fprintf(w, "  suppressed:\n")
				for _, fd := range suppressed {
					fmt.Fprintf(w, "    %s  %s  %s  %s%s\n", where(name, fd), fd.Severity, fd.Rule, fd.Message, reason(fd))
				}
			}
			continue
		}
		if len(fresh) == 0 && len(suppressed) == 0 && len(repeated) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s\n", name)
		for _, fd := range fresh {
			if fd.Line > 0 {
				fmt.Fprintf(w, "    %s%d:%d  %s  %s  %s\n", numberer.next(), fd.Line, fd.Column, fd.Severity, fd.Rule, fd.Message)
			} else {
				fmt.Fprintf(w, "    %s%s  %s  %s\n", numberer.next(), fd.Severity, fd.Rule, fd.Message)
			}
		}
		if len(repeated) > 0 {
			fmt.Fprintf(w, "    (%s reported under %s)\n", plural(len(repeated), "finding"), DisplayPath(repeated[0], cwd))
		}
		if len(suppressed) > 0 {
			fmt.Fprintf(w, "    suppressed:\n")
			for _, fd := range suppressed {
				fmt.Fprintf(w, "      %d:%d  %s  %s  %s%s\n", fd.Line, fd.Column, fd.Severity, fd.Rule, fd.Message, reason(fd))
			}
		}
	}
	fmt.Fprintln(w)
}

func reason(fd finding.Finding) string {
	if fd.Reason == "" {
		return ""
	}
	return "  -- " + fd.Reason
}

func where(name string, fd finding.Finding) string {
	if fd.Line > 0 {
		return fmt.Sprintf("%s:%d:%d", name, fd.Line, fd.Column)
	}
	return name
}

// TextSummary writes the trailing summary line; printed only for more than
// one entry (§8.1).
func TextSummary(w io.Writer, s Summary) {
	if s.Entries <= 1 {
		return
	}
	fmt.Fprintf(w, "%d entries, %d failed, %s, %s",
		s.Entries, s.Failed, plural(s.Errors, "error"), plural(s.Warnings, "warning"))
	if s.Notes > 0 {
		fmt.Fprintf(w, ", %s", plural(s.Notes, "note"))
	}
	fmt.Fprintln(w)
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// jsonFinding is the §8.2 finding shape; severity is a string there.
type jsonFinding struct {
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Message  string `json:"message"`
	Category string `json:"category"`
	Reason   string `json:"reason,omitempty"`
	// ReachedFrom lists every entry that reached a callee finding when
	// more than one did; the finding appears under the first only.
	ReachedFrom []string `json:"reached_from,omitempty"`
}

type jsonEntry struct {
	Path       string        `json:"path"`
	OK         bool          `json:"ok"`
	Findings   []jsonFinding `json:"findings"`
	Suppressed []jsonFinding `json:"suppressed"`
}

func toJSON(fs []finding.Finding) []jsonFinding {
	out := []jsonFinding{}
	for _, f := range fs {
		out = append(out, jsonFinding{
			Severity: f.Severity.String(), Rule: f.Rule, File: f.File,
			Line: f.Line, Column: f.Column, Message: f.Message, Reason: f.Reason,
			Category: rules.CategoryOfID(f.Rule),
		})
	}
	return out
}

type jsonDoc struct {
	Entries []jsonEntry `json:"entries"`
	Summary Summary     `json:"summary"`
}

// JSON writes the whole run as one document (§8.2).
func JSON(w io.Writer, results []check.Result, failOn finding.Severity) error {
	doc := jsonDoc{Entries: []jsonEntry{}, Summary: Summarize(results, failOn)}
	// Which entries reach each callee finding (review 3 N11).
	reached := map[string][]string{}
	for _, r := range results {
		for _, f := range r.Files {
			if f.Name == r.Entry {
				continue
			}
			for _, fd := range f.Findings {
				reached[findingKey(fd)] = append(reached[findingKey(fd)], r.Entry)
			}
		}
	}
	d := NewDedupe()
	Seed(d, results)
	for _, r := range results {
		fresh := Fresh(d, r)
		js := toJSON(fresh)
		for i, fd := range fresh {
			if from := reached[findingKey(fd)]; len(from) > 1 && fd.File != r.Entry {
				js[i].ReachedFrom = from
			}
		}
		e := jsonEntry{
			Path:       r.Entry,
			OK:         !check.Failed(r, failOn),
			Findings:   js,
			Suppressed: toJSON(check.AllSuppressed(r)),
		}
		doc.Entries = append(doc.Entries, e)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}
