package rules_test

import (
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/shape"
	"github.com/najemi-software/jobsworth/internal/testkit"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Hand-built-input tables for the advanced rule functions (test spec §4
// rules row; test review 3 N2). Each row is one branch; fixtures and doc
// examples cover the same rules through the pipeline.

func parseFile(t *testing.T, src string) *workflow.File {
	t.Helper()
	f, err := workflow.Parse([]byte(src))
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	return f
}

func scopesOf(f *workflow.File) map[string]*rules.Scope {
	out := map[string]*rules.Scope{}
	if f.Kind != workflow.KindWorkflow {
		out[""] = rules.BuildScope(f, "", nil, nil)
		return out
	}
	for _, j := range f.Jobs {
		if j.Uses == nil {
			out[j.Name] = rules.BuildScope(f, j.Name, nil, nil)
		}
	}
	return out
}

// sortedIDs renders the findings' ids, leaving out rules that are off by
// default: the functions emit them and the config filter drops them
// later, so a table about one branch should not have to list them.
func sortedIDs(fs []finding.Finding) string {
	var ids []string
	for _, f := range fs {
		if r, _, ok := rules.Lookup(f.Rule); ok && r.Off {
			continue
		}
		ids = append(ids, f.Rule)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// hasRaw reports a finding of the given rule, default-off ones included.
func hasRaw(fs []finding.Finding, id string) bool {
	for _, f := range fs {
		if f.Rule == id {
			return true
		}
	}
	return false
}

func expectIDs(t *testing.T, got []finding.Finding, want string) {
	t.Helper()
	if sortedIDs(got) != want {
		t.Fatalf("rules %q want %q\n%+v", sortedIDs(got), want, got)
	}
}

const wf = "name: T\non: push\npermissions: read-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n"

func TestCheckStructureTable(t *testing.T) {
	t.Parallel()
	in := rules.StructureInputs{}
	for _, tc := range []struct{ name, src, want string }{
		{"clean", wf + "      - run: make\n", ""},
		{"unknown runner label", "name: T\non: push\njobs:\n  build:\n    runs-on: ubunut-latest\n    steps:\n      - run: make\n", "runs-on-unknown"},
		{"larger-runner convention is accepted", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-8core\n    steps:\n      - run: make\n", ""},
		{"retired runner label", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-20.04\n    steps:\n      - run: make\n", "runs-on-retired"},
		{"runs-on missing", "name: T\non: push\njobs:\n  build:\n    steps:\n      - run: make\n", "runs-on-missing"},
		{"empty steps", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: []\n", "job-empty-steps"},
		{"null step", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      -\n      - run: make\n", "step-null"},
		{"step with neither run nor uses", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - name: nothing\n", "step-no-action"},
		{"workflow name missing", "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "workflow-name-missing"},
		{"workflow name expression", "name: ${{ github.ref }}\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "name-expression"},
		{"outputs read by a job that does not need the producer", "name: T\non:\n  workflow_call:\njobs:\n  a:\n    runs-on: ubuntu-latest\n    outputs:\n      x: ${{ steps.s.outputs.x }}\n    steps:\n      - id: s\n        run: echo \"x=1\" >> \"$GITHUB_OUTPUT\"\n  b:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ needs.a.outputs.x }}\n", "job-orphan-outputs"},
		{"unreachable through literal false", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: false\n    steps:\n      - run: make\n  build:\n    runs-on: ubuntu-latest\n    needs: gate\n    steps:\n      - run: make\n", "job-unreachable"},
		{"unreachable through a status contradiction", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: success() && failure()\n    steps:\n      - run: make\n  build:\n    runs-on: ubuntu-latest\n    needs: gate\n    steps:\n      - run: make\n", "job-unreachable"},
		{"reachable when always() sits between", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: false\n    steps:\n      - run: make\n  mid:\n    runs-on: ubuntu-latest\n    needs: gate\n    if: always()\n    steps:\n      - run: make\n  build:\n    runs-on: ubuntu-latest\n    needs: mid\n    steps:\n      - run: make\n", ""},
		{"action without name or description", "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hi\n", "action-description-missing,action-name-missing"},
		{"reusable input without description", "name: T\non:\n  workflow_call:\n    inputs:\n      env:\n        type: string\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ inputs.env }}\n", ""}, // input-description-missing is off by default
		{"group and labels", "name: T\non: push\njobs:\n  build:\n    runs-on:\n      group: big\n      labels: [self-hosted]\n    steps:\n      - run: make\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectIDs(t, rules.CheckStructure(parseFile(t, tc.src), in), tc.want)
		})
	}
	// workflow-name-duplicate needs the repository's other workflows.
	f := parseFile(t, wf+"      - run: make\n")
	// Reported on the later path only, so the pair is reported once.
	both := []rules.NamedWorkflow{{Name: "T", Path: "/r/.github/workflows/a.yml"}, {Name: "T", Path: "/r/.github/workflows/b.yml"}}
	expectIDs(t, rules.CheckStructure(f, rules.StructureInputs{Path: "/r/.github/workflows/b.yml", Workflows: both}), "workflow-name-duplicate")
	expectIDs(t, rules.CheckStructure(f, rules.StructureInputs{Path: "/r/.github/workflows/a.yml", Workflows: both}), "")
	// runs-on-unknown accepts configured labels.
	f = parseFile(t, "name: T\non: push\njobs:\n  build:\n    runs-on: gpu-box\n    steps:\n      - run: make\n")
	expectIDs(t, rules.CheckStructure(f, rules.StructureInputs{RunnerLabels: []string{"gpu-box"}}), "")
	expectIDs(t, rules.CheckStructure(f, rules.StructureInputs{}), "runs-on-unknown")
}

func TestCheckTriggersTable(t *testing.T) {
	t.Parallel()
	body := "permissions: read-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n"
	for _, tc := range []struct{ name, src, want string }{
		{"clean", "name: T\non: push\n" + body, ""},
		{"unknown event", "name: T\non: pussh\n" + body, "on-unknown-event"},
		{"missing on", "name: T\n" + body, "on-missing"},
		{"filter conflict", "name: T\non:\n  push:\n    branches: [main]\n    branches-ignore: [dev]\n" + body, "on-filter-conflict"},
		{"empty filter", "name: T\non:\n  push:\n    branches: []\n" + body, "on-filter-empty"},
		{"filter with expression", "name: T\non:\n  push:\n    branches: ['${{ vars.BRANCH }}']\n" + body, "on-filter-expression"},
		{"unknown key under event", "name: T\non:\n  push:\n    branch: [main]\n" + body, "on-unknown-key"},
		{"unknown activity type", "name: T\non:\n  pull_request:\n    types: [opened, reopend]\n" + body, "on-unknown-type"},
		{"tags only on push", "name: T\non:\n  push:\n    tags: ['v*']\n" + body, ""}, // on-push-tags-only is off by default
		{"cron too frequent", "name: T\non:\n  schedule:\n    - cron: '*/2 * * * *'\n" + body, "cron-too-frequent"},
		{"cron impossible date", "name: T\non:\n  schedule:\n    - cron: '0 0 31 2 *'\n" + body, "cron-impossible-date"},
		{"cron syntax", "name: T\non:\n  schedule:\n    - cron: '0 0 * *'\n" + body, "cron-syntax"},
		{"cron unquoted", "name: T\non:\n  schedule:\n    - cron: 0 0 * * *\n" + body, "cron-quoted"},
		{"dispatch input type", "name: T\non:\n  workflow_dispatch:\n    inputs:\n      level:\n        type: text\n" + body, "dispatch-input-type"},
		{"dispatch input limit", "name: T\non:\n  workflow_dispatch:\n    inputs:\n" + strings.Repeat("      i%d:\n        type: string\n", 1) + body, ""},
		{"input declared without a trigger", "name: T\non: push\ninputs:\n  x:\n    type: string\n" + body, ""},
		{"workflow_run target unknown", "name: T\non:\n  workflow_run:\n    workflows: [Nope]\n    types: [completed]\n" + body, ""}, // needs WorkflowNames
		{"concurrency group static", "name: T\non: push\nconcurrency: build\n" + body, "concurrency-group-static"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectIDs(t, rules.CheckTriggers(parseFile(t, tc.src), rules.TriggerInputs{}), tc.want)
		})
	}
	// on-workflow-run-unknown needs the repository's workflow names.
	f := parseFile(t, "name: T\non:\n  workflow_run:\n    workflows: [Nope]\n    types: [completed]\n"+body)
	expectIDs(t, rules.CheckTriggers(f, rules.TriggerInputs{HasRepo: true, WorkflowNames: []string{"CI"}}), "on-workflow-run-unknown")
	expectIDs(t, rules.CheckTriggers(f, rules.TriggerInputs{HasRepo: true, WorkflowNames: []string{"Nope"}}), "")
	// Extra events from config are accepted.
	f = parseFile(t, "name: T\non: custom_event\n"+body)
	expectIDs(t, rules.CheckTriggers(f, rules.TriggerInputs{ExtraEvents: []string{"custom_event"}}), "")
}

func TestCheckConditionsTable(t *testing.T) {
	t.Parallel()
	step := func(ifv string) string {
		return wf + "      - run: make\n      - if: " + ifv + "\n        run: echo ok\n"
	}
	for _, tc := range []struct{ name, src, want string }{
		{"literal false", step("false"), "if-literal-false"},
		{"redundant success is off by default", step("success()"), ""},
		{"status contradiction", step("success() && failure()"), "if-status-contradiction"},
		{"not always", step("${{ !always() }}"), "if-status-contradiction"},
		{"always false comparison", step("github.ref == 'a' && github.ref == 'b'"), "if-always-false"},
		{"satisfiable", step("github.ref == 'a' || github.ref == 'b'"), ""},
		{"failure on a job with no needs", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    if: failure()\n    steps:\n      - run: make\n", "if-status-contradiction"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectIDs(t, rules.CheckConditions(parseFile(t, tc.src), rules.ConditionInputs{}), tc.want)
		})
	}
	if got := rules.CheckConditions(parseFile(t, step("success()")), rules.ConditionInputs{}); !hasRaw(got, "if-redundant-success") {
		t.Fatalf("if-redundant-success should be emitted (config turns it off): %+v", got)
	}
	// Caller value sets (§4.3): complete sets prove a condition dead.
	callee := parseFile(t, "name: C\non:\n  workflow_call:\n    inputs:\n      env:\n        type: string\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    if: inputs.env == 'prod'\n    steps:\n      - run: echo deploy\n")
	vals := map[string][]rules.CallerValue{"env": {{Site: "a.yml:5 (job x)", Value: "staging"}}}
	expectIDs(t, rules.CheckConditions(callee, rules.ConditionInputs{CallerValues: vals, KnownCallers: 1, WalkedCallers: 1, Complete: true}), "if-dead-for-all-callers")
	expectIDs(t, rules.CheckConditions(callee, rules.ConditionInputs{CallerValues: vals, KnownCallers: 2, WalkedCallers: 1, Complete: true}), "")
	expectIDs(t, rules.CheckConditions(callee, rules.ConditionInputs{CallerValues: vals, KnownCallers: 1, WalkedCallers: 1, Complete: false}), "")
	expectIDs(t, rules.CheckConditions(callee, rules.ConditionInputs{CallerValues: map[string][]rules.CallerValue{"env": {{Site: "a", Value: "prod"}, {Site: "b", Value: "staging"}}}, KnownCallers: 2, WalkedCallers: 2, Complete: true}), "")
	// --input seeds are complete by definition.
	dispatch := parseFile(t, "name: D\non:\n  workflow_dispatch:\n    inputs:\n      dry:\n        type: boolean\n        default: true\njobs:\n  build:\n    runs-on: ubuntu-latest\n    if: inputs.dry == false\n    steps:\n      - run: make\n")
	expectIDs(t, rules.CheckConditions(dispatch, rules.ConditionInputs{Seeded: map[string]string{"dry": "true"}}), "if-dead-for-all-callers")
	expectIDs(t, rules.CheckConditions(dispatch, rules.ConditionInputs{}), "")
}

func TestCheckReachabilityTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"gated chain without a guard", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: github.ref == 'refs/heads/main'\n    steps:\n      - run: make\n  build:\n    runs-on: ubuntu-latest\n    needs: gate\n    steps:\n      - run: make\n", "job-needs-skipped"},
		{"guarded with always and result", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: github.ref == 'refs/heads/main'\n    steps:\n      - run: make\n  build:\n    runs-on: ubuntu-latest\n    needs: gate\n    if: always() && needs.gate.result != 'failure'\n    steps:\n      - run: make\n", ""},
		{"dependant reads the outputs", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: github.ref == 'refs/heads/main'\n    outputs:\n      v: ${{ steps.s.outputs.v }}\n    steps:\n      - id: s\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n  build:\n    runs-on: ubuntu-latest\n    needs: gate\n    steps:\n      - run: echo ${{ needs.gate.outputs.v }}\n", ""},
		{"dependency never runs is job-unreachable's business", "name: T\non: push\njobs:\n  gate:\n    runs-on: ubuntu-latest\n    if: false\n    steps:\n      - run: make\n  build:\n    runs-on: ubuntu-latest\n    needs: gate\n    steps:\n      - run: make\n", ""},
		{"unreachable step with consumed outputs", wf + "      - id: s\n        if: false\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ steps.s.outputs.v }}\n", "step-unreachable"},
		{"unreachable step nobody reads", wf + "      - id: s\n        if: false\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - run: make\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, tc.src)
			expectIDs(t, rules.CheckReachability(f, scopesOf(f)), tc.want)
		})
	}
}

func TestCheckPerformanceTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"checkout then build", wf + "      - uses: actions/checkout@v4\n      - run: make\n", ""},
		{"trailing checkout", wf + "      - run: make\n      - uses: actions/checkout@v4\n", "checkout-trailing"},
		{"checkout followed only by echoes", wf + "      - uses: actions/checkout@v4\n      - run: echo hi\n", "checkout-unused"},
		{"checkout followed by a substitution that reads the tree", wf + "      - uses: actions/checkout@v4\n      - run: echo \"v=$(cat VERSION)\"\n", ""},
		{"redundant root checkout", wf + "      - uses: actions/checkout@v4\n      - run: make\n      - uses: actions/checkout@v4\n      - run: make test\n", "checkout-redundant"},
		{"second checkout of another repo is not redundant", wf + "      - uses: actions/checkout@v4\n      - run: make\n      - uses: actions/checkout@v4\n        with:\n          repository: acme/shared\n          path: shared\n      - run: make test\n", ""},
		{"whole workspace artifact", wf + "      - uses: actions/checkout@v4\n      - run: make\n      - uses: actions/upload-artifact@v4\n        with:\n          path: .\n", "artifact-whole-workspace"},
		{"mergeable sequential jobs", "name: T\non: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n  b:\n    runs-on: ubuntu-latest\n    needs: a\n    steps:\n      - run: make test\n", "jobs-mergeable"},
		{"not mergeable across runners", "name: T\non: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n  b:\n    runs-on: windows-latest\n    needs: a\n    steps:\n      - run: make test\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, tc.src)
			expectIDs(t, rules.CheckPerformance(f, scopesOf(f), rules.PerformanceInputs{}), tc.want)
		})
	}
	// A configured filesystem-clean action keeps a checkout unused.
	f := parseFile(t, wf+"      - uses: actions/checkout@v4\n      - uses: acme/lint@v1\n")
	expectIDs(t, rules.CheckPerformance(f, scopesOf(f), rules.PerformanceInputs{FSCleanActions: []string{"acme/lint"}}), "checkout-unused")
	expectIDs(t, rules.CheckPerformance(f, scopesOf(f), rules.PerformanceInputs{}), "")
}

func TestCheckSecurityTable(t *testing.T) {
	t.Parallel()
	base := rules.SecurityInputs{Self: "example/app", TrustedOwners: []string{"acme", "aws-actions"}}
	prt := "name: T\non: pull_request_target\npermissions: read-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n"
	for _, tc := range []struct {
		name, src, want string
		in              rules.SecurityInputs
	}{
		{"clean", wf + "      - uses: actions/checkout@v4\n      - run: make\n", "", base},
		{"injection into run", wf + "      - run: echo ${{ github.event.issue.title }}\n", "injection-run", base},
		{"injection into github-script", wf + "      - uses: actions/github-script@v7\n        with:\n          script: console.log('${{ github.event.issue.title }}')\n", "injection-script", base},
		{"untrusted path through env is fine", wf + "      - env:\n          T: ${{ github.event.issue.title }}\n        run: echo \"$T\"\n", "", base},
		{"tainted env through a step output", wf + "      - id: m\n        env:\n          T: ${{ github.event.issue.title }}\n        run: echo \"t=$T\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ steps.m.outputs.t }}\n", "injection-run", base},
		{"unpinned untrusted owner", wf + "      - uses: someone/thing@v1\n", "uses-unpinned", base},
		{"unpinned trusted owner", wf + "      - uses: acme/thing@v1\n", "", base},
		{"sha pinned", wf + "      - uses: someone/thing@0123456789abcdef0123456789abcdef01234567 # v1\n", "", base},
		{"docker unpinned", wf + "      - uses: docker://alpine:latest\n", "uses-docker-unpinned", base},
		{"write-all permissions", "name: T\non: push\npermissions: write-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "permissions-write-all", base},
		{"unknown permission scope", "name: T\non: push\npermissions:\n  content: read\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "permissions-unknown-scope", base},
		{"unknown permission level", "name: T\non: push\npermissions:\n  contents: rw\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "permissions-unknown-level", base},
		{"id-token unused", "name: T\non: push\npermissions:\n  id-token: write\n  contents: read\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "permissions-id-token-unused", base},
		{"id-token used by an OIDC action", "name: T\non: push\npermissions:\n  id-token: write\n  contents: read\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: aws-actions/configure-aws-credentials@v4\n", "", base},
		{"prt with untrusted checkout", prt + "      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n      - run: echo hi\n", "prt-untrusted-checkout", base},
		{"prt untrusted checkout and run from the tree", prt + "      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n      - run: npm install\n", "prt-untrusted-checkout-and-run", base},
		{"prt no permissions", "name: T\non: pull_request_target\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "permissions-missing,prt-no-permissions", base},
		{"secret echoed", wf + "      - run: echo ${{ secrets.TOKEN }}\n", "secret-echo", base},
		{"secret to artifact", wf + "      - run: echo ${{ secrets.TOKEN }} > creds.txt\n      - uses: actions/upload-artifact@v4\n        with:\n          path: creds.txt\n", "secret-to-artifact", base},
		{"cache key from head ref on prt", prt + "      - uses: actions/cache@v4\n        with:\n          path: ~/.npm\n          key: npm-${{ github.head_ref }}\n", "cache-poisoning-path", base},
		{"self-hosted on pull_request", "name: T\non: pull_request\npermissions: read-all\njobs:\n  build:\n    runs-on: self-hosted\n    steps:\n      - run: make\n", "self-hosted-pr-trigger", base},
		{"persist-credentials only in strict mode", wf + "      - uses: actions/checkout@v4\n      - uses: actions/upload-artifact@v4\n        with:\n          path: dist\n", "", base},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, tc.src)
			expectIDs(t, rules.CheckSecurity(f, scopesOf(f), tc.in), tc.want)
		})
	}
	// injection-via-input: one hop into a callee's run: sink, and two hops
	// through a forwarding callee resolved by Resolve.
	sink := parseFile(t, "name: Sink\ndescription: D\ninputs:\n  text:\n    required: true\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ${{ inputs.text }}\n")
	forward := parseFile(t, "name: Fwd\ndescription: D\ninputs:\n  title:\n    required: true\nruns:\n  using: composite\n  steps:\n    - uses: acme/sink@v1\n      with:\n        text: ${{ inputs.title }}\n")
	caller := parseFile(t, wf+"      - uses: acme/fwd@v1\n        with:\n          title: ${{ github.event.issue.title }}\n")
	scopes := map[string]*rules.Scope{"build": rules.BuildScope(caller, "build", []rules.Callee{{Step: 0, File: forward, Name: "acme/fwd@v1", Key: "fwd"}}, nil)}
	expectIDs(t, rules.CheckSecurity(caller, scopes, base), "") // one hop: fwd has no sink of its own
	resolve := func(callee *workflow.File, raw string) *workflow.File {
		if callee == forward && raw == "acme/sink@v1" {
			return sink
		}
		return nil
	}
	in := base
	in.Resolve = resolve
	expectIDs(t, rules.CheckSecurity(caller, scopes, in), "injection-via-input")
	direct := parseFile(t, wf+"      - uses: acme/sink@v1\n        with:\n          text: ${{ github.event.issue.title }}\n")
	scopes = map[string]*rules.Scope{"build": rules.BuildScope(direct, "build", []rules.Callee{{Step: 0, File: sink, Name: "acme/sink@v1", Key: "sink"}}, nil)}
	expectIDs(t, rules.CheckSecurity(direct, scopes, base), "injection-via-input")
}

func TestCheckShapesTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"literal json chain", wf + "      - run: echo ${{ fromJSON('{\"a\":1}').a }}\n", ""},
		{"mismatch on a literal", wf + "      - run: echo ${{ fromJSON('{\"a\":1}').b }}\n", "shape-mismatch"},
		{"invalid json literal", wf + "      - run: echo ${{ fromJSON('{a:1}') }}\n", "fromjson-invalid-literal"},
		{"fromJSON of a parsed object", wf + "      - run: echo ${{ fromJSON(fromJSON('{\"a\":1}')).a }}\n", "fromjson-non-string"},
		{"jq filter syntax", wf + "      - run: \"jq '{version: .tag' release.json\"\n", "jq-syntax"},
		{"unknown function", wf + "      - run: echo ${{ formatt('{0}', github.sha) }}\n", "expr-unknown-function"},
		{"arity", wf + "      - run: echo ${{ toJSON(github, github) }}\n", "expr-arity"},
		{"format placeholders", wf + "      - run: echo ${{ format('{0} {1}', github.sha) }}\n", "expr-format-placeholders"},
		{"contains over an object", wf + "      - run: echo ${{ contains(fromJSON('{\"a\":1}'), 'a') }}\n", "expr-contains-types"},
		{"hashFiles absolute", wf + "      - run: echo ${{ hashFiles('/etc/hosts') }}\n", "expr-hashfiles-absolute"},
		{"secrets in if", wf + "      - if: secrets.TOKEN != ''\n        run: echo hi\n", "expr-secrets-in-if"},
		{"env in a job if", "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    if: env.X == '1'\n    steps:\n      - run: make\n", "expr-env-in-if-job"},
		{"matrix at the workflow level", "name: T\non: push\nenv:\n  OS: ${{ matrix.os }}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n", "expr-context-unavailable"},
		{"string escape", wf + "      - run: echo ${{ 'it\\'s' }}\n", "expr-string-escape"},
		{"comparison across types", "name: T\non:\n  workflow_call:\n    inputs:\n      count:\n        type: number\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ inputs.count == 'many' }}\n", "expr-comparison-type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, tc.src)
			expectIDs(t, rules.CheckShapes(f, scopesOf(f), rules.ShapeInputs{}), tc.want)
		})
	}
	// Files read in scripts (§7.4) need ReadFile; a missing one is reported.
	f := parseFile(t, wf+"      - id: s\n        run: echo \"cfg=$(cat config.json)\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ fromJSON(steps.s.outputs.cfg).a }}\n")
	files := map[string]string{"config.json": `{"a": 1}`}
	read := func(p string) ([]byte, bool) { v, ok := files[p]; return []byte(v), ok }
	expectIDs(t, rules.CheckShapes(f, scopesOf(f), rules.ShapeInputs{ReadFile: read}), "")
	files["config.json"] = `{"b": 1}`
	expectIDs(t, rules.CheckShapes(f, scopesOf(f), rules.ShapeInputs{ReadFile: read}), "shape-mismatch")
	delete(files, "config.json")
	if got := rules.CheckShapes(f, scopesOf(f), rules.ShapeInputs{ReadFile: read}); !hasRaw(got, "shape-file-missing") {
		t.Fatalf("shape-file-missing (default-off) should be emitted: %+v", got)
	}
}

func TestCheckMatrixTable(t *testing.T) {
	t.Parallel()
	job := func(strategy string) string {
		return "name: T\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    strategy:\n" + strategy + "    steps:\n      - run: echo ${{ matrix.os }}\n"
	}
	for _, tc := range []struct{ name, src, want string }{
		{"plain matrix", job("      matrix:\n        os: [a, b]\n"), ""},
		{"empty axis", job("      matrix:\n        os: []\n"), "matrix-empty"},
		{"exclude matches nothing", job("      matrix:\n        os: [a, b]\n        exclude:\n          - os: c\n"), "matrix-exclude-no-match"},
		{"fail-fast is not a boolean", job("      matrix:\n        os: [a]\n      fail-fast: yes please\n"), "matrix-strategy-type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectIDs(t, rules.CheckMatrix(parseFile(t, tc.src)), tc.want)
		})
	}
}

func TestCheckScriptsTable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake tools are shell scripts")
	}
	testkit.FakeTools(t) // t.Setenv: not parallel
	in := rules.ScriptInputs{ShellcheckEnabled: "auto", ShellcheckMinSeverity: finding.Note, Python: "auto", Pwsh: "auto"}
	for _, tc := range []struct{ name, src, want string }{
		{"shellcheck finding", wf + "      - run: ls $FILES\n", "shellcheck-SC2086"},
		{"shellcheck clean", wf + "      - run: ls \"$FILES\"\n", ""},
		{"python syntax", wf + "      - shell: python\n        run: |\n          def broken(:\n              pass\n", "python-syntax"},
		{"python clean", wf + "      - shell: python\n        run: print('ok')\n", ""},
		{"pwsh syntax", wf + "      - shell: pwsh\n        run: Write-Host \"unterminated\n", "pwsh-syntax"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, tc.src)
			expectIDs(t, rules.CheckScripts(f, scopesOf(f), in), tc.want)
		})
	}
	off := in
	off.ShellcheckEnabled = "false"
	f := parseFile(t, wf+"      - run: ls $FILES\n")
	expectIDs(t, rules.CheckScripts(f, scopesOf(f), off), "")
}

