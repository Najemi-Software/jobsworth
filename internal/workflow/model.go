package workflow

// Pos is a 1-based line/column in the source file. Zero means unknown.
type Pos struct {
	Line   int
	Column int
}

// Input is one declared input (§2, §7.1). Trigger is "workflow_call" or
// "workflow_dispatch" for workflows and "" for actions.
type Input struct {
	Name    string
	Pos     Pos
	Trigger string
	// Type is the declared type: string, number, boolean, choice,
	// environment — or "" when undeclared (actions, or omitted).
	Type     string
	Required bool
	// HasDefault distinguishes `default: ""` from no default at all.
	HasDefault bool
	Default    Scalar
	Options    []string
	// HasDescription is true when a description: key is present.
	HasDescription bool
}

// Scalar is a YAML scalar value with its resolved tag, so rules can tell
// `true` (!!bool) from `"true"` (!!str).
type Scalar struct {
	Value string
	// Tag is the short tag: !!str, !!bool, !!int, !!float, !!null; "" when
	// the value was not a scalar at all.
	Tag string
	Pos Pos
}

// IsExpr reports whether the scalar contains a ${{ }} expression.
func (s Scalar) IsExpr() bool {
	return containsExpr(s.Value)
}

// Output is a file-level output: on.workflow_call.outputs.<x> for workflows,
// outputs.<x> for actions. Value is the `value:` expression (workflows only).
type Output struct {
	Name  string
	Pos   Pos
	Value string
}

// JobOutput is one key of jobs.<j>.outputs.
type JobOutput struct {
	Name  string
	Pos   Pos
	Value string
}

// Need is one entry of jobs.<j>.needs.
type Need struct {
	Name string
	Pos  Pos
}

// With is one key of a `with:` block.
type With struct {
	Name  string
	Value Scalar
}

// Uses is one `uses:` occurrence. Job is the containing job name ("" inside
// an action). JobLevel is true for jobs.<j>.uses (a reusable workflow call).
type Uses struct {
	Raw      string
	Pos      Pos
	Job      string
	JobLevel bool
	// Comment is the trailing line comment, without the #, trimmed.
	Comment string
}

// Step is one entry of a steps: sequence (medium §0.1). Index is its
// position in the scope, 0-based.
type Step struct {
	Index int
	Pos   Pos
	ID    string
	IDPos Pos
	Name  Scalar
	// Uses/Run: at most one is normally set; both set is step-run-and-uses.
	Uses    string
	UsesPos Pos
	HasRun  bool
	Run     string
	RunPos  Pos
	// RunBlock is true when run: is a block scalar (| or >): the script's
	// first line is then the line after RunPos.
	RunBlock bool
	// Shell is the step's own shell: ("" when inherited).
	Shell string
	With  []With
	// HasWith is true when a with: block exists, even if empty.
	HasWith bool
	WithPos Pos
	Env     []With
	If      Scalar
	// WorkingDirectory is the literal working-directory:, if any.
	WorkingDirectory string
	// IsNull marks a null entry in the steps list (advanced §3.2).
	IsNull bool
	// ContinueOnError is the continue-on-error: value when present.
	ContinueOnError *Scalar
	// TimeoutMinutes is true when timeout-minutes: is set on the step.
	TimeoutMinutes bool
}

// Secret is one on.workflow_call.secrets declaration.
type Secret struct {
	Name     string
	Pos      Pos
	Required bool
}

// Job is one entry of jobs:.
type Job struct {
	Name    string
	Pos     Pos
	Needs   []Need
	Outputs []JobOutput
	// Uses is set for a reusable-workflow job; With are its inputs.
	Uses *Uses
	With []With
	// Steps are the job's steps (empty for uses: jobs).
	Steps []Step
	// DefaultShell is jobs.<j>.defaults.run.shell.
	DefaultShell string
	// DisplayName is the job's name: value.
	DisplayName Scalar
	Env         []With
	// Secrets is the secrets: block of a uses: job. SecretsInherit is
	// `secrets: inherit`; SecretsExpr is an expression-valued block.
	Secrets        []With
	SecretsInherit bool
	SecretsExpr    bool
	HasSecrets     bool
	SecretsPos     Pos
	If             Scalar
	// Matrix is the raw strategy.matrix node, for shape inference.
	Matrix *MatrixSpec
	// Advanced-spec keys.
	Permissions *Permissions
	RunsOn      *RunsOn
	Concurrency *Concurrency
	Strategy    *Strategy
	// HasSteps is true when a steps: key exists; StepsEmpty when it is an
	// empty list.
	HasSteps       bool
	StepsEmpty     bool
	TimeoutMinutes bool
	Environment    *Scalar
	// Container and Services are the raw text of those blocks ("" when
	// absent), for job-equivalence comparisons.
	Container string
	Services  string
	// Keys lists every key of the job mapping, in order.
	Keys []string
}

