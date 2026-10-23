package rules

import (
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

func valueSetRules(f *workflow.File) []finding.Finding { return CheckValueSets(f) }
func ifs(f *workflow.File) []finding.Finding           { return CheckIfs(f) }
func splitLines(s string) []string                     { return strings.Split(s, "\n") }

// Hand-built-input tables for the checkout, secrets, values and bash rule
// functions (test spec §4 rules row, §6.1; test review 1 N2). Each row is
// one branch of the function under test; the fixtures and doc examples
// cover the same rules through the pipeline.

const job = "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n"

func TestCheckCheckoutSubdirsTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		steps string
		self  string
		want  []string
		sub   string
		repo  string // expected context Repository
		ref   string
	}{
		{"no checkout before a local action", "      - uses: ./.github/actions/greet\n", "example/app", []string{"local-action-no-checkout"}, "", "", ""},
		{"root checkout then local action", "      - uses: actions/checkout@v4\n      - uses: ./.github/actions/greet\n", "example/app", nil, "", "", ""},
		{"checkout of another repository", "      - uses: actions/checkout@v4\n        with:\n          repository: acme/shared\n      - uses: ./.github/actions/greet\n", "example/app", []string{"local-action-wrong-repo"}, "acme/shared", "acme/shared", ""},
		{"checkout of self by name is not foreign", "      - uses: actions/checkout@v4\n        with:\n          repository: example/app\n      - uses: ./.github/actions/greet\n", "example/app", nil, "", "", ""},
		{"unknown self never flags wrong repo", "      - uses: actions/checkout@v4\n        with:\n          repository: acme/shared\n      - uses: ./.github/actions/greet\n", "", nil, "", "", ""},
		{"sparse checkout missing the action", "      - uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            src\n      - uses: ./.github/actions/greet\n", "example/app", []string{"local-action-sparse-miss"}, "", "", ""},
		{"sparse checkout covering the action", "      - uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            .github\n      - uses: ./.github/actions/greet\n", "example/app", nil, "", "", ""},
		{"subdirectory checkout only", "      - uses: actions/checkout@v4\n        with:\n          path: src\n      - uses: ./.github/actions/greet\n", "example/app", []string{"local-action-after-subdir-only"}, "", "", ""},
		{"literal ref recorded", "      - uses: actions/checkout@v4\n        with:\n          ref: v1.2.3\n      - uses: ./.github/actions/greet\n", "example/app", nil, "", "", "v1.2.3"},
		{"expression ref is unresolvable", "      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n      - uses: ./.github/actions/greet\n", "example/app", []string{"checkout-ref-unresolvable"}, "", "", ""},
		{"dynamic repository is not foreign", "      - uses: actions/checkout@v4\n        with:\n          repository: ${{ inputs.repo }}\n      - uses: ./.github/actions/greet\n", "example/app", nil, "", "", ""},
		{"no local action, no findings", "      - run: make\n", "example/app", nil, "", "", ""},
		{"conditional root under the consumer's condition covers it", "      - uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            docs\n      - if: failure()\n        uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            .github\n      - if: failure()\n        uses: ./.github/actions/greet\n", "example/app", nil, "", "", ""},
		{"conditional root under another condition does not", "      - uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            docs\n      - if: github.ref == 'a'\n        uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            src\n      - if: github.ref == 'b'\n        uses: ./.github/actions/greet\n", "example/app", []string{"local-action-sparse-miss"}, "(docs)", "", ""},
		{"conditional root sharing the condition is blamed", "      - uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            docs\n      - if: github.ref == 'a'\n        uses: actions/checkout@v4\n        with:\n          sparse-checkout: |\n            src\n      - if: github.ref == 'a'\n        uses: ./.github/actions/greet\n", "example/app", []string{"local-action-sparse-miss"}, "(src)", "", ""},
		{"later conditional foreign checkout the consumer shares resolves there", "      - uses: actions/checkout@v4\n      - if: github.ref == 'a'\n        uses: actions/checkout@v4\n        with:\n          repository: acme/shared\n      - if: github.ref == 'a'\n        uses: ./.github/actions/greet\n", "example/app", nil, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parse(t, job+tc.steps)
			got, ctx, _ := CheckCheckoutSubdirs(f, "build", tc.self, "", nil)
			expectRules(t, got, tc.want...)
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
			if tc.repo != "" && (ctx == nil || ctx.Repository != tc.repo) {
				t.Errorf("context repository = %+v, want %q", ctx, tc.repo)
			}
			if tc.ref != "" && (ctx == nil || ctx.Ref != tc.ref) {
				t.Errorf("context ref = %+v, want %q", ctx, tc.ref)
			}
		})
	}
}

