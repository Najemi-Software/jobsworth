// Package scripts runs the optional external tools of advanced §5
// (shellcheck, interpreter parse-only checks) and models the
// $GITHUB_OUTPUT writes of python and PowerShell steps (§5.4). Every
// tool is probed once per process; an absent tool silently disables the
// rules that need it.
package scripts

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var probes sync.Map // tool -> string path ("" when absent)

// forks bounds concurrent tool subprocesses: hundreds of parallel cases
// each spawning an interpreter is the fork-stall shape seen under -race
// (test review 5); eight in flight is plenty for a linter.
var forks = make(chan struct{}, 8)

func acquireFork() func() {
	forks <- struct{}{}
	return func() { <-forks }
}

// Path returns the executable for a tool, probing PATH once.
func Path(tool string) (string, bool) {
	if v, ok := probes.Load(tool); ok {
		p := v.(string)
		return p, p != ""
	}
	p, err := exec.LookPath(tool)
	if err != nil {
		p = ""
	}
	probes.Store(tool, p)
	return p, p != ""
}

// Available reports whether a tool is on PATH.
func Available(tool string) bool {
	_, ok := Path(tool)
	return ok
}

// ResetProbes forgets the probe results (tests that change PATH).
func ResetProbes() {
	probes.Range(func(k, _ any) bool {
		probes.Delete(k)
		return true
	})
}

