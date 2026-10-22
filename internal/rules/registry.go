package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/finding"
)

// Status is a rule's lifecycle state (spec-rules §3.1).
type Status string

const (
	Stable       Status = "stable"
	Experimental Status = "experimental"
	Deprecated   Status = "deprecated"
)

// Rule is one registry record (spec-rules §3.1). Severity is the default;
// Off means the rule ships disabled. Doc is loaded from doc/<id>.md.
type Rule struct {
	ID      string
	Family  string
	Summary string
	Since   string
	Status  Status
	// Replacement names the successor of a deprecated rule.
	Replacement string
	// Category is the orthogonal classification of advanced §10:
	// correctness (the default when empty), security, hygiene,
	// performance. Enrich marks a rule that only fires under
	// `check --enrich` (advanced §11).
	Category string
	Aliases  []string
	Related  []string
	Severity finding.Severity
	Off      bool
	Fixable  bool
	Enrich   bool
	// Dynamic marks an umbrella whose findings carry derived ids
	// (shellcheck -> shellcheck-SC2086); the umbrella itself never fires.
	Dynamic bool
}

// Categories in display order (advanced §10).
var Categories = []string{"correctness", "security", "hygiene", "performance"}

// CategoryOf returns a rule's category, correctness when unset.
func CategoryOf(r Rule) string {
	if r.Category == "" {
		return "correctness"
	}
	return r.Category
}

// CategoryOfID is CategoryOf by id; correctness for unknown ids.
func CategoryOfID(id string) string {
	if r, _, ok := Lookup(id); ok {
		return CategoryOf(r)
	}
	return "correctness"
}

// IDPattern is the id scheme (spec-rules §3.2).
var IDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Families in display order (spec-rules §3.3). Families that only hold
// rules from later specs are listed once those rules exist.
var Families = []string{"inputs", "calls", "jobs", "steps", "bash", "checkout", "secrets", "values", "shapes", "fetch", "structure", "suppress",
	"expressions", "triggers", "security", "conditions", "reachability", "performance", "scripts"}