func TestCheckSecretsTable(t *testing.T) {
	t.Parallel()
	reusable := func(secrets, body string) string {
		return "name: T\non:\n  workflow_call:\n" + secrets + "jobs:\n  build:\n    runs-on: ubuntu-latest\n" + body
	}
	for _, tc := range []struct {
		name            string
		src             string
		inherit, caller bool
		want            []string
		sub             string
		lax             bool // secret-undeclared is a warning when no local caller can confirm the contract
	}{
		{"declared and used", reusable("    secrets:\n      TOKEN: {required: true}\n", "    steps:\n      - run: x=${{ secrets.TOKEN }}\n"), false, false, nil, "", false},
		{"undeclared with a local caller is an error", reusable("    secrets:\n      TOKEN: {required: true}\n", "    steps:\n      - run: x=${{ secrets.OTHER }}\n"), false, true, []string{"secret-undeclared", "secret-unused"}, "OTHER", false},
		{"undeclared without a local caller is a warning", reusable("    secrets:\n      TOKEN: {required: true}\n", "    steps:\n      - run: x=${{ secrets.OTHER }}\n"), false, false, []string{"secret-undeclared", "secret-unused"}, "no caller in this repository", true},
		{"undeclared may resolve when a caller inherits", reusable("    secrets:\n      TOKEN: {required: true}\n", "    steps:\n      - run: x=${{ secrets.OTHER }}\n"), true, true, []string{"secret-undeclared", "secret-unused"}, "secrets: inherit", true},
		{"declared but unused", reusable("    secrets:\n      TOKEN: {required: true}\n", "    steps:\n      - run: echo hi\n"), false, false, []string{"secret-unused"}, "TOKEN", false},
		{"GITHUB_TOKEN declared", reusable("    secrets:\n      GITHUB_TOKEN: {required: true}\n", "    steps:\n      - run: x=${{ secrets.GITHUB_TOKEN }}\n"), false, false, []string{"secret-github-token-declared"}, "", false},
		{"GITHUB_TOKEN is never undeclared", reusable("    secrets:\n      TOKEN: {required: true}\n", "    steps:\n      - run: x=${{ secrets.TOKEN }} ${{ secrets.GITHUB_TOKEN }}\n"), false, false, nil, "", false},
		{"secret flows to a job output", job + "      - run: echo ok\n" + "    outputs:\n      token: ${{ secrets.TOKEN }}\n", false, false, []string{"secret-to-output"}, "secrets.TOKEN", false},
		{"secret in a step name", job + "      - name: deploy with ${{ secrets.TOKEN }}\n        run: echo deploying\n", false, false, []string{"secret-in-name"}, "", false},
		{"secret in a composite action", "name: N\ndescription: D\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: x=${{ secrets.TOKEN }}\n", false, false, []string{"secret-in-action"}, "TOKEN", false},
		{"plain workflow secrets need no declaration", job + "      - run: x=${{ secrets.TOKEN }}\n", false, false, nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckSecrets(parse(t, tc.src), tc.inherit, tc.caller)
			if tc.lax {
				if strings.Join(ids(got), ",") != strings.Join(tc.want, ",") {
					t.Fatalf("rules = %v want %v", ids(got), tc.want)
				}
				for _, fd := range got {
					if fd.Rule == "secret-undeclared" && fd.Severity != finding.Warning {
						t.Fatalf("secret-undeclared severity %v, want the deliberate warning", fd.Severity)
					}
				}
			} else {
				expectRules(t, got, tc.want...)
			}
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
		})
	}
}

func TestValuesTable(t *testing.T) {
	t.Parallel()
	step := func(ifv string) string {
		return job + "      - id: t\n        run: make test\n      - if: " + ifv + "\n        run: echo ok\n"
	}
	for _, tc := range []struct {
		name string
		src  string
		fn   func(*workflow.File) []finding.Finding
		want []string
		sub  string
		lax  bool // the rule deliberately emits below its registered severity
	}{
		{"outcome typo", step("steps.t.outcome == 'sucess'"), valueSetRules, []string{"value-set-mismatch"}, "sucess", false},
		{"outcome valid", step("steps.t.outcome == 'success'"), valueSetRules, nil, "", false},
		{"runner.os casing", step("runner.os == 'linux'"), valueSetRules, []string{"value-set-mismatch"}, "Linux", false},
		{"event name outside the triggers is a warning", step("github.event_name == 'pull_request'"), valueSetRules, []string{"value-set-mismatch"}, "pull_request", true},
		{"event name among the triggers", step("github.event_name == 'push'"), valueSetRules, nil, "", false},
		{"bare word is a non-empty string", step("success"), ifs, []string{"if-always-true-string"}, "", false},
		{"double wrapped", step("${{ github.event_name == 'push' }} && ${{ runner.os == 'Linux' }}"), ifs, []string{"if-double-wrapped"}, "", false},
		{"always with other", step("always() && github.ref == 'refs/heads/main'"), ifs, []string{"if-status-with-other"}, "", false},
		{"status with needs result is the idiom", step("always() && needs.x.result == 'success'"), ifs, nil, "", false},
		{"plain comparison", step("github.ref == 'refs/heads/main'"), ifs, nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn(parse(t, tc.src))
			if tc.lax {
				if strings.Join(ids(got), ",") != strings.Join(tc.want, ",") {
					t.Fatalf("rules = %v want %v", ids(got), tc.want)
				}
				if got[0].Severity != finding.Warning {
					t.Fatalf("severity %v, want the deliberate warning (medium review 3 D9)", got[0].Severity)
				}
			} else {
				expectRules(t, got, tc.want...)
			}
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
		})
	}
}