// MatrixSpec is strategy.matrix in a shape-friendly form: literal axes and
// include rows, or an expression.
type MatrixSpec struct {
	Pos Pos
	// Axes maps axis name to its literal values (scalars rendered as
	// Scalar; non-scalar elements are kept as Any via Tag "").
	Axes map[string][]Scalar
	// Include rows: each row maps field name to a scalar.
	Include []map[string]Scalar
	// Exclude rows (advanced §7.3).
	Exclude []map[string]Scalar
	// Objects maps an axis whose elements are mappings to those rows
	// (matrix of objects, advanced §7.3).
	Objects map[string][]map[string]Scalar
	// Empty lists the axes declared with an empty list.
	Empty []string
	// Expr is set when matrix: is itself an expression.
	Expr string
	// IncludeExpr is set when include: (or any axis) is an expression; the
	// matrix then has fields the file does not name.
	IncludeExpr string
}

// Expr is one expression found in the file: the inside of a ${{ }} or the
// whole value of an `if:`. Job is the containing job ("" outside any job).
// Step is the containing step's index, -1 outside any step. InOutputs marks
// expressions in a job's outputs: mapping or an action's outputs.*.value,
// which run after every step (medium §1.1). Key is the nearest mapping key
// the scalar sits under (if, run, name, with, env, value, ...).
type Expr struct {
	Text      string
	Pos       Pos
	Job       string
	Step      int
	InOutputs bool
	Key       string
	// IsIf marks a bare if: value treated as an expression.
	IsIf bool
	// Raw is the whole scalar the expression was found in.
	Raw string
	// Section is the top-level key the expression sits under (on, env,
	// jobs, concurrency, run-name, name, permissions, defaults, runs).
	// Parent is the key of the mapping that holds the scalar ("<job>" or
	// "<step>" for a job or step mapping, "" at the root).
	Section string
	Parent  string
}

// File is the parsed model of one workflow or action file.
type File struct {
	Kind Kind
	// Inputs/Outputs are the file-level declarations per the §2 table.
	Inputs  []Input
	Outputs []Output
	// Jobs is in declaration order (workflows only).
	Jobs []Job
	// Uses lists every uses: in the file, job-level and step-level, in
	// document order.
	Uses []Uses
	// Exprs lists every expression in the file in document order.
	Exprs []Expr
	// Triggers: which of the input-bearing triggers the workflow declares.
	HasWorkflowCall     bool
	HasWorkflowDispatch bool
	// Events lists every trigger name under on: (medium §5.1).
	Events []string
	// Secrets are on.workflow_call.secrets declarations.
	Secrets []Secret
	// DefaultShell is the workflow-level defaults.run.shell.
	DefaultShell string
	// Steps are a composite action's runs.steps.
	Steps []Step
	// Using is runs.using as written (actions only): composite, node20,
	// docker, ...
	Using string
	// RunName is the workflow's run-name:.
	RunName Scalar
	Env     []With
	// Name is the workflow/action name:.
	Name Scalar
	// Description is an action's description:.
	Description Scalar
	// Advanced-spec blocks.
	On          *On
	Permissions *Permissions
	Concurrency *Concurrency
	Crons       []Cron
	// Keys lists the top-level keys in order.
	Keys []string
}

// StepsOf returns the steps of the scope named job ("" for an action).
func StepsOf(f *File, job string) []Step {
	if job == "" {
		return f.Steps
	}
	if j := JobByName(f, job); j != nil {
		return j.Steps
	}
	return nil
}

// EffectiveShell resolves a step's shell per medium §0.2: step, job
// default, workflow default, then "bash" for workflows and "" (missing)
// for composite actions.
func EffectiveShell(f *File, job string, s Step) string {
	if s.Shell != "" {
		return s.Shell
	}
	if job != "" {
		if j := JobByName(f, job); j != nil && j.DefaultShell != "" {
			return j.DefaultShell
		}
		if f.DefaultShell != "" {
			return f.DefaultShell
		}
		return "bash"
	}
	return ""
}

// JobByName finds a job; nil if absent.
func JobByName(f *File, name string) *Job {
	for i := range f.Jobs {
		if f.Jobs[i].Name == name {
			return &f.Jobs[i]
		}
	}
	return nil
}

// InputNames returns the declared input names in declaration order, deduped.
func InputNames(f *File) []string {
	seen := map[string]bool{}
	var out []string
	for _, in := range f.Inputs {
		if !seen[in.Name] {
			seen[in.Name] = true
			out = append(out, in.Name)
		}
	}
	return out
}

// WorkflowCallInputs returns only the on.workflow_call.inputs declarations
// (the ones a caller's with: is checked against, §7.2).
func WorkflowCallInputs(f *File) []Input {
	var out []Input
	for _, in := range f.Inputs {
		if in.Trigger == "workflow_call" {
			out = append(out, in)
		}
	}
	return out
}