func TestCheckExprTypesTable(t *testing.T) {
	t.Parallel()
	callee := parseFile(t, "name: C\non:\n  workflow_call:\n    inputs:\n      count:\n        type: number\n      flag:\n        type: boolean\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ inputs.count }} ${{ inputs.flag }}\n")
	caller := parseFile(t, "name: T\non: push\njobs:\n  call:\n    uses: ./.github/workflows/c.yml\n    with:\n      count: ${{ github.ref }}\n      flag: ${{ github.event_name == 'push' }}\n")
	job := caller.Jobs[0]
	shapes := map[string]*shape.Shape{"count": shape.StringShape(), "flag": shape.BoolShape()}
	expectIDs(t, rules.CheckExprTypes(job, callee, "c.yml", shapes), "call-input-type")
	expectIDs(t, rules.CheckExprTypes(job, callee, "c.yml", map[string]*shape.Shape{"count": shape.NumberShape(), "flag": shape.BoolShape()}), "")
	expectIDs(t, rules.CheckExprTypes(job, callee, "c.yml", map[string]*shape.Shape{"count": shape.AnyShape()}), "")
}

// The small helpers (test review 4 N2): one table each.

func TestCheckCallSecretsTable(t *testing.T) {
	t.Parallel()
	callee := parseFile(t, "name: C\non:\n  workflow_call:\n    secrets:\n      TOKEN:\n        required: true\n      OPTIONAL:\n        required: false\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: x=${{ secrets.TOKEN }} ${{ secrets.OPTIONAL }}\n")
	bare := parseFile(t, "name: C\non:\n  workflow_call:\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")
	caller := func(secrets string) workflow.Job {
		f := parseFile(t, "name: T\non: push\njobs:\n  call:\n    uses: ./.github/workflows/c.yml\n"+secrets)
		return f.Jobs[0]
	}
	for _, tc := range []struct {
		name   string
		job    workflow.Job
		callee *workflow.File
		want   string
	}{
		{"all supplied", caller("    secrets:\n      TOKEN: ${{ secrets.TOKEN }}\n      OPTIONAL: ${{ secrets.OPT }}\n"), callee, ""},
		{"required missing", caller("    secrets:\n      OPTIONAL: ${{ secrets.OPT }}\n"), callee, "call-secret-missing"},
		{"unknown name", caller("    secrets:\n      TOKEN: ${{ secrets.TOKEN }}\n      EXTRA: ${{ secrets.X }}\n"), callee, "call-secret-unknown"},
		{"literal value", caller("    secrets:\n      TOKEN: hunter2\n"), callee, "call-secret-literal"},
		{"inherit covers everything", caller("    secrets: inherit\n"), callee, ""},
		{"inherit on a callee without secrets", caller("    secrets: inherit\n"), bare, ""}, // call-secret-inherit-partial is off by default
		{"no secrets block, none required", caller(""), bare, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectIDs(t, rules.CheckCallSecrets(tc.job, tc.callee, "c.yml"), tc.want)
		})
	}
	if got := rules.CheckCallSecrets(caller("    secrets: inherit\n"), bare, "c.yml"); !hasRaw(got, "call-secret-inherit-partial") {
		t.Fatalf("call-secret-inherit-partial should be emitted: %+v", got)
	}
}