func TestChoicesTable(t *testing.T) {
	t.Parallel()
	dispatch := func(input string) string {
		return "name: T\non:\n  workflow_dispatch:\n    inputs:\n      level:\n        type: choice\n" + input + "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ inputs.level }}\n"
	}
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"default among options", dispatch("        options: [low, high]\n        default: low\n"), nil},
		{"default not an option", dispatch("        options: [low, high]\n        default: medium\n"), []string{"choice-default-not-option"}},
		{"no options", dispatch("        default: low\n"), []string{"choice-no-options"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectRules(t, CheckChoices(parse(t, tc.src)), tc.want...)
		})
	}
}

func TestCheckStepsSyntax(t *testing.T) {
	t.Parallel()
	f := parse(t, job+"      - run: |\n          if true; then echo hi\n")
	got := CheckSteps(BuildScope(f, "build", nil, nil))
	expectRules(t, got, "bash-syntax")
	f = parse(t, job+"      - run: |\n          if [ -z \"$X\" ]; then echo; fi\n")
	expectRules(t, CheckSteps(BuildScope(f, "build", nil, nil)))
}

func TestCheckBashTable(t *testing.T) {
	t.Parallel()
	run := func(body string) string {
		out := job + "      - id: s\n        run: |\n"
		for _, line := range splitLines(body) {
			out += "          " + line + "\n"
		}
		return out
	}
	for _, tc := range []struct {
		name string
		src  string
		want []string
		sub  string
	}{
		{"clean output write", run("echo \"k=v\" >> \"$GITHUB_OUTPUT\""), nil, ""},
		{"set-output", run("echo \"::set-output name=k::v\""), []string{"bash-deprecated-command"}, "set-output"},
		{"output line without redirect but referenced", run("echo \"k=v\"") + "      - run: echo ${{ steps.s.outputs.k }}\n", []string{"bash-output-unredirected"}, "steps.s.outputs.k"},
		{"output line without redirect and unreferenced", run("echo \"k=v\""), nil, ""},
		{"reading the output file", run("cat \"$GITHUB_OUTPUT\""), []string{"bash-output-read"}, ""},
		{"k=v into GITHUB_PATH", run("echo \"k=/usr/bin\" >> \"$GITHUB_PATH\""), []string{"bash-path-kv-line"}, ""},
		{"bare line into GITHUB_ENV", run("echo \"/usr/local/bin\" >> \"$GITHUB_ENV\""), []string{"bash-env-bare-line"}, ""},
		{"bad env key", run("echo \"my key=1\" >> \"$GITHUB_ENV\""), []string{"bash-env-bad-key"}, ""},
		{"reserved env key", run("echo \"GITHUB_TOKEN=x\" >> \"$GITHUB_ENV\""), []string{"bash-env-reserved-key"}, ""},
		{"env read in the same step", run("echo \"VERSION=1.2.3\" >> \"$GITHUB_ENV\"\necho \"building $VERSION\""), []string{"bash-env-same-step"}, "VERSION"},
		{"env read in a later step is fine", run("echo \"VERSION=1.2.3\" >> \"$GITHUB_ENV\"") + "      - run: echo \"building $VERSION\"\n", nil, ""},
		{"multiline write never closed", run("echo \"body<<EOF\" >> \"$GITHUB_OUTPUT\"\necho \"line\" >> \"$GITHUB_OUTPUT\""), []string{"heredoc-unterminated"}, "closing line"},
		{"multiline write closed", run("echo \"body<<EOF\" >> \"$GITHUB_OUTPUT\"\necho \"line\" >> \"$GITHUB_OUTPUT\"\necho \"EOF\" >> \"$GITHUB_OUTPUT\""), nil, ""},
		{"common delimiter around command output", run("echo \"body<<EOF\" >> \"$GITHUB_OUTPUT\"\necho \"$(git log -5)\" >> \"$GITHUB_OUTPUT\"\necho \"EOF\" >> \"$GITHUB_OUTPUT\"") + "      - run: echo ${{ steps.s.outputs.body }}\n", []string{"heredoc-delim-collision"}, "random delimiter"},
		{"indented form", run("echo \"body<<-EOF\" >> \"$GITHUB_OUTPUT\"\necho \"line\" >> \"$GITHUB_OUTPUT\"\necho \"EOF\" >> \"$GITHUB_OUTPUT\""), []string{"heredoc-indented-form"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parse(t, tc.src)
			sc := BuildScope(f, "build", nil, nil)
			got := CheckBash(sc)
			expectRules(t, got, tc.want...)
			if tc.sub != "" {
				msgContains(t, got, tc.want[0], tc.sub)
			}
		})
	}
}
