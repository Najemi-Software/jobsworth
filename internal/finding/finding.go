// Package finding holds the severity scale and the Finding record every rule
// produces (spec §1.4). It is data only: no I/O, no formatting.
package finding

import (
	"fmt"
	"sort"
	"strings"
)

// Severity orders findings from most to least serious. Zero value is Error
// so an uninitialised severity never silently passes a --fail-on threshold.
type Severity int

const (
	Error Severity = iota
	Warning
	Note
)

// MarshalJSON renders the severity by name.
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON accepts the name (or a legacy number).
func (s *Severity) UnmarshalJSON(data []byte) error {
	t := strings.Trim(string(data), `"`)
	if v, ok := ParseSeverity(t); ok {
		*s = v
		return nil
	}
	switch t {
	case "0":
		*s = Error
	case "1":
		*s = Warning
	case "2":
		*s = Note
	default:
		return fmt.Errorf("unknown severity %q", t)
	}
	return nil
}

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Note:
		return "note"
	}
	return fmt.Sprintf("severity(%d)", int(s))
}

// ParseSeverity maps the CLI/config spelling to a Severity.
func ParseSeverity(s string) (Severity, bool) {
	switch s {
	case "error":
		return Error, true
	case "warning":
		return Warning, true
	case "note":
		return Note, true
	}
	return 0, false
}

// Finding is one reported problem. File is the absolute path of a local file
// or the display name (owner/repo/path@ref) of a fetched one. Line/Column are
// 1-based; zero means unknown.
type Finding struct {
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"`
	Column   int      `json:"column,omitempty"`
	Message  string   `json:"message"`
	// Reason is the text after " -- " in the suppression that hid this
	// finding; set only on suppressed findings.
	Reason string `json:"reason,omitempty"`
}

// AtOrAbove reports whether the finding's severity reaches threshold.
func AtOrAbove(f Finding, threshold Severity) bool {
	return f.Severity <= threshold
}

// Sort orders findings by severity, then line, then column, then rule, then
// message — a total order so output never depends on rule execution order.
func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
}

// Count tallies findings per severity.
type Count struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Notes    int `json:"notes"`
}

// Tally counts findings by severity.
func Tally(fs []Finding) Count {
	var c Count
	for _, f := range fs {
		switch f.Severity {
		case Error:
			c.Errors++
		case Warning:
			c.Warnings++
		case Note:
			c.Notes++
		}
	}
	return c
}
