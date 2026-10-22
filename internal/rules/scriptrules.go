package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/scripts"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family scripts (advanced §5): shellcheck and parse-only checks for
// other interpreters, each an optional external tool.

// ScriptInputs is the scripts configuration (advanced §5.2, §5.3).
type ScriptInputs struct {
	ShellcheckEnabled     string // auto | true | false
	ShellcheckPath        string
	ShellcheckMinSeverity finding.Severity
	ShellcheckExtraArgs   []string
	Python                string // auto | true | false
	Pwsh                  string
}

var reShellcheckID = regexp.MustCompile(`^shellcheck-SC(\d+)$`)

// ShellcheckRule synthesises the per-code rule record for a shellcheck
// finding id (advanced §5.2: one id per code so each can be configured).
func ShellcheckRule(id string) (Rule, bool) {
	m := reShellcheckID.FindStringSubmatch(id)
	if m == nil {
		return Rule{}, false
	}
	r, _, _ := Lookup("shellcheck-codes")
	r.ID = id
	r.Summary = "shellcheck SC" + m[1]
	r.Dynamic = false
	r.Aliases = nil
	r.Off = m[1] == "1090" || m[1] == "1091" // sourcing: off by default
	return r, true
}

// toolPath resolves a tool per its enabled setting: "" when off or absent.
func toolPath(tool, enabled, explicit string) (string, bool) {
	if enabled == "false" {
		return "", false
	}
	if explicit != "" {
		return explicit, true
	}
	return scripts.Path(tool)
}

// CheckScripts implements §5.2 and §5.3 over one file's scopes.
func CheckScripts(f *workflow.File, scopes map[string]*Scope, in ScriptInputs) []finding.Finding {
	var out []finding.Finding
	sc, scOK := toolPath("shellcheck", in.ShellcheckEnabled, in.ShellcheckPath)
	if in.ShellcheckEnabled == "true" && !scOK {
		out = append(out, at("shellcheck-codes", workflow.Pos{Line: 1, Column: 1}, "scripts.shellcheck.enabled is true but no shellcheck binary was found"))
	}
	py, pyOK := toolPath("python3", in.Python, "")
	if !pyOK {
		py, pyOK = toolPath("python", in.Python, "")
	}
	ps, psOK := toolPath("pwsh", in.Pwsh, "")
	node, nodeOK := scripts.Path("node")
	ruby, rubyOK := scripts.Path("ruby")
	for _, scope := range scopes {
		for _, st := range scope.Steps {
			if !st.HasRun || st.Run == "" {
				continue
			}
			kind := ShellKind(scope.Shell[st.Index])
			body := ReplaceSentinel(st.Run)
			switch kind {
			case "bash", "sh":
				if !scOK {
					continue
				}
				fs, err := scripts.Shellcheck(sc, body, kind, in.ShellcheckExtraArgs)
				if err != nil {
					continue
				}
				for _, sf := range fs {
					sev := finding.Note
					switch sf.Level {
					case "error":
						sev = finding.Error
					case "warning":
						sev = finding.Warning
					}
					if sev > in.ShellcheckMinSeverity {
						continue
					}
					fd := at("shellcheck-SC"+strconv.Itoa(sf.Code), st.RunPos, fmt.Sprintf("step %s line %d: SC%d %s", StepLabel(st), absScriptLine(st, sf.Line), sf.Code, sf.Message))
					fd.Severity = sev
					out = append(out, fd)
				}
			case "python":
				if pyOK {
					out = append(out, syntaxFinding("python-syntax", "python", py, st, body)...)
				}
			case "pwsh", "powershell":
				if psOK {
					out = append(out, syntaxFinding("pwsh-syntax", "pwsh", ps, st, body)...)
				}
			case "node":
				if nodeOK {
					out = append(out, syntaxFinding("node-syntax", "node", node, st, body)...)
				}
			case "ruby":
				if rubyOK {
					out = append(out, syntaxFinding("ruby-syntax", "ruby", ruby, st, body)...)
				}
			}
		}
	}
	return out
}

func syntaxFinding(id, tool, path string, st workflow.Step, body string) []finding.Finding {
	se, err := scripts.Syntax(tool, path, body)
	if err != nil || se == nil {
		return nil
	}
	line := ""
	if se.Line > 0 {
		line = fmt.Sprintf(" (line %d)", absScriptLine(st, se.Line))
	}
	return []finding.Finding{at(id, st.RunPos, fmt.Sprintf("step %s: %s does not parse the script%s: %s", StepLabel(st), tool, line, se.Message))}
}

// ReplaceSentinel substitutes ${{ }} expressions with a placeholder a
// script parser accepts.
func ReplaceSentinel(body string) string {
	var b strings.Builder
	for {
		i := strings.Index(body, "${{")
		if i < 0 {
			b.WriteString(body)
			return b.String()
		}
		b.WriteString(body[:i])
		rest := body[i+3:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			b.WriteString("__GHA_EXPR__")
			return b.String()
		}
		b.WriteString("__GHA_EXPR__")
		body = rest[j+2:]
	}
}
