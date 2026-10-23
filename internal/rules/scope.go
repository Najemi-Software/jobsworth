package rules

import (
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/scripts"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Scope is one job body or composite action body (medium §0.1): its steps,
// the bash analysis of every bash step, every steps.* reference in it, and
// the declarations of the actions its steps use.
type Scope struct {
	File *workflow.File
	// Job is the job name, "" for a composite action.
	Job   string
	Steps []workflow.Step
	// Shell is the effective shell per step index ("" for a composite step
	// with no shell:).
	Shell map[int]string
	// Bash holds the analysis for every step whose shell is bash/sh; a
	// step with a parse error has an entry with ParseErr set.
	Bash map[int]*bash.Analysis
	// Refs are the steps.* references in the scope with their expression.
	Refs []ScopeRef
	// Callees maps a step index to its action's declarations, when known.
	Callees     map[int]*workflow.File
	CalleeNames map[int]string
	calleeKeys  map[int]string
	byID        map[string]int
}

// ScopeRef is one steps.* reference and where it sits.
type ScopeRef struct {
	workflow.StepRef
	Expr workflow.Expr
}

// Callee records the declarations of the action a step uses. Key is an
// opaque identity the walker uses to track which outputs callers consume.
type Callee struct {
	Step int
	File *workflow.File
	Name string
	Key  string
}

// ConsumedOutputs returns, per callee key, the outputs this scope
// references through steps.<id>.outputs.<o> (medium §1.6).
func (sc *Scope) ConsumedOutputs() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	keyOf := map[int]string{}
	for i := range sc.Callees {
		keyOf[i] = sc.CalleeNames[i]
	}
	for _, r := range sc.Refs {
		if r.Field != "outputs" {
			continue
		}
		st, ok := sc.StepByID(r.ID)
		if !ok {
			continue
		}
		key, has := sc.calleeKeys[st.Index]
		if !has {
			continue
		}
		if out[key] == nil {
			out[key] = map[string]bool{}
		}
		out[key][r.Output] = true
	}
	return out
}

// BuildScope assembles a scope. callees are the loaded actions of its
// uses: steps; opaque extends the heredoc opaque-command list.
func BuildScope(f *workflow.File, job string, callees []Callee, opaque []string) *Scope {
	return BuildScopeWith(f, job, callees, bash.Options{Opaque: opaque})
}

// BuildScopeWith is BuildScope with the full analyser options.
func BuildScopeWith(f *workflow.File, job string, callees []Callee, bo bash.Options) *Scope {
	sc := &Scope{
		File: f, Job: job, Steps: workflow.StepsOf(f, job),
		Shell: map[int]string{}, Bash: map[int]*bash.Analysis{},
		Callees: map[int]*workflow.File{}, CalleeNames: map[int]string{}, calleeKeys: map[int]string{}, byID: map[string]int{},
	}
	for _, c := range callees {
		sc.Callees[c.Step] = c.File
		sc.CalleeNames[c.Step] = c.Name
		sc.calleeKeys[c.Step] = c.Key
	}
	// Cross-step variables (advanced §7.1): literal env: entries and the
	// $GITHUB_ENV writes of earlier steps seed each step's variable table.
	seeds := map[string]string{}
	addEnv := func(ws []workflow.With) {
		for _, w := range ws {
			if !w.Value.IsExpr() {
				seeds[w.Name] = w.Value.Value
			}
		}
	}
	addEnv(f.Env)
	if job != "" {
		if j := workflow.JobByName(f, job); j != nil {
			addEnv(j.Env)
		}
	}
	for _, st := range sc.Steps {
		if st.ID != "" {
			if _, dup := sc.byID[st.ID]; !dup {
				sc.byID[st.ID] = st.Index
			}
		}
		shell := EffectiveShellFor(f, job, st)
		sc.Shell[st.Index] = shell
		if st.HasRun && !IsBashShell(shell) {
			// Python and PowerShell $GITHUB_OUTPUT writes are modelled
			// minimally (advanced §5.4) so the step rules see them.
			switch ShellKind(shell) {
			case "python":
				w, open, reason := scripts.PythonWrites(st.Run)
				sc.Bash[st.Index] = bash.NewSynthetic(w, open, reason)
			case "pwsh", "powershell":
				w, open, reason := scripts.PwshWrites(st.Run)
				sc.Bash[st.Index] = bash.NewSynthetic(w, open, reason)
			}
		}
		if st.HasRun && IsBashShell(shell) {
			stepOpts := bo
			stepOpts.Seeds = map[string]string{}
			for k, v := range seeds {
				stepOpts.Seeds[k] = v
			}
			for _, w := range st.Env {
				if !w.Value.IsExpr() {
					stepOpts.Seeds[w.Name] = w.Value.Value
				}
			}
			a := bash.AnalyseWith(st.Run, isPosix(shell), stepOpts)
			sc.Bash[st.Index] = a
			if a.ParseErr == nil {
				for k, w := range a.Target(bash.Env).Writes {
					if !w.ValueDynamic && w.Definite {
						seeds[k] = w.Value
					} else {
						delete(seeds, k)
					}
				}
			}
		}
	}
	for _, e := range f.Exprs {
		if e.Job != job {
			continue
		}
		for _, r := range workflow.StepRefs(e.Text) {
			sc.Refs = append(sc.Refs, ScopeRef{StepRef: r, Expr: e})
		}
	}
	return sc
}