// Rules is the ordered registry: by family, then spec order within the
// family.
var Rules = []Rule{
	// inputs — basic §7.1
	{ID: "input-undeclared", Family: "inputs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Input is referenced but not declared", Related: []string{"input-unused"}},
	{ID: "input-unused", Family: "inputs", Severity: finding.Warning, Since: "0.1", Status: Stable,
		Summary: "Input is declared but never referenced", Related: []string{"input-undeclared"}},
	{ID: "input-default-type", Family: "inputs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Input default is not representable in its declared type", Related: []string{"call-input-type", "choice-default-not-option", "if-not-boolean"}},
	{ID: "choice-default-not-option", Family: "inputs", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Choice input's default is not one of its options", Related: []string{"choice-no-options", "input-default-type"}},
	{ID: "choice-no-options", Family: "inputs", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Choice input declares no options", Related: []string{"choice-default-not-option"}},

	// calls — basic §7.2
	{ID: "call-input-unknown", Family: "calls", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Reusable workflow call supplies an input the callee does not declare", Related: []string{"call-input-missing", "call-input-type"}},
	{ID: "call-input-missing", Family: "calls", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Reusable workflow call omits a required input", Related: []string{"call-input-unknown"}},
	{ID: "call-input-type", Family: "calls", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Reusable workflow call passes a literal of the wrong type", Related: []string{"input-default-type", "call-input-type-decl"}},

	// calls — medium §1.4
	{ID: "step-input-unknown", Family: "calls", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step supplies an input the action does not declare", Related: []string{"step-input-missing", "uses-docker-unpinned"}},
	{ID: "step-input-missing", Family: "calls", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step omits a required action input", Related: []string{"step-input-unknown"}},
	{ID: "step-input-type", Family: "calls", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step passes a literal of the wrong type to a typed input", Related: []string{"step-input-choice"}},
	{ID: "step-input-choice", Family: "calls", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step passes a literal outside a choice input's options", Related: []string{"step-input-type"}},
	{ID: "step-with-on-run", Family: "calls", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Run step has a with block, which the runner ignores", Related: []string{"step-run-and-uses"}},
	{ID: "step-run-and-uses", Family: "calls", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step has both run and uses", Related: []string{"step-with-on-run", "checkout-trailing"}},

	// jobs — basic §7.3, §7.4
	{ID: "needs-unknown", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Job needs a job that does not exist", Related: []string{"needs-self", "needs-cycle"}},
	{ID: "needs-self", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Job lists itself in needs", Related: []string{"needs-cycle"}},
	{ID: "needs-cycle", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Needs edges form a cycle", Related: []string{"needs-self"}},
	{ID: "needs-output-job-unknown", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Expression references outputs of a job that does not exist", Related: []string{"needs-output-not-needed", "needs-output-undeclared"}},
	{ID: "needs-output-not-needed", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Job references outputs of a job it does not need", Related: []string{"needs-output-undeclared"}},
	{ID: "needs-output-undeclared", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Job references an output the needed job does not declare", Related: []string{"output-unused", "jobs-mergeable"}},
	{ID: "call-output-job-unknown", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Workflow output references a job that does not exist", Related: []string{"call-output-undeclared"}},
	{ID: "call-output-undeclared", Family: "jobs", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Workflow output references a job output that is not declared", Related: []string{"call-output-job-unknown"}},
	{ID: "output-unused", Family: "jobs", Severity: finding.Warning, Since: "0.1", Status: Stable,
		Summary: "Job output is declared but nothing references it", Related: []string{"needs-output-undeclared"}},

	// steps — medium §0.2, §1.1–1.3, §1.5
	{ID: "step-id-unknown", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step output reference names an id no step has", Related: []string{"step-forward-ref", "step-output-unused"}},
	{ID: "step-id-duplicate", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Two steps in one scope share an id", Related: []string{"step-id-invalid"}},
	{ID: "step-id-invalid", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step id has characters outside [A-Za-z0-9_-] or starts with a digit", Related: []string{"step-id-duplicate", "step-output-unused"}},
	{ID: "step-forward-ref", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step references a step declared later in the scope", Related: []string{"step-id-unknown"}},
	{ID: "step-output-unwritten", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step output is referenced but never written", Related: []string{"bash-output-unredirected", "step-output-conditional"}},
	{ID: "step-output-conditional", Family: "steps", Severity: finding.Note, Off: true, Since: "0.2", Status: Stable,
		Summary: "Step output is only written on some paths", Related: []string{"step-output-unwritten"}},
	{ID: "step-output-action-undeclared", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step output reference names an output the action does not declare", Related: []string{"step-output-unwritten", "uses-docker-unpinned"}},
	{ID: "step-output-not-run", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Step output reference points at a step with neither run nor uses", Related: []string{"step-run-and-uses", "step-no-action"}},
	{ID: "step-output-unused", Family: "steps", Severity: finding.Note, Since: "0.2", Status: Stable,
		Summary: "Step writes an output nothing references", Related: []string{"step-id-unreferenced"}},
	{ID: "step-id-unreferenced", Family: "steps", Severity: finding.Note, Off: true, Since: "0.2", Status: Stable,
		Summary: "Step has an id nothing references", Related: []string{"step-output-unused"}},
	{ID: "action-output-unlifted", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Composite action output references a step output that does not exist", Related: []string{"step-output-unwritten", "step-output-action-undeclared", "bash-output-unredirected", "action-output-unused"}},
	{ID: "action-output-unused", Family: "steps", Severity: finding.Note, Since: "0.2", Status: Stable,
		Summary: "Composite action declares an output no local caller consumes", Related: []string{"output-unused"}},
	{ID: "composite-shell-missing", Family: "steps", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Composite run step has no shell", Related: []string{"bash-syntax"}},
	// bash — medium §0.2, §2
	{ID: "bash-syntax", Family: "bash", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Run script does not parse as bash", Related: []string{"composite-shell-missing"}},
	{ID: "bash-deprecated-command", Family: "bash", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Script emits a workflow command the runner dropped in 2023", Related: []string{"bash-output-unredirected", "step-output-unwritten"}},
	{ID: "bash-output-unredirected", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Echo of a referenced output key goes to the log, not $GITHUB_OUTPUT", Related: []string{"step-output-unwritten"}},
	{ID: "bash-output-read", Family: "bash", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Script reads $GITHUB_OUTPUT, which never holds other steps' state", Related: []string{"bash-env-same-step"}},
	{ID: "bash-path-kv-line", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "KEY=VALUE line written to $GITHUB_PATH", Related: []string{"bash-env-bare-line"}},
	{ID: "bash-env-bare-line", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Line without = written to $GITHUB_ENV", Related: []string{"bash-path-kv-line", "bash-env-bad-key"}},
	{ID: "bash-env-bad-key", Family: "bash", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "$GITHUB_ENV line has an empty or invalid key", Related: []string{"bash-env-bare-line"}},
	{ID: "bash-env-reserved-key", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "$GITHUB_ENV sets a key the runner owns", Related: []string{"bash-env-bad-key"}},
	{ID: "bash-env-same-step", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Script reads a variable it just wrote to $GITHUB_ENV", Related: []string{"bash-output-read"}},
	{ID: "heredoc-unterminated", Family: "bash", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "KEY<<DELIM opener has no matching DELIM line", Related: []string{"heredoc-empty-delim"}},
	{ID: "heredoc-empty-delim", Family: "bash", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "KEY<< with no delimiter", Related: []string{"heredoc-unterminated"}},
	{ID: "heredoc-escaped-newline", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Multi-line value written with literal \\n in an echo without -e", Related: []string{"heredoc-unterminated"}},
	{ID: "heredoc-delim-collision", Family: "bash", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Common delimiter around a value from an opaque command", Related: []string{"heredoc-unterminated"}},
	{ID: "heredoc-indented-form", Family: "bash", Severity: finding.Note, Off: true, Since: "0.2", Status: Stable,
		Summary: "KEY<<-DELIM form, which the runner does not treat specially", Related: []string{"heredoc-unterminated"}},

	// checkout — medium §3
	{ID: "local-action-no-checkout", Family: "checkout", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Local action used before any checkout in the job", Related: []string{"local-action-after-subdir-only"}},
	{ID: "local-action-wrong-repo", Family: "checkout", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Local action will be loaded from a different repository's checkout", Related: []string{"local-action-no-checkout"}},
	{ID: "local-action-sparse-miss", Family: "checkout", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Sparse checkout does not cover the local action's path", Related: []string{"local-action-no-checkout"}},
	{ID: "local-action-after-subdir-only", Family: "checkout", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Only subdirectory checkouts precede a local action", Related: []string{"local-action-no-checkout"}},
	{ID: "checkout-ref-unresolvable", Family: "checkout", Severity: finding.Note, Off: true, Since: "0.2", Status: Stable,
		Summary: "Checkout ref is an expression and cannot be verified", Related: []string{"local-action-wrong-repo", "checkout-trailing"}},

	// secrets — medium §4
	{ID: "call-secret-unknown", Family: "secrets", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Reusable workflow call supplies a secret the callee does not declare", Related: []string{"call-secret-missing"}},
	{ID: "call-secret-missing", Family: "secrets", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Reusable workflow call omits a required secret", Related: []string{"call-secret-unknown"}},
	{ID: "call-secret-literal", Family: "secrets", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Secret passed as a bare literal in source", Related: []string{"call-secret-unknown"}},
	{ID: "call-secret-inherit-partial", Family: "secrets", Severity: finding.Note, Off: true, Since: "0.2", Status: Stable,
		Summary: "secrets: inherit on a callee that declares no secrets", Related: []string{"call-secret-missing"}},
	{ID: "secret-undeclared", Family: "secrets", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Reusable workflow references a secret it does not declare", Related: []string{"secret-unused"}},
	{ID: "secret-unused", Family: "secrets", Severity: finding.Note, Since: "0.2", Status: Stable,
		Summary: "Declared secret is never referenced", Related: []string{"secret-undeclared"}},
	{ID: "secret-in-action", Family: "secrets", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Composite action references the secrets context, which does not exist there", Related: []string{"secret-undeclared"}},
	{ID: "secret-github-token-declared", Family: "secrets", Severity: finding.Note, Since: "0.2", Status: Stable,
		Summary: "GITHUB_TOKEN declared as a secret input", Related: []string{"secret-unused"}},
	{ID: "secret-to-output", Family: "secrets", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Output value is a secret, which consumers receive empty", Related: []string{"secret-in-name"}},
	{ID: "secret-in-name", Family: "secrets", Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "Secret interpolated into a name, which is logged in clear", Related: []string{"secret-to-output"}},

	{ID: "secret-unknown-in-scope", Family: "secrets", Severity: finding.Warning, Since: "0.4", Status: Stable, Enrich: true,
		Summary: "secrets.X referenced but no repository or organisation secret has that name", Related: []string{"secret-undeclared"}},

	// values — medium §5.1, §5.4
	{ID: "value-set-mismatch", Family: "values", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Comparison against a literal outside the path's documented value set", Related: []string{"if-always-true-string"}},
	{ID: "if-always-true-string", Family: "values", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "if: is a plain string, which is always truthy", Related: []string{"if-double-wrapped"}},
	{ID: "if-double-wrapped", Family: "values", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "if: joins two expression blocks into a string", Related: []string{"if-always-true-string"}},
	{ID: "if-status-with-other", Family: "values", Aliases: []string{"if-always-with-and"}, Severity: finding.Warning, Since: "0.2", Status: Stable,
		Summary: "if: combines always() with other conditions", Related: []string{"if-double-wrapped"}},

	// shapes — medium §6
	{ID: "shape-mismatch", Family: "shapes", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "Member access that cannot succeed on the value's inferred shape", Related: []string{"fromjson-non-string"}},
	{ID: "fromjson-invalid-literal", Family: "shapes", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "fromJSON of a string literal that is not valid JSON", Related: []string{"fromjson-non-string"}},
	{ID: "fromjson-non-string", Family: "shapes", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "fromJSON applied to a value that is already an object or array", Related: []string{"shape-mismatch"}},
	{ID: "jq-syntax", Family: "shapes", Severity: finding.Error, Since: "0.2", Status: Stable,
		Summary: "jq filter in a run script does not parse", Related: []string{"shape-mismatch"}},
	{ID: "shape-file-missing", Family: "shapes", Severity: finding.Note, Off: true, Since: "0.2", Status: Stable,
		Summary: "Script reads a literal JSON or YAML path that does not exist in the repo", Related: []string{"shape-mismatch"}},

	{ID: "matrix-empty", Family: "shapes", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Matrix axis with an empty list", Related: []string{"matrix-exclude-no-match", "shape-mismatch"}},
	{ID: "matrix-exclude-no-match", Family: "shapes", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Matrix exclude entry that matches no combination", Related: []string{"matrix-empty"}},
	{ID: "matrix-strategy-type", Family: "shapes", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "strategy.max-parallel or fail-fast of the wrong type", Related: []string{"matrix-empty"}},

	// fetch — basic §3.1, §5.6
	{ID: "fetch-failed", Family: "fetch", Severity: finding.Warning, Since: "0.1", Status: Stable,
		Summary: "Callee could not be fetched from GitHub", Related: []string{"fetch-not-found", "fetch-offline"},
		Aliases: []string{"uses-fetch-failed"}},
	{ID: "fetch-not-found", Family: "fetch", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Referenced repo, ref, or file does not exist on GitHub", Related: []string{"fetch-failed", "uses-not-found"},
		Aliases: []string{"uses-not-action"}},
	{ID: "fetch-offline", Family: "fetch", Severity: finding.Warning, Since: "0.1", Status: Stable,
		Summary: "Callee is not cached and the run is offline", Related: []string{"fetch-failed"}},
	{ID: "uses-no-ref", Family: "fetch", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "External uses reference has no @ref", Related: []string{"uses-invalid", "uses-docker-unpinned"}},
	{ID: "uses-invalid", Family: "fetch", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Uses reference is malformed or names the wrong kind of callee", Related: []string{"uses-no-ref"}},
	{ID: "uses-dynamic", Family: "fetch", Severity: finding.Note, Since: "0.1", Status: Stable,
		Summary: "Uses reference contains an expression and cannot be followed"},
	{ID: "uses-not-found", Family: "fetch", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Local or linked uses reference does not exist on disk", Related: []string{"fetch-not-found"}},
	{ID: "callee-unparseable", Family: "fetch", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Callee is not valid YAML or is neither a workflow nor an action", Related: []string{"file-invalid"}},

	// structure — basic §1.1, §2
	{ID: "file-invalid", Family: "structure", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Entry file is unreadable, not valid YAML, or neither a workflow nor an action", Related: []string{"callee-unparseable"}},

	// structure — advanced §3
	{ID: "runs-on-unknown", Family: "structure", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "runs-on label that no GitHub-hosted or configured runner has", Related: []string{"runs-on-retired", "runs-on-matrix"}},
	{ID: "runs-on-retired", Family: "structure", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "runs-on label GitHub has retired", Related: []string{"runs-on-unknown"}},
	{ID: "runs-on-matrix", Family: "structure", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "runs-on: ${{ matrix.os }} with an unknown or retired label in the axis", Related: []string{"runs-on-unknown", "runs-on-retired"}},
	{ID: "runs-on-missing", Family: "structure", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Non-uses job without runs-on:", Related: []string{"job-no-steps"}},
	{ID: "runs-on-group-and-labels", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "runs-on group with no labels:", Related: []string{}},
	{ID: "job-empty-steps", Family: "structure", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "steps: [] runs a job that does nothing", Related: []string{"job-no-steps"}},
	{ID: "job-no-steps", Family: "structure", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Job with neither steps: nor uses:", Related: []string{"job-empty-steps", "runs-on-missing"}},
	{ID: "step-null", Family: "structure", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "null entry in a steps list", Related: []string{"step-no-action"}},
	{ID: "step-no-action", Family: "structure", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Step with neither run: nor uses:", Related: []string{"step-null", "step-output-not-run"}},
	{ID: "job-orphan-outputs", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Job outputs that nothing needs or lifts", Related: []string{"output-unused", "needs-output-not-needed"}},
	{ID: "job-unreachable", Family: "structure", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Job whose needs chain passes through an if: false job", Related: []string{"if-literal-false"}},
	{ID: "job-timeout-missing", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "Job without timeout-minutes:", Related: []string{}},
	{ID: "step-continue-on-error-outputs", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable,
		Summary: "continue-on-error step whose outputs are consumed without an outcome check", Related: []string{"step-output-conditional"}},
	{ID: "workflow-name-missing", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Workflow without name:", Related: []string{"workflow-name-duplicate"}},
	{ID: "workflow-name-duplicate", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Two workflows in the repo share a name:", Related: []string{"workflow-name-missing"}},
	{ID: "action-name-missing", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Action without name:", Related: []string{"action-description-missing"}},
	{ID: "action-description-missing", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Action without description:", Related: []string{"action-name-missing"}},
	{ID: "input-description-missing", Family: "structure", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "Input without description:", Related: []string{}},
	{ID: "name-expression", Family: "structure", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "name: with an expression, which is not evaluated there", Related: []string{}},

	{ID: "environment-unknown", Family: "structure", Severity: finding.Error, Since: "0.4", Status: Stable, Enrich: true,
		Summary: "environment: names an environment the repository does not have", Related: []string{"environment-no-protection"}},
	{ID: "runs-on-group-unknown", Family: "structure", Severity: finding.Error, Since: "0.4", Status: Stable, Enrich: true,
		Summary: "runs-on group the organisation does not have", Related: []string{"runs-on-unknown"}},

	// suppress — spec-rules §5.4
	{ID: "suppress-wildcard", Family: "suppress", Severity: finding.Note, Since: "0.1", Status: Stable,
		Summary: "Suppression uses * and hides future rules too", Related: []string{"suppress-unused"}},
	{ID: "suppress-unclosed", Family: "suppress", Severity: finding.Warning, Since: "0.1", Status: Stable,
		Summary: "jobsworth-ignore-start has no matching jobsworth-ignore-end"},
	{ID: "suppress-unknown-rule", Family: "suppress", Severity: finding.Error, Since: "0.1", Status: Stable,
		Summary: "Suppression names a rule id that does not exist", Related: []string{"suppress-unused", "jobs-mergeable"}},
	{ID: "suppress-unused", Family: "suppress", Severity: finding.Note, Since: "0.1", Status: Stable,
		Summary: "Suppression suppressed nothing", Related: []string{"suppress-wildcard", "jobs-mergeable"}},

	// expressions — advanced §6
	{ID: "expr-unknown-function", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Call to a function GitHub Actions expressions do not have", Related: []string{"expr-arity"}},
	{ID: "expr-arity", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Expression function called with the wrong number of arguments", Related: []string{"expr-unknown-function", "expr-format-placeholders"}},
	{ID: "expr-format-placeholders", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "format() placeholders and arguments do not line up", Related: []string{"expr-arity"}},
	{ID: "expr-contains-types", Family: "expressions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "contains() applied to an object", Related: []string{"shape-mismatch"}},
	{ID: "expr-hashfiles-absolute", Family: "expressions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "hashFiles() with a path outside the workspace", Related: []string{}},
	{ID: "expr-string-escape", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "String literal is double-quoted or has an unescaped quote", Related: []string{}},
	{ID: "expr-comparison-type", Family: "expressions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Comparison between a number or boolean and a string that can never match", Related: []string{"value-set-mismatch"}},
	{ID: "expr-env-in-if-job", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "env.* used in a job-level if:, where the env context is empty", Related: []string{"expr-context-unavailable"}},
	{ID: "expr-secrets-in-if", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "secrets.* used in an if:, where the secrets context is unavailable", Related: []string{"expr-context-unavailable"}},
	{ID: "expr-context-unavailable", Family: "expressions", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "A context used where GitHub does not provide it", Related: []string{"expr-env-in-if-job", "expr-secrets-in-if"}},

	// triggers — advanced §2
	{ID: "on-missing", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Workflow has no on: block", Related: []string{"on-unknown-event"}},
	{ID: "on-unknown-event", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "on: names an event GitHub does not have", Related: []string{"on-unknown-key", "on-unknown-type"}},
	{ID: "on-unknown-key", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Event child key the event does not accept", Related: []string{"on-unknown-event"}},
	{ID: "on-unknown-type", Family: "triggers", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "types: entry that is not an activity type of the event", Related: []string{"on-unknown-event"}},
	{ID: "on-filter-conflict", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Both a filter and its -ignore form on one event", Related: []string{"on-filter-shape"}},
	{ID: "on-filter-shape", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Filter value is not a string or list of strings", Related: []string{"on-filter-conflict", "on-filter-empty"}},
	{ID: "on-filter-expression", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "Expression inside an on: filter", Related: []string{"on-filter-glob"}},
	{ID: "on-filter-glob", Family: "triggers", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Filter pattern with an unbalanced [ or trailing backslash", Related: []string{"on-filter-expression"}},
	{ID: "on-filter-empty", Family: "triggers", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Empty branches:/paths: list matches nothing", Related: []string{"on-filter-shape"}},
	{ID: "on-workflow-run-target", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "workflow_run without workflows:", Related: []string{"on-workflow-run-unknown"}},
	{ID: "on-workflow-run-unknown", Family: "triggers", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "workflow_run names a workflow no local file declares", Related: []string{"on-workflow-run-target"}},
	{ID: "on-push-tags-only", Family: "triggers", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "push with tags: and no branches: no longer triggers on branch pushes", Related: []string{}},
	{ID: "cron-syntax", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "schedule cron expression does not parse", Related: []string{"cron-too-frequent", "cron-impossible-date"}},
	{ID: "cron-too-frequent", Family: "triggers", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Schedule fires more often than every 5 minutes", Related: []string{"cron-syntax"}},
	{ID: "cron-impossible-date", Family: "triggers", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Schedule names a day no listed month has", Related: []string{"cron-syntax"}},
	{ID: "cron-quoted", Family: "triggers", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Cron string not quoted in YAML", Related: []string{"cron-syntax"}},
	{ID: "dispatch-input-type", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "workflow_dispatch input type is not one GitHub supports", Related: []string{"call-input-type-decl"}},
	{ID: "call-input-type-decl", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "workflow_call input type is not string, boolean or number", Related: []string{"dispatch-input-type"}},
	{ID: "dispatch-input-limit", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "More than 25 workflow_dispatch inputs", Related: []string{}},
	{ID: "input-no-trigger", Family: "triggers", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "inputs.* referenced in a workflow with no input-bearing trigger", Related: []string{"input-undeclared"}},
	{ID: "dispatch-input-event-spelling", Family: "triggers", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "github.event.inputs.X where inputs.X would do", Related: []string{}},
	{ID: "concurrency-group-static", Family: "triggers", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "Concurrency group without an expression serialises every run", Related: []string{"concurrency-cancel-on-push-main"}},
	{ID: "concurrency-cancel-on-push-main", Family: "triggers", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true,
		Summary: "cancel-in-progress on a group keyed on github.ref in a deploying workflow", Related: []string{"concurrency-group-static"}},

	{ID: "on-filter-no-branch", Family: "triggers", Severity: finding.Note, Since: "0.4", Status: Stable, Enrich: true, Category: "hygiene",
		Summary: "A literal branches: entry that does not exist", Related: []string{"on-filter-glob"}},

	// security — advanced §1
	{ID: "injection-run", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Attacker-controlled context interpolated into a run: script", Related: []string{"injection-script", "injection-via-input"}},
	{ID: "injection-script", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Attacker-controlled context interpolated into github-script code", Related: []string{"injection-run"}},
	{ID: "injection-via-input", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Untrusted context passed to an input the callee interpolates into a sink", Related: []string{"injection-run"}},
	{ID: "prt-untrusted-checkout", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Privileged trigger checks out the pull request head", Related: []string{"prt-untrusted-checkout-and-run", "prt-no-permissions", "checkout-unused"}},
	{ID: "prt-untrusted-checkout-and-run", Family: "security", Severity: finding.Error, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Privileged trigger checks out the pull request head and runs code from it", Related: []string{"prt-untrusted-checkout"}},
	{ID: "prt-no-permissions", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Privileged-trigger workflow without a permissions: block", Related: []string{"permissions-missing"}},
	{ID: "uses-unpinned", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "security",
		Summary: "External action from an untrusted owner not pinned to a commit SHA", Related: []string{"uses-unpinned-no-comment", "uses-docker-unpinned"}},
	{ID: "uses-unpinned-no-comment", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "SHA-pinned action without a version comment", Related: []string{"uses-unpinned", "checkout-trailing"}},
	{ID: "uses-docker-unpinned", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "security",
		Summary: "docker:// image without a digest", Related: []string{"uses-unpinned"}},
	{ID: "permissions-write-all", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "permissions: write-all", Related: []string{"permissions-missing"}},
	{ID: "permissions-missing", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "No permissions: block; the token gets the repository default", Related: []string{"permissions-write-all", "prt-no-permissions"}},
	{ID: "permissions-unknown-scope", Family: "security", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "permissions: key that is not a documented scope", Related: []string{"permissions-unknown-level"}},
	{ID: "permissions-unknown-level", Family: "security", Severity: finding.Error, Since: "0.4", Status: Stable,
		Summary: "permissions: value other than read, write or none", Related: []string{"permissions-unknown-scope"}},
	{ID: "permissions-id-token-unused", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "security",
		Summary: "id-token: write granted and nothing consumes it", Related: []string{"permissions-write-all"}},
	{ID: "checkout-persist-credentials", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "security",
		Summary: "Checkout keeps the token and a later step exposes the tree", Related: []string{"secret-to-artifact", "artifact-whole-workspace"}},
	{ID: "secret-to-artifact", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "A secret is written to a file that an artifact upload covers", Related: []string{"secret-echo"}},
	{ID: "secret-echo", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "security",
		Summary: "A secret is echoed", Related: []string{"secret-to-artifact"}},
	{ID: "self-hosted-pr-trigger", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Self-hosted runner on an unrestricted pull_request trigger", Related: []string{}},
	{ID: "cache-poisoning-path", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Category: "security",
		Summary: "Cache key built from an untrusted path on a privileged trigger", Related: []string{"prt-untrusted-checkout"}},

	{ID: "environment-no-protection", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Enrich: true, Category: "security",
		Summary: "Deployment environment without protection on a privileged trigger", Related: []string{"environment-unknown"}},
	{ID: "uses-archived", Family: "security", Severity: finding.Warning, Since: "0.4", Status: Stable, Enrich: true, Category: "security",
		Summary: "The action's repository is archived", Related: []string{"uses-deprecated-tag", "uses-unpinned"}},
	{ID: "uses-deprecated-tag", Family: "security", Severity: finding.Note, Since: "0.4", Status: Stable, Enrich: true, Category: "hygiene",
		Summary: "The resolved tag is more than one major behind the latest release", Related: []string{"uses-archived", "uses-unpinned"}},

	// conditions — advanced §4
	{ID: "if-not-boolean", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "if: whose value is a string or number, not a boolean", Related: []string{"if-string-compare-bool", "if-always-true-string"}},
	{ID: "if-string-compare-bool", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "A string-valued step output compared with a boolean literal", Related: []string{"if-not-boolean"}},
	{ID: "if-always-false", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "if: that is false for every value", Related: []string{"if-literal-false", "if-status-contradiction"}},
	{ID: "if-literal-false", Family: "conditions", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "hygiene",
		Summary: "if: false, the deliberate disablement", Related: []string{"if-always-false", "job-unreachable"}},
	{ID: "if-status-contradiction", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "Status functions that cannot hold together", Related: []string{"if-always-false"}},
	{ID: "if-redundant-success", Family: "conditions", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "hygiene",
		Summary: "if: success() alone, which is the default", Related: []string{}},
	{ID: "if-dead-for-all-callers", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "if: false for every value any known caller passes", Related: []string{"if-dead-for-caller"}},
	{ID: "if-dead-for-caller", Family: "conditions", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true,
		Summary: "if: false for one caller's values", Related: []string{"if-dead-for-all-callers"}},
	{ID: "expr-or-default-boolean", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "boolean input || default drops false", Related: []string{"expr-and-or-ternary-falsy"}},
	{ID: "expr-and-or-ternary-falsy", Family: "conditions", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "cond && x || y where x can be falsy", Related: []string{"expr-or-default-boolean"}},

	// reachability — advanced §8
	{ID: "step-unreachable", Family: "reachability", Severity: finding.Note, Since: "0.4", Status: Stable,
		Summary: "Step whose if: is always false while its outputs are consumed", Related: []string{"if-always-false"}},
	{ID: "job-needs-skipped", Family: "reachability", Severity: finding.Note, Since: "0.4", Status: Stable,
		Summary: "Job silently skipped when a conditional dependency skips", Related: []string{"job-unreachable"}},
	{ID: "output-behind-condition", Family: "reachability", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true,
		Summary: "Job output from a conditional step consumed without a result check", Related: []string{"step-output-conditional"}},

	// performance — advanced §9
	{ID: "checkout-unused", Family: "performance", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "performance",
		Summary: "Checkout followed only by steps that do not touch the filesystem", Related: []string{"checkout-trailing", "checkout-redundant"}},
	{ID: "checkout-trailing", Family: "performance", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "performance",
		Summary: "Checkout as the last step", Related: []string{"checkout-unused"}},
	{ID: "checkout-redundant", Family: "performance", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "performance",
		Summary: "Second root checkout of the same repository and ref", Related: []string{"checkout-unused"}},
	{ID: "jobs-mergeable", Family: "performance", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "performance",
		Summary: "Two jobs in a chain that could be one", Related: []string{}},
	{ID: "setup-no-cache", Family: "performance", Severity: finding.Note, Since: "0.4", Status: Stable, Off: true, Category: "performance",
		Summary: "setup-* without cache: in a job that installs packages", Related: []string{}},
	{ID: "artifact-whole-workspace", Family: "performance", Severity: finding.Note, Since: "0.4", Status: Stable, Category: "performance",
		Summary: "upload-artifact of the whole workspace", Related: []string{}},

	// scripts — advanced §5
	{ID: "shellcheck-codes", Family: "scripts", Severity: finding.Warning, Since: "0.4", Status: Stable, Dynamic: true,
		Summary: "shellcheck findings, one derived id per code (shellcheck-SC2086)", Related: []string{"bash-syntax"}},
	{ID: "python-syntax", Family: "scripts", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "A shell: python step that python cannot parse", Related: []string{"pwsh-syntax"}},
	{ID: "pwsh-syntax", Family: "scripts", Severity: finding.Warning, Since: "0.4", Status: Stable,
		Summary: "A shell: pwsh step that PowerShell cannot parse", Related: []string{"python-syntax"}},
	{ID: "node-syntax", Family: "scripts", Severity: finding.Warning, Since: "0.4", Status: Stable, Off: true,
		Summary: "A shell: node step that node cannot parse", Related: []string{"ruby-syntax"}},
	{ID: "ruby-syntax", Family: "scripts", Severity: finding.Warning, Since: "0.4", Status: Stable, Off: true,
		Summary: "A shell: ruby step that ruby cannot parse", Related: []string{"node-syntax"}},
}

// Registry maps each canonical id to its default severity. Kept for the rule
// functions' `at` helper and for older call sites; Lookup is the full record.
var Registry = func() map[string]finding.Severity {
	m := make(map[string]finding.Severity, len(Rules))
	for _, r := range Rules {
		m[r.ID] = r.Severity
	}
	return m
}()

var byID = func() map[string]int {
	m := map[string]int{}
	for i, r := range Rules {
		m[r.ID] = i
		for _, a := range r.Aliases {
			m[a] = i
		}
	}
	return m
}()

// Lookup resolves an id or alias to its rule. canonical is the rule's ID,
// which differs from id when an alias was given.
func Lookup(id string) (r Rule, canonical string, ok bool) {
	i, ok := byID[id]
	if !ok {
		if dr, dok := ShellcheckRule(id); dok {
			return dr, id, true
		}
		return Rule{}, "", false
	}
	return Rules[i], Rules[i].ID, true
}

// IsKnown reports whether id is a registered rule, an alias, or a
// derived id of a dynamic umbrella.
func IsKnown(id string) bool {
	if _, ok := byID[id]; ok {
		return true
	}
	_, ok := ShellcheckRule(id)
	return ok
}

// IsDerived reports whether id is a derived id of a dynamic umbrella.
func IsDerived(id string) bool {
	_, static := byID[id]
	if static {
		return false
	}
	_, ok := ShellcheckRule(id)
	return ok
}

// InFamily returns the rules of one family in registry order.
func InFamily(family string) []Rule {
	var out []Rule
	for _, r := range Rules {
		if r.Family == family {
			out = append(out, r)
		}
	}
	return out
}

// Effective computes a rule's effective severity under cfg (spec-rules §6):
// a config override wins; experimental rules are off unless named; else the
// default. off is true when the rule does not run.
func Effective(r Rule, cfg config.Config) (sev finding.Severity, off bool, changed bool) {
	for _, key := range append([]string{r.ID}, r.Aliases...) {
		if o, ok := cfg.Rules[key]; ok {
			if o.Off {
				return r.Severity, true, !r.Off
			}
			return o.Severity, false, r.Off || o.Severity != r.Severity
		}
	}
	if r.Status == Experimental {
		return r.Severity, true, false
	}
	return r.Severity, r.Off, false
}

// ApplyEffective rewrites findings to their effective severity and drops the
// ones whose rule is off. Unregistered ids pass through untouched. A
// deprecated rule that fired adds one note naming its replacement
// (spec-rules §3.2).
func ApplyEffective(cfg config.Config, fs []finding.Finding) []finding.Finding {
	lookup := func(id string) (Rule, bool) {
		r, _, ok := Lookup(id)
		return r, ok
	}
	return applyEffective(cfg, fs, lookup)
}

// ApplyEffectiveWith is ApplyEffective with an injected registry lookup,
// for tests that need a deprecated rule without registering one.
func ApplyEffectiveWith(cfg config.Config, fs []finding.Finding, lookup func(string) (Rule, bool)) []finding.Finding {
	return applyEffective(cfg, fs, lookup)
}

func applyEffective(cfg config.Config, fs []finding.Finding, lookup func(string) (Rule, bool)) []finding.Finding {
	out := fs[:0:0]
	deprecated := map[string]Rule{}
	for _, f := range fs {
		r, ok := lookup(f.Rule)
		if !ok {
			out = append(out, f)
			continue
		}
		sev, off, changed := Effective(r, cfg)
		if off {
			continue
		}
		// A config override wins; otherwise the rule's own choice stands
		// (some rules downgrade individual findings, e.g. value-set-mismatch
		// on github.event_name).
		if changed {
			f.Severity = sev
		}
		out = append(out, f)
		if r.Status == Deprecated {
			deprecated[r.ID] = r
		}
	}
	ids := make([]string, 0, len(deprecated))
	for id := range deprecated {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := deprecated[id]
		out = append(out, finding.Finding{
			Severity: finding.Note, Rule: r.ID,
			Message: fmt.Sprintf("rule %s is deprecated and will be removed; use %s instead", r.ID, r.Replacement),
		})
	}
	return out
}

func init() {
	// Config errors for unknown rule ids suggest the nearest registered id.
	config.NearestRule = func(id string) []string { return Nearest(id, 1) }
}

// Nearest returns up to n registered ids closest to id by edit distance,
// for the "unknown rule" message (spec-rules §2.2).
func Nearest(id string, n int) []string {
	type cand struct {
		id   string
		dist int
	}
	var cands []cand
	for _, r := range Rules {
		cands = append(cands, cand{r.ID, levenshtein(id, r.ID)})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].dist < cands[j].dist })
	var out []string
	for i := 0; i < len(cands) && i < n; i++ {
		out = append(out, cands[i].id)
	}
	return out
}

func levenshtein(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