func TestCheckStepInputsTable(t *testing.T) {
	t.Parallel()
	action := parseFile(t, "name: A\ndescription: D\ninputs:\n  name:\n    description: n\n    required: true\n  level:\n    description: l\n    type: choice\n    options: [low, high]\n  count:\n    description: c\n    type: number\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ${{ inputs.name }}\n")
	for _, tc := range []struct{ name, steps, want string }{
		{"run and uses", "      - uses: acme/a@v1\n        run: echo hi\n", "step-run-and-uses"},
		{"with on a run step", "      - run: echo hi\n        with:\n          x: 1\n", "step-with-on-run"},
		{"all good", "      - uses: acme/a@v1\n        with:\n          name: world\n          level: high\n          count: 3\n", ""},
		{"unknown input", "      - uses: acme/a@v1\n        with:\n          name: world\n          nmae: typo\n", "step-input-unknown"},
		{"required missing", "      - uses: acme/a@v1\n        with:\n          level: low\n", "step-input-missing"},
		{"choice outside the options", "      - uses: acme/a@v1\n        with:\n          name: world\n          level: medium\n", "step-input-choice"},
		{"number given a word", "      - uses: acme/a@v1\n        with:\n          name: world\n          count: many\n", "step-input-type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, wf+tc.steps)
			sc := rules.BuildScope(f, "build", nil, nil)
			st := workflow.StepsOf(f, "build")[0]
			expectIDs(t, rules.CheckStepInputs(sc, st, action, "acme/a@v1"), tc.want)
		})
	}
	// A nil callee runs only the structural rules.
	f := parseFile(t, wf+"      - uses: acme/a@v1\n        with:\n          nmae: typo\n")
	expectIDs(t, rules.CheckStepInputs(rules.BuildScope(f, "build", nil, nil), workflow.StepsOf(f, "build")[0], nil, "acme/a@v1"), "")
}

