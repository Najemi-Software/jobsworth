package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

var (
	envKeyRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	commonDelims   = map[string]bool{"EOF": true, "END": true, "STOP": true, "EOM": true, "MARKER": true, "_": true}
	reservedPrefix = []string{"GITHUB_", "RUNNER_"}
)

// CheckBash implements medium §2 over one scope. Steps whose shell is not
// bash, or whose script failed to parse, are skipped.
func CheckBash(sc *Scope) []finding.Finding {
	var out []finding.Finding
	for _, st := range sc.Steps {
		a := sc.Bash[st.Index]
		if a == nil || a.ParseErr != nil {
			continue
		}
		// Every bash finding anchors at the run: key (spec-rules §5.2); the
		// script line is named in the message where it matters.
		line := func(int) workflow.Pos { return st.RunPos }
		label := StepLabel(st)

		// §2.1
		for _, d := range a.Deprecated {
			out = append(out, at("bash-deprecated-command", line(d.Line), fmt.Sprintf("step %s emits %s, which the runner ignores since 2023; write to %s instead", label, d.Command, d.Replacement)))
		}
		// §2.2
		if st.ID != "" {
			ts := a.Target(bash.Output)
			for _, kv := range a.Unredirected {
				if _, written := ts.Writes[kv.Key]; written {
					continue
				}
				if sc.Referenced(st.ID, kv.Key) {
					out = append(out, at("bash-output-unredirected", line(kv.Line), fmt.Sprintf("step %s echoes %q without redirecting to $GITHUB_OUTPUT, but steps.%s.outputs.%s is referenced", label, kv.Key+"=...", st.ID, kv.Key)))
				}
			}
		}
		// §2.3
		for _, r := range a.Reads {
			out = append(out, at("bash-output-read", line(r.Line), fmt.Sprintf("step %s reads $GITHUB_OUTPUT (%s); it is append-only and never holds other steps' outputs", label, r.How)))
		}
		// §2.4
		for _, l := range a.Target(bash.Path).Lines {
			if l.Dynamic || l.InHeredoc {
				continue
			}
			if k, _, ok := kvLine(l.Text); ok && envKeyRe.MatchString(k) {
				out = append(out, at("bash-path-kv-line", line(l.Line), fmt.Sprintf("step %s writes %q to $GITHUB_PATH; KEY=VALUE lines belong in $GITHUB_ENV", label, l.Text)))
			}
		}
		for _, l := range a.Target(bash.Env).Lines {
			if l.Dynamic || l.InHeredoc || strings.TrimSpace(l.Text) == "" {
				continue
			}
			if _, _, _, opener := heredocOpenerLine(l.Text); opener {
				continue
			}
			k, _, ok := kvLine(l.Text)
			if !ok {
				out = append(out, at("bash-env-bare-line", line(l.Line), fmt.Sprintf("step %s writes %q to $GITHUB_ENV without a =; paths belong in $GITHUB_PATH", label, l.Text)))
				continue
			}
			if !envKeyRe.MatchString(k) {
				out = append(out, at("bash-env-bad-key", line(l.Line), fmt.Sprintf("step %s writes %q to $GITHUB_ENV; the key must match [A-Za-z_][A-Za-z0-9_]*", label, l.Text)))
				continue
			}
			if isReservedEnv(k) {
				out = append(out, at("bash-env-reserved-key", line(l.Line), fmt.Sprintf("step %s sets %s via $GITHUB_ENV; the runner owns it and ignores the write", label, k)))
			}
		}
		// §2.5
		for _, t := range []bash.Target{bash.Output, bash.Env} {
			for _, h := range a.Target(t).Heredocs {
				switch {
				case h.EmptyDelim:
					out = append(out, at("heredoc-empty-delim", line(h.Line), fmt.Sprintf("step %s writes %s<< with no delimiter to %s", label, h.Key, t)))
					continue
				case h.EscapedNewline:
					out = append(out, at("heredoc-escaped-newline", line(h.Line), fmt.Sprintf("step %s writes %s<<%s with literal \\n in an echo without -e; the value contains backslash-n, not newlines", label, h.Key, h.Delim)))
					continue
				case !h.Terminated:
					out = append(out, at("heredoc-unterminated", line(h.Line), fmt.Sprintf("step %s opens %s<<%s to %s but never writes the %s closing line", label, h.Key, h.Delim, t, h.Delim)))
					continue
				}
				if h.Indented {
					out = append(out, at("heredoc-indented-form", line(h.Line), fmt.Sprintf("step %s uses %s<<-%s; the runner does not strip tabs, use %s<<%s", label, h.Key, h.Delim, h.Key, h.Delim)))
				}
				if commonDelims[h.Delim] && h.OpaqueLines > 0 {
					out = append(out, at("heredoc-delim-collision", line(h.Line), fmt.Sprintf("step %s uses delimiter %s around a value from a command whose output may contain it; use a random delimiter (e.g. $(openssl rand -hex 8))", label, h.Delim)))
				}
			}
		}
		// §2.6
		seen := map[string]bool{}
		for _, u := range a.EnvSameStep {
			if seen[u.Key] {
				continue
			}
			seen[u.Key] = true
			out = append(out, at("bash-env-same-step", line(u.RefLine), fmt.Sprintf("step %s writes %s to $GITHUB_ENV on line %d and reads $%s on line %d; $GITHUB_ENV takes effect from the next step", label, u.Key, u.WriteLine, u.Key, u.RefLine)))
		}
	}
	return out
}

func kvLine(text string) (k, v string, ok bool) {
	eq := strings.IndexByte(text, '=')
	if eq < 0 {
		return "", "", false
	}
	return text[:eq], text[eq+1:], true
}

var openerRe = regexp.MustCompile(`^([^\s=<]+)<<-?(.*)$`)

func heredocOpenerLine(text string) (key, delim string, indented, ok bool) {
	m := openerRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", "", false, false
	}
	return m[1], m[2], strings.Contains(text, "<<-"), true
}

func isReservedEnv(k string) bool {
	if k == "CI" {
		return true
	}
	for _, p := range reservedPrefix {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}