// Finding is one shellcheck result.
type Finding struct {
	Code    int    `json:"code"`
	Level   string `json:"level"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Shellcheck runs shellcheck over a script body on stdin with --format
// json1 and the given dialect (bash or sh). path is the binary.
func Shellcheck(path, body, shell string, extra []string) ([]Finding, error) {
	args := append([]string{"--format=json1", "--shell=" + shell, "-"}, extra...)
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(body)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	release := acquireFork()
	err := cmd.Run()
	release()
	// shellcheck exits 1 when it has findings; only a missing/invalid
	// output is an error.
	var doc struct {
		Comments []Finding `json:"comments"`
	}
	trimmed := bytes.TrimSpace(out.Bytes())
	if len(trimmed) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []Finding
		if jerr := json.Unmarshal(trimmed, &list); jerr != nil {
			return nil, jerr
		}
		return list, nil
	}
	if jerr := json.Unmarshal(trimmed, &doc); jerr != nil {
		return nil, jerr
	}
	return doc.Comments, nil
}

// SyntaxError is a parse failure reported by an interpreter.
type SyntaxError struct {
	Message string
	Line    int
}

var reLineN = regexp.MustCompile(`(?i)line (\d+)`)
var reColonLine = regexp.MustCompile(`:(\d+):`)

// Syntax runs an interpreter's parse-only check over a body. tool is
// python, pwsh, node or ruby; path the binary. ok is false when the body
// does not parse; the error text is the interpreter's.
func Syntax(tool, path, body string) (*SyntaxError, error) {
	var cmd *exec.Cmd
	var tmp string
	switch tool {
	case "python":
		cmd = exec.Command(path, "-c", "import ast, sys\nast.parse(sys.stdin.read())")
		cmd.Stdin = strings.NewReader(body)
	case "pwsh":
		cmd = exec.Command(path, "-NoProfile", "-NonInteractive", "-Command", "$null = [scriptblock]::Create([Console]::In.ReadToEnd())")
		cmd.Stdin = strings.NewReader(body)
	case "node":
		f, err := os.CreateTemp("", "jobsworth-*.js")
		if err != nil {
			return nil, err
		}
		tmp = f.Name()
		_, _ = f.WriteString(body)
		_ = f.Close()
		defer os.Remove(tmp)
		cmd = exec.Command(path, "--check", tmp)
	case "ruby":
		cmd = exec.Command(path, "-c")
		cmd.Stdin = strings.NewReader(body)
	default:
		return nil, nil
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	release := acquireFork()
	err := cmd.Run()
	release()
	if err == nil {
		return nil, nil
	}
	text := strings.TrimSpace(errb.String())
	if text == "" {
		text = strings.TrimSpace(out.String())
	}
	if tmp != "" {
		text = strings.ReplaceAll(text, tmp, filepath.Base(tmp))
	}
	line := 0
	if m := reLineN.FindStringSubmatch(text); m != nil {
		line, _ = strconv.Atoi(m[1])
	} else if m := reColonLine.FindStringSubmatch(text); m != nil {
		line, _ = strconv.Atoi(m[1])
	}
	// Keep the last meaningful line of the interpreter's report.
	lines := strings.Split(text, "\n")
	msg := strings.TrimSpace(lines[len(lines)-1])
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); strings.Contains(t, "Error") || strings.Contains(t, "error") {
			msg = t
			break
		}
	}
	return &SyntaxError{Message: msg, Line: line}, nil
}

// --- §5.4 non-bash $GITHUB_OUTPUT writes -------------------------------------------------

var (
	rePyOpen   = regexp.MustCompile(`with\s+open\(\s*(?:os\.environ\[\s*["']GITHUB_OUTPUT["']\s*\]|os\.getenv\(\s*["']GITHUB_OUTPUT["']\s*\)|os\.environ\.get\(\s*["']GITHUB_OUTPUT["']\s*\))\s*,\s*["']a["']\s*\)\s+as\s+([A-Za-z_][A-Za-z0-9_]*)\s*:`)
	rePyWrite  = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.write\(\s*(f?)(?:"([^"]*)"|'([^']*)')\s*\)`)
	rePyPrint  = regexp.MustCompile(`print\(\s*(f?)(?:"([^"]*)"|'([^']*)')\s*,\s*file\s*=\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	rePyOutput = regexp.MustCompile(`GITHUB_OUTPUT`)
)

// PythonWrites models the recognised python idiom: a `with open(...) as f`
// over GITHUB_OUTPUT followed by f.write("k=v\n") or print("k=v",
// file=f) with literal text. Anything else touching GITHUB_OUTPUT opens
// the target.
func PythonWrites(body string) (writes map[string]string, open bool, reason string) {
	writes = map[string]string{}
	if !rePyOutput.MatchString(body) {
		return writes, true, "python script with no modelled $GITHUB_OUTPUT write"
	}
	handles := map[string]bool{}
	for _, m := range rePyOpen.FindAllStringSubmatch(body, -1) {
		handles[m[1]] = true
	}
	if len(handles) == 0 {
		return writes, true, "GITHUB_OUTPUT used outside the with open(...) as f idiom"
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || rePyOpen.MatchString(t) {
			continue
		}
		var handle, fstr, text string
		if m := rePyWrite.FindStringSubmatch(t); m != nil {
			handle, fstr, text = m[1], m[2], m[3]+m[4]
		} else if m := rePyPrint.FindStringSubmatch(t); m != nil {
			handle, fstr, text = m[4], m[1], m[2]+m[3]
		} else {
			if rePyOutput.MatchString(t) {
				return writes, true, "GITHUB_OUTPUT used in a form the analyser does not model"
			}
			continue
		}
		if !handles[handle] {
			continue
		}
		if fstr == "f" || strings.Contains(text, "{") {
			return writes, true, "f-string or format written to GITHUB_OUTPUT"
		}
		text = strings.ReplaceAll(text, `\n`, "\n")
		for _, kv := range strings.Split(text, "\n") {
			kv = strings.TrimSpace(kv)
			if kv == "" {
				continue
			}
			if eq := strings.IndexByte(kv, '='); eq > 0 {
				writes[kv[:eq]] = kv[eq+1:]
			} else if i := strings.Index(kv, "<<"); i > 0 {
				writes[kv[:i]] = ""
			} else {
				return writes, true, "a line without k=v written to GITHUB_OUTPUT"
			}
		}
	}
	return writes, false, ""
}

var (
	rePsRedirect = regexp.MustCompile(`^(?:"([^"]*)"|'([^']*)')\s*>>\s*\$env:GITHUB_OUTPUT\s*$`)
	rePsAdd      = regexp.MustCompile(`^Add-Content\s+(?:-Path\s+)?\$env:GITHUB_OUTPUT\s+(?:-Value\s+)?(?:"([^"]*)"|'([^']*)')\s*$`)
	rePsOutput   = regexp.MustCompile(`GITHUB_OUTPUT`)
)

// pick returns the quote style and text of whichever alternative matched.
func pick(dq, sq string) (string, string) {
	if dq != "" {
		return `"`, dq
	}
	return "'", sq
}

// PwshWrites models `"k=v" >> $env:GITHUB_OUTPUT` and
// `Add-Content $env:GITHUB_OUTPUT "k=v"`, literal only.
func PwshWrites(body string) (writes map[string]string, open bool, reason string) {
	writes = map[string]string{}
	if !rePsOutput.MatchString(body) {
		return writes, true, "PowerShell script with no modelled $GITHUB_OUTPUT write"
	}
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		var quote, text string
		if m := rePsRedirect.FindStringSubmatch(t); m != nil {
			quote, text = pick(m[1], m[2])
		} else if m := rePsAdd.FindStringSubmatch(t); m != nil {
			quote, text = pick(m[1], m[2])
		} else {
			if rePsOutput.MatchString(t) {
				return writes, true, "GITHUB_OUTPUT used in a form the analyser does not model"
			}
			continue
		}
		if quote == `"` && strings.Contains(text, "$") {
			return writes, true, "an expanded value is written to GITHUB_OUTPUT"
		}
		if eq := strings.IndexByte(text, '='); eq > 0 {
			writes[text[:eq]] = text[eq+1:]
		} else {
			return writes, true, "a line without k=v written to GITHUB_OUTPUT"
		}
	}
	return writes, false, ""
}
