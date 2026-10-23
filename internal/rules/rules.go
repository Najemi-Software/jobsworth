// Package rules holds the validation rules of spec §7. Every rule is a pure
// function of parsed data: it reads no files and no network. Findings are
// returned with File left empty; the walker fills it in from the file being
// validated.
package rules

import (
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Finding builds a finding for a registered rule from outside the package
// (walk-level rules such as action-output-unused).
func Finding(id string, pos workflow.Pos, msg string) finding.Finding {
	return at(id, pos, msg)
}

// at builds a finding with the registry's default severity. An unregistered
// id is a programming error: it would ship unconfigurable and undocumented.
func at(id string, pos workflow.Pos, msg string) finding.Finding {
	sev, ok := Registry[id]
	if !ok {
		// A derived id of a dynamic umbrella (shellcheck-SC2086).
		r, _, known := Lookup(id)
		if !known {
			panic("rules: finding with unregistered rule id " + id)
		}
		sev = r.Severity
	}
	return finding.Finding{
		Severity: sev,
		Rule:     id,
		Line:     pos.Line,
		Column:   pos.Column,
		Message:  msg,
	}
}