func TestCheckStepExprTypesTable(t *testing.T) {
	t.Parallel()
	action := parseFile(t, "name: A\ndescription: D\ninputs:\n  flag:\n    description: f\n    type: boolean\n  count:\n    description: c\n    type: number\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ${{ inputs.flag }}\n")
	f := parseFile(t, wf+"      - uses: acme/a@v1\n        with:\n          flag: ${{ github.event_name }}\n          count: ${{ github.run_number }}\n")
	sc := rules.BuildScope(f, "build", nil, nil)
	st := workflow.StepsOf(f, "build")[0]
	// A string-shaped expression may hold anything at runtime: no finding.
	expectIDs(t, rules.CheckStepExprTypes(sc, st, action, "acme/a@v1", map[string]*shape.Shape{"flag": shape.StringShape(), "count": shape.StringShape()}), "")
	// A definitely-typed value that cannot fit does.
	expectIDs(t, rules.CheckStepExprTypes(sc, st, action, "acme/a@v1", map[string]*shape.Shape{"flag": shape.NumberShape()}), "step-input-type")
	expectIDs(t, rules.CheckStepExprTypes(sc, st, action, "acme/a@v1", map[string]*shape.Shape{"flag": shape.BoolShape(), "count": shape.NumberShape()}), "")
	expectIDs(t, rules.CheckStepExprTypes(sc, st, nil, "acme/a@v1", nil), "")
}

func TestCheckStepContinueTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, steps, want string }{
		{"outputs of a continue-on-error step consumed unguarded", "      - id: t\n        continue-on-error: true\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ steps.t.outputs.v }}\n", "step-continue-on-error-outputs"},
		{"guarded by outcome in the consumer's if", "      - id: t\n        continue-on-error: true\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - if: steps.t.outcome == 'success'\n        run: echo ${{ steps.t.outputs.v }}\n", ""},
		{"guarded inside the expression", "      - id: t\n        continue-on-error: true\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ steps.t.outcome == 'success' && steps.t.outputs.v || 'none' }}\n", ""},
		{"no continue-on-error", "      - id: t\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ steps.t.outputs.v }}\n", ""},
		{"nothing consumes the outputs", "      - id: t\n        continue-on-error: true\n        run: echo \"v=1\" >> \"$GITHUB_OUTPUT\"\n      - run: echo done\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseFile(t, wf+tc.steps)
			got := rules.CheckStepContinue(rules.BuildScope(f, "build", nil, nil))
			if tc.want == "" && len(got) != 0 || tc.want != "" && !hasRaw(got, tc.want) { // default-off rule
				t.Fatalf("got %+v want %q", got, tc.want)
			}
		})
	}
}

func TestCheckCheckoutContext(t *testing.T) {
	t.Parallel()
	f := parseFile(t, wf+"      - uses: actions/checkout@v4\n        with:\n          repository: acme/shared\n          ref: v2\n      - uses: ./.github/actions/notify\n")
	fs, ctx := rules.CheckCheckout(f, "build", "example/app", "", nil)
	if sortedIDs(fs) != "local-action-wrong-repo" || ctx == nil || ctx.Repository != "acme/shared" || ctx.Ref != "v2" {
		t.Fatalf("findings %q context %+v", sortedIDs(fs), ctx)
	}
	if _, ctx := rules.CheckCheckout(f, "", "example/app", "", nil); ctx != nil {
		t.Fatal("no job, no context")
	}
	f = parseFile(t, wf+"      - run: make\n")
	if fs, ctx := rules.CheckCheckout(f, "build", "example/app", "", nil); len(fs) != 0 || ctx != nil {
		t.Fatalf("no checkout: %+v %+v", fs, ctx)
	}
}
