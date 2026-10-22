package testkit

import (
	"fmt"
	"strings"

	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Builders (test spec §3.3): unit tests that need a parsed structure
// without writing YAML by hand. Each builder renders YAML and parses it
// with the real parser, so positions, expressions and uses: are exactly
// what a file would give; the model packages carry no test-only code.
//
//	step := testkit.Step().ID("meta").Run(`echo "k=v" >> $GITHUB_OUTPUT`).Build()
//	wf := testkit.Workflow().Job("build", testkit.Job().Outputs("v", "${{ steps.meta.outputs.k }}")).Build()

// StepBuilder builds one step.
type StepBuilder struct {
	id, name, run, uses, shell, ifv string
	with, env                       [][2]string
}

// Step starts a step.
func Step() *StepBuilder { return &StepBuilder{} }

func (b *StepBuilder) ID(id string) *StepBuilder      { b.id = id; return b }
func (b *StepBuilder) Name(n string) *StepBuilder     { b.name = n; return b }
func (b *StepBuilder) Run(script string) *StepBuilder { b.run = script; return b }
func (b *StepBuilder) Uses(ref string) *StepBuilder   { b.uses = ref; return b }
func (b *StepBuilder) Shell(s string) *StepBuilder    { b.shell = s; return b }
func (b *StepBuilder) If(cond string) *StepBuilder    { b.ifv = cond; return b }
func (b *StepBuilder) With(k, v string) *StepBuilder {
	b.with = append(b.with, [2]string{k, v})
	return b
}
func (b *StepBuilder) Env(k, v string) *StepBuilder { b.env = append(b.env, [2]string{k, v}); return b }

// YAML renders the step as a sequence item at the given indent.
func (b *StepBuilder) YAML(indent string) string {
	var sb strings.Builder
	first := true
	line := func(k, v string) {
		prefix := indent + "  "
		if first {
			prefix = indent + "- "
			first = false
		}
		sb.WriteString(prefix + k + ": " + v + "\n")
	}
	if b.id != "" {
		line("id", b.id)
	}
	if b.name != "" {
		line("name", quote(b.name))
	}
	if b.ifv != "" {
		line("if", quote(b.ifv))
	}
	if b.uses != "" {
		line("uses", b.uses)
	}
	if len(b.with) > 0 {
		line("with", "")
		for _, kv := range b.with {
			sb.WriteString(indent + "    " + kv[0] + ": " + quote(kv[1]) + "\n")
		}
	}
	if len(b.env) > 0 {
		line("env", "")
		for _, kv := range b.env {
			sb.WriteString(indent + "    " + kv[0] + ": " + quote(kv[1]) + "\n")
		}
	}
	if b.shell != "" {
		line("shell", b.shell)
	}
	if b.run != "" {
		line("run", "|")
		for _, l := range strings.Split(strings.TrimRight(b.run, "\n"), "\n") {
			sb.WriteString(indent + "    " + l + "\n")
		}
	}
	if first {
		sb.WriteString(indent + "- run: echo ok\n")
	}
	return sb.String()
}

// Build parses a one-step workflow and returns the step.
func (b *StepBuilder) Build() workflow.Step {
	f := Workflow().Job("build", Job().Steps(b)).Build()
	return workflow.StepsOf(f, "build")[0]
}

// JobBuilder builds one job.
type JobBuilder struct {
	runsOn, ifv, uses string
	needs             []string
	outputs, with     [][2]string
	env               [][2]string
	steps             []*StepBuilder
	permissions       string
}

// Job starts a job on ubuntu-latest.
func Job() *JobBuilder { return &JobBuilder{runsOn: "ubuntu-latest"} }

func (b *JobBuilder) RunsOn(r string) *JobBuilder      { b.runsOn = r; return b }
func (b *JobBuilder) If(cond string) *JobBuilder       { b.ifv = cond; return b }
func (b *JobBuilder) Needs(jobs ...string) *JobBuilder { b.needs = append(b.needs, jobs...); return b }
func (b *JobBuilder) Outputs(k, v string) *JobBuilder {
	b.outputs = append(b.outputs, [2]string{k, v})
	return b
}
func (b *JobBuilder) Env(k, v string) *JobBuilder { b.env = append(b.env, [2]string{k, v}); return b }
func (b *JobBuilder) Steps(steps ...*StepBuilder) *JobBuilder {
	b.steps = append(b.steps, steps...)
	return b
}
func (b *JobBuilder) Permissions(p string) *JobBuilder { b.permissions = p; return b }

// Uses makes the job a reusable-workflow call with the given with: pairs.
func (b *JobBuilder) Uses(ref string, with ...[2]string) *JobBuilder {
	b.uses = ref
	b.with = append(b.with, with...)
	return b
}

// YAML renders the job body under jobs.<name>.
func (b *JobBuilder) YAML() string {
	var sb strings.Builder
	if len(b.needs) > 0 {
		sb.WriteString("    needs: [" + strings.Join(b.needs, ", ") + "]\n")
	}
	if b.ifv != "" {
		sb.WriteString("    if: " + quote(b.ifv) + "\n")
	}
	if b.permissions != "" {
		sb.WriteString("    permissions: " + b.permissions + "\n")
	}
	if b.uses != "" {
		sb.WriteString("    uses: " + b.uses + "\n")
		if len(b.with) > 0 {
			sb.WriteString("    with:\n")
			for _, kv := range b.with {
				sb.WriteString("      " + kv[0] + ": " + quote(kv[1]) + "\n")
			}
		}
		return sb.String()
	}
	sb.WriteString("    runs-on: " + b.runsOn + "\n")
	if len(b.env) > 0 {
		sb.WriteString("    env:\n")
		for _, kv := range b.env {
			sb.WriteString("      " + kv[0] + ": " + quote(kv[1]) + "\n")
		}
	}
	if len(b.outputs) > 0 {
		sb.WriteString("    outputs:\n")
		for _, kv := range b.outputs {
			sb.WriteString("      " + kv[0] + ": " + quote(kv[1]) + "\n")
		}
	}
	sb.WriteString("    steps:\n")
	if len(b.steps) == 0 {
		sb.WriteString("      - run: echo ok\n")
	}
	for _, st := range b.steps {
		sb.WriteString(st.YAML("      "))
	}
	return sb.String()
}

// WorkflowBuilder builds a workflow file.
type WorkflowBuilder struct {
	name, on, permissions string
	jobs                  []namedJob
	inputs                []inputDecl
	secrets               []string
	outputs               [][2]string
}

type namedJob struct {
	name string
	job  *JobBuilder
}

type inputDecl struct {
	trigger, name, typ, def string
	required                bool
	options                 []string
}

// Workflow starts a workflow named T on push with read-all permissions.
func Workflow() *WorkflowBuilder {
	return &WorkflowBuilder{name: "T", on: "push", permissions: "read-all"}
}

func (b *WorkflowBuilder) Name(n string) *WorkflowBuilder        { b.name = n; return b }
func (b *WorkflowBuilder) Permissions(p string) *WorkflowBuilder { b.permissions = p; return b }

// On sets the triggers as written after `on:` (a scalar, flow list or
// block the builder emits verbatim on one line).
func (b *WorkflowBuilder) On(events string) *WorkflowBuilder { b.on = events; return b }

// Job adds a job.
func (b *WorkflowBuilder) Job(name string, job *JobBuilder) *WorkflowBuilder {
	b.jobs = append(b.jobs, namedJob{name, job})
	return b
}

// Input declares an input under workflow_call (the trigger becomes
// workflow_call when any input or secret is declared).
func (b *WorkflowBuilder) Input(name, typ, def string, required bool) *WorkflowBuilder {
	b.inputs = append(b.inputs, inputDecl{trigger: "workflow_call", name: name, typ: typ, def: def, required: required})
	return b
}

// DispatchInput declares a workflow_dispatch input; options apply to choice.
func (b *WorkflowBuilder) DispatchInput(name, typ, def string, options ...string) *WorkflowBuilder {
	b.inputs = append(b.inputs, inputDecl{trigger: "workflow_dispatch", name: name, typ: typ, def: def, options: options})
	return b
}

// Secret declares a required workflow_call secret.
func (b *WorkflowBuilder) Secret(name string) *WorkflowBuilder {
	b.secrets = append(b.secrets, name)
	return b
}

// Output declares a workflow_call output.
func (b *WorkflowBuilder) Output(name, value string) *WorkflowBuilder {
	b.outputs = append(b.outputs, [2]string{name, value})
	return b
}

// YAML renders the workflow.
func (b *WorkflowBuilder) YAML() string {
	var sb strings.Builder
	sb.WriteString("name: " + quote(b.name) + "\n")
	byTrigger := map[string][]inputDecl{}
	for _, in := range b.inputs {
		byTrigger[in.trigger] = append(byTrigger[in.trigger], in)
	}
	if len(b.inputs) == 0 && len(b.secrets) == 0 && len(b.outputs) == 0 {
		sb.WriteString("on: " + b.on + "\n")
	} else {
		sb.WriteString("on:\n")
		triggers := []string{"workflow_call"}
		if len(byTrigger["workflow_dispatch"]) > 0 {
			triggers = append(triggers, "workflow_dispatch")
		}
		for _, tr := range triggers {
			sb.WriteString("  " + tr + ":\n")
			if ins := byTrigger[tr]; len(ins) > 0 {
				sb.WriteString("    inputs:\n")
				for _, in := range ins {
					sb.WriteString("      " + in.name + ":\n")
					if in.typ != "" {
						sb.WriteString("        type: " + in.typ + "\n")
					}
					if in.def != "" {
						sb.WriteString("        default: " + quote(in.def) + "\n")
					}
					if in.required {
						sb.WriteString("        required: true\n")
					}
					if len(in.options) > 0 {
						sb.WriteString("        options: [" + strings.Join(in.options, ", ") + "]\n")
					}
				}
			}
			if tr == "workflow_call" {
				if len(b.secrets) > 0 {
					sb.WriteString("    secrets:\n")
					for _, s := range b.secrets {
						sb.WriteString("      " + s + ":\n        required: true\n")
					}
				}
				if len(b.outputs) > 0 {
					sb.WriteString("    outputs:\n")
					for _, o := range b.outputs {
						sb.WriteString("      " + o[0] + ":\n        value: " + quote(o[1]) + "\n")
					}
				}
			}
		}
	}
	if b.permissions != "" {
		sb.WriteString("permissions: " + b.permissions + "\n")
	}
	sb.WriteString("jobs:\n")
	if len(b.jobs) == 0 {
		b.jobs = []namedJob{{"build", Job()}}
	}
	for _, j := range b.jobs {
		sb.WriteString("  " + j.name + ":\n")
		sb.WriteString(j.job.YAML())
	}
	return sb.String()
}

// Build parses the rendered workflow; a parse failure is a builder bug
// and panics with the YAML.
func (b *WorkflowBuilder) Build() *workflow.File {
	return mustParse(b.YAML())
}

// ActionBuilder builds a composite action file.
type ActionBuilder struct {
	name    string
	inputs  []inputDecl
	outputs [][2]string
	steps   []*StepBuilder
}

// Action starts a composite action.
func Action() *ActionBuilder { return &ActionBuilder{name: "A"} }

func (b *ActionBuilder) Name(n string) *ActionBuilder { b.name = n; return b }

// Input declares an input with an optional default ("" for none).
func (b *ActionBuilder) Input(name, def string, required bool) *ActionBuilder {
	b.inputs = append(b.inputs, inputDecl{name: name, def: def, required: required})
	return b
}

// Output declares an output with a value expression.
func (b *ActionBuilder) Output(name, value string) *ActionBuilder {
	b.outputs = append(b.outputs, [2]string{name, value})
	return b
}

// Steps adds composite steps; a run: step defaults to shell: bash.
func (b *ActionBuilder) Steps(steps ...*StepBuilder) *ActionBuilder {
	for _, st := range steps {
		if st.run != "" && st.shell == "" {
			st.shell = "bash"
		}
	}
	b.steps = append(b.steps, steps...)
	return b
}

// YAML renders the action.
func (b *ActionBuilder) YAML() string {
	var sb strings.Builder
	sb.WriteString("name: " + quote(b.name) + "\ndescription: Example\n")
	if len(b.inputs) > 0 {
		sb.WriteString("inputs:\n")
		for _, in := range b.inputs {
			sb.WriteString("  " + in.name + ":\n    description: " + in.name + "\n")
			if in.def != "" {
				sb.WriteString("    default: " + quote(in.def) + "\n")
			}
			if in.required {
				sb.WriteString("    required: true\n")
			}
		}
	}
	if len(b.outputs) > 0 {
		sb.WriteString("outputs:\n")
		for _, o := range b.outputs {
			sb.WriteString("  " + o[0] + ":\n    description: " + o[0] + "\n    value: " + quote(o[1]) + "\n")
		}
	}
	sb.WriteString("runs:\n  using: composite\n  steps:\n")
	if len(b.steps) == 0 {
		sb.WriteString("    - shell: bash\n      run: echo ok\n")
	}
	for _, st := range b.steps {
		sb.WriteString(st.YAML("    "))
	}
	return sb.String()
}

// Build parses the rendered action.
func (b *ActionBuilder) Build() *workflow.File { return mustParse(b.YAML()) }

func mustParse(src string) *workflow.File {
	f, err := workflow.Parse([]byte(src))
	if err != nil {
		panic(fmt.Sprintf("testkit builder produced YAML that does not parse: %v\n%s", err, src))
	}
	return f
}

// quote renders a scalar so that expressions, colons and leading
// characters survive the YAML parser; plain words stay plain.
func quote(v string) string {
	if v == "" {
		return "''"
	}
	if strings.ContainsAny(v, ":#{}[]&*!|>'\"%@`\n") || strings.HasPrefix(v, "-") || strings.HasPrefix(v, "?") || strings.TrimSpace(v) != v {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
	}
	switch strings.ToLower(v) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return "'" + v + "'"
	}
	return v
}