// IsBashShell reports whether a shell value is parsed as bash (§0.2).
func IsBashShell(shell string) bool {
	k := ShellKind(shell)
	return k == "bash" || k == "sh"
}

func isPosix(shell string) bool {
	return ShellKind(shell) == "sh"
}

// ShellKind classifies a shell: value by its first word (advanced §5.1):
// bash, sh, pwsh, powershell, python, node, ruby, cmd, ... A custom
// template (`bash --noprofile {0}`, `python {0}`) is classified by the
// interpreter it names.
func ShellKind(shell string) string {
	s := strings.ToLower(strings.TrimSpace(shell))
	if s == "" {
		return ""
	}
	first := strings.Fields(s)[0]
	first = first[strings.LastIndex(first, "/")+1:]
	switch first {
	case "python3", "python2":
		return "python"
	case "pwsh.exe", "powershell.exe":
		return strings.TrimSuffix(first, ".exe")
	}
	return first
}

// EffectiveShellFor is workflow.EffectiveShell plus the advanced §5.1
// defaults: a job whose every runs-on label is a literal windows-* gets
// pwsh when nothing names a shell.
func EffectiveShellFor(f *workflow.File, job string, st workflow.Step) string {
	shell := workflow.EffectiveShell(f, job, st)
	if st.Shell != "" || job == "" {
		return shell
	}
	j := workflow.JobByName(f, job)
	if j == nil || j.DefaultShell != "" || f.DefaultShell != "" || j.RunsOn == nil || len(j.RunsOn.Labels) == 0 {
		return shell
	}
	for _, l := range j.RunsOn.Labels {
		if l.IsExpr() || !strings.HasPrefix(strings.ToLower(l.Value), "windows-") {
			return shell
		}
	}
	return "pwsh"
}

// StepByID returns the step with the id, if any.
func (sc *Scope) StepByID(id string) (workflow.Step, bool) {
	i, ok := sc.byID[id]
	if !ok {
		return workflow.Step{}, false
	}
	return sc.Steps[i], true
}

// IDs lists the declared ids in step order.
func (sc *Scope) IDs() []string {
	var out []string
	for _, st := range sc.Steps {
		if st.ID != "" {
			out = append(out, st.ID)
		}
	}
	return out
}

// Referenced reports whether anything in the scope references
// steps.<id>.outputs.<output> (any output when output is "").
func (sc *Scope) Referenced(id, output string) bool {
	for _, r := range sc.Refs {
		if r.ID == id && (output == "" || (r.Field == "outputs" && r.Output == output)) {
			return true
		}
	}
	return false
}

// StepLabel renders a step for messages: its name, else its uses, else
// its index.
func StepLabel(st workflow.Step) string {
	switch {
	case st.Name.Value != "":
		return "'" + st.Name.Value + "'"
	case st.ID != "":
		return "'" + st.ID + "'"
	case st.Uses != "":
		return "'" + st.Uses + "'"
	}
	return "#" + strconv.Itoa(st.Index+1)
}
