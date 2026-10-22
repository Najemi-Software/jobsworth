package explain

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/explain"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

type result struct {
	stdout, stderr string
	code           int
}

func run(t *testing.T, kit *testkit.Kit, cwd string, tokens ...string) result {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		t.Fatalf("parse %q: %v", tokens, err)
	}
	env := map[string]string{"JOBSWORTH_CACHE_DIR": kit.CacheDir, "JOBSWORTH_API_URL": kit.Server.URL, "JOBSWORTH_TOKEN": "t"}
	var out, errb bytes.Buffer
	err = RunWith(args, flags, Env{Cwd: cwd, Home: kit.Home, Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &errb})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	stable := strings.ReplaceAll(out.String(), cwd, "<ROOT>")
	stable = strings.ReplaceAll(stable, kit.Testdata, "<TESTDATA>")
	return result{stdout: stable, stderr: errb.String(), code: cli.ExitCode(err, cli.ExitFindings)}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", "explain", name)
	if *update {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if string(want) != got {
		t.Fatalf("golden %s differs (run with -update to accept)\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

// exampleRepo writes a doc's flagged example as a workflow or action in a
// repo whose .github tree is a copy of testdata/callees, so local
// references resolve as they do in the doc tests.
func exampleRepo(t *testing.T, kit *testkit.Kit, body string) (root, path string) {
	t.Helper()
	root = t.TempDir()
	src := filepath.Join(kit.Testdata, "callees")
	_ = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		data, _ := os.ReadFile(p)
		return os.WriteFile(target, data, 0o644)
	})
	name := "example.yml"
	if strings.Contains(body, "runs:") && !strings.Contains(body, "jobs:") {
		name = "action.yml"
	}
	dir := filepath.Join(root, ".github", "workflows")
	if name == "action.yml" {
		dir = filepath.Join(root, ".github", "actions", "example")
	}
	_ = os.MkdirAll(dir, 0o755)
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}

// TestGoldenPerRule is spec-explain §8: every rule's context renderer has a
// golden text and json output against the rule's own flagged example.
func TestGoldenPerRule(t *testing.T) {
	kit := testkit.New(t, testdata)
	for _, r := range rules.Rules {
		r := r
		t.Run(r.ID, func(t *testing.T) {
			doc, err := rules.LoadDoc(r.ID)
			if err != nil {
				t.Fatal(err)
			}
			root, path := exampleRepo(t, kit, doc.Flagged)
			tokens := []string{"--brief", "--rule", r.ID}
			cfgLines := []string{"schema: 1", "security: { trusted_owners: [acme, dorny, najemi-software, hashicorp] }"}
			if r.ID != "permissions-missing" {
				cfgLines = append(cfgLines, "rules:", "  permissions-missing: off")
			}
			for _, h := range doc.TestHints {
				switch {
				case h == "offline":
					tokens = append(tokens, "--offline")
				case h == "enrich":
					tokens = append(tokens, "--enrich")
				case h == "enable":
					cfgLines = append(cfgLines, "  "+r.ID+": "+r.Severity.String())
					if r.ID == "permissions-missing" {
						cfgLines[len(cfgLines)-1] = "rules:\n" + cfgLines[len(cfgLines)-1]
					}
				case strings.HasPrefix(h, "self="):
					// Checkout tracking needs the repo's own slug: make the
					// example repo a git checkout with that origin.
					if _, err := exec.LookPath("git"); err != nil {
						t.Skip("git not on PATH")
					}
					for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/" + strings.TrimPrefix(h, "self=") + ".git"}} {
						cmd := exec.Command("git", args...)
						cmd.Dir = root
						if out, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("git %v: %v %s", args, err, out)
						}
					}
				}
			}
			_ = os.WriteFile(filepath.Join(root, config.FileName), []byte(strings.Join(cfgLines, "\n")+"\n"), 0o644)
			// Find the first line the rule fires on by running check.
			cfg, _ := config.LoadDefault(root, rules.IsKnown)
			res := check.RunEntry(path, check.Options{Fetcher: testkit.Fetcher(kit, strings.Contains(strings.Join(tokens, " "), "--offline")), Config: cfg, RootOverride: root, SelfSlug: selfOf(doc.TestHints),
				Enrich: strings.Contains(strings.Join(tokens, " "), "--enrich"), Store: cache.New(kit.CacheDir)})
			line, fired := 0, false
			ruleID := r.ID
			for _, f := range check.AllFindings(res) {
				if f.Rule == r.ID || (r.Dynamic && rules.IsDerived(f.Rule)) {
					fired = true
					ruleID = f.Rule // a derived id (shellcheck-SC2086) stands for its umbrella
					if f.File == path {
						line = f.Line
						break
					}
				}
			}
			for i := range tokens {
				if tokens[i] == "--rule" && i+1 < len(tokens) {
					tokens[i+1] = ruleID
				}
			}
			if !fired {
				t.Fatalf("rule %s did not fire on its flagged example: %+v", r.ID, check.AllFindings(res))
			}
			loc := filepath.Join(".github", "workflows", "example.yml")
			if strings.HasSuffix(path, "action.yml") {
				loc = filepath.Join(".github", "actions", "example", "action.yml")
			}
			if line > 0 {
				loc += ":" + itoa(line) // a line-0 finding is explained through `<file> --rule`
			}
			text := run(t, kit, root, append(tokens, loc)...)
			if text.code != 0 {
				t.Fatalf("exit %d: %s\n%s", text.code, text.stderr, text.stdout)
			}
			golden(t, r.ID+".txt", text.stdout)
			js := run(t, kit, root, append(append(tokens, "--format", "json"), loc)...)
			var doc2 map[string]any
			if err := json.Unmarshal([]byte(js.stdout), &doc2); err != nil {
				t.Fatalf("json: %v\n%s", err, js.stdout)
			}
			// Every rule gets a real context block; falling to the generic
			// renderer is a regression unless listed here (review N3).
			genericOK := map[string]bool{"file-invalid": true}
			for _, f := range doc2["findings"].([]any) {
				ctx := f.(map[string]any)["context"].(map[string]any)
				if ctx["kind"] == "generic" && !genericOK[r.ID] {
					t.Errorf("rule %s rendered the generic context block; give its family a renderer", r.ID)
				}
			}
			golden(t, r.ID+".json", js.stdout)
		})
	}
}

func selfOf(hints []string) string {
	for _, h := range hints {
		if strings.HasPrefix(h, "self=") {
			return strings.TrimPrefix(h, "self=")
		}
	}
	return ""
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(n)) }

func fmtInt(n int) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestParseLocation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "ci.yml")
	_ = os.WriteFile(file, []byte("on: push\n"), 0o644)
	_ = os.Mkdir(filepath.Join(dir, "needs-unknown"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "needs-unknown", "x"), nil, 0o644)
	cases := []struct {
		arg     string
		want    explain.Location
		wantErr string
	}{
		{"ci.yml:12", explain.Location{File: file, Line: 12}, ""},
		{"ci.yml:12:5", explain.Location{File: file, Line: 12, Column: 5}, ""},
		{file + ":3", explain.Location{File: file, Line: 3}, ""},
		{"ci.yml", explain.Location{File: file}, ""},
		{"ci.yml:x", explain.Location{}, "line must be a positive number"},
		{"ci.yml:1:y", explain.Location{}, "column must be a positive number"},
		{"missing.yml:1", explain.Location{}, "not a rule id or a file"},
		{"acme/shared/.github/workflows/lint.yml@v2:7", explain.Location{File: "acme/shared/.github/workflows/lint.yml@v2", Remote: true, Line: 7}, ""},
		{"actions/checkout@v4:3:1", explain.Location{File: "actions/checkout@v4", Remote: true, Line: 3, Column: 1}, ""},
		{"needs-unknown:1", explain.Location{}, "is a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			t.Parallel()
			got, err := explain.ParseLocation(tc.arg, dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestRuleArgumentDelegates(t *testing.T) {
	kit := testkit.New(t, testdata)
	r := run(t, kit, t.TempDir(), "--no-config", "needs-self")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "needs-self  family: jobs") {
		t.Fatalf("exit %d:\n%s", r.code, r.stdout)
	}
	r = run(t, kit, t.TempDir(), "--no-config", "uses-fetch-failed")
	if r.code != 0 || !strings.Contains(r.stdout, "is an alias of 'fetch-failed'") {
		t.Fatalf("alias: exit %d:\n%s", r.code, r.stdout)
	}
	r = run(t, kit, t.TempDir(), "needs-unkown")
	if r.code != 1 || !strings.Contains(r.stderr, "did you mean") {
		t.Fatalf("unknown id: exit %d %s", r.code, r.stderr)
	}
	// A file named like a rule id needs the ./ prefix.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "needs-self"), []byte("on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    needs: [a]\n    steps:\n      - run: echo ok\nname: T1\npermissions: read-all\n"), 0o644)
	r = run(t, kit, dir, "--brief", "./needs-self:5")
	if r.code != 0 || !strings.Contains(r.stdout, "needs-self") || !strings.Contains(r.stdout, "context:") {
		t.Fatalf("./ prefix: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const unwritten = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: |
          VERSION=$(git describe --tags)
          echo "version=$VERSION"
          echo "sha=$(git rev-parse HEAD)" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
name: T2
permissions: read-all
`

func TestRunBlockRedirect(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{".github/workflows/ci.yml": unwritten})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:9")
	if r.code != 0 || !strings.Contains(r.stdout, "line 9 is inside the run: block starting at :7") || !strings.Contains(r.stdout, "bash-output-unredirected") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
}

func TestSeveralFindingsAndColumn(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{".github/workflows/ci.yml": unwritten})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:11")
	if r.code != 0 || !strings.Contains(r.stdout, "step-output-unwritten") || !strings.Contains(r.stdout, "see also: bash-output-unredirected fired on :7") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
	if !strings.Contains(r.stdout, "← key 'version' seen here, not written") || !strings.Contains(r.stdout, "writes GITHUB_OUTPUT key 'sha' (definite)") {
		t.Fatalf("statement list:\n%s", r.stdout)
	}
	// --rule filters; a wrong rule on the line yields the nearby list.
	r = run(t, kit, root, "--brief", "--rule", "needs-unknown", ".github/workflows/ci.yml:11")
	if r.code != 1 || !strings.Contains(r.stdout, "no finding at :11") {
		t.Fatalf("filtered: exit %d\n%s", r.code, r.stdout)
	}
}

func TestNearbyAndNoFindings(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{
		".github/workflows/ci.yml":    unwritten,
		".github/workflows/clean.yml": "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\nname: T3\npermissions: read-all\n",
	})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:2")
	if r.code != 1 || !strings.Contains(r.stdout, "no finding at :2") || !strings.Contains(r.stdout, "nearest findings:") || !strings.Contains(r.stdout, "ci.yml:7:14") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
	r = run(t, kit, root, "--brief", ".github/workflows/clean.yml:3")
	if r.code != 1 || !strings.Contains(r.stdout, "no findings in") {
		t.Fatalf("clean: exit %d\n%s", r.code, r.stdout)
	}
}

func TestOpenTargetWhyPassed(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{".github/workflows/ci.yml": "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - id: gen\n        run: ./scripts/generate.sh\n      - run: echo ${{ steps.gen.outputs.list }}\nname: T4\npermissions: read-all\n"})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:8")
	if r.code != 1 || !strings.Contains(r.stdout, "OPEN GITHUB_OUTPUT target") || !strings.Contains(r.stdout, "opaque: external script") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
}

func TestSuppressedStillExplained(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{".github/workflows/ci.yml": "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    needs: nope  # jobsworth-ignore needs-unknown -- generated job\n    steps:\n      - run: echo ok\nname: T5\npermissions: read-all\n"})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:5")
	if r.code != 0 || !strings.Contains(r.stdout, `(suppressed on :5 — "generated job")`) || !strings.Contains(r.stdout, "needs-unknown") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
}

func TestCalleeFindingsListed(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{
		".github/workflows/ci.yml":         "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./.github/actions/dirty\n        with:\n          used: 1\nname: T6\npermissions: read-all\n",
		".github/actions/dirty/action.yml": "name: Dirty\ninputs:\n  used: {}\n  unused: {}\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ${{ inputs.used }} ${{ inputs.typo }}\ndescription: D\n",
	})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:7")
	if r.code != 1 || !strings.Contains(r.stdout, "the callee at this line has 2 finding(s)") || !strings.Contains(r.stdout, "jobsworth explain .github/actions/dirty/action.yml:") {
		t.Fatalf("exit %d\n%s", r.code, r.stdout)
	}
	// A fetched callee display name is accepted as a location.
	r = run(t, kit, root, "--brief", "acme/shared/.github/workflows/lint.yml@v2:8")
	if r.code != 0 || !strings.Contains(r.stdout, "input-unused") {
		t.Fatalf("remote location: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}

func TestLast(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{
		".github/workflows/a.yml": "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    needs: nope\n    steps:\n      - run: echo ok\nname: T7\npermissions: read-all\n",
		".github/workflows/b.yml": unwritten,
	})
	env := map[string]string{"JOBSWORTH_CACHE_DIR": kit.CacheDir, "JOBSWORTH_API_URL": kit.Server.URL, "JOBSWORTH_TOKEN": "t"}
	getenv := func(k string) string { return env[k] }

	// No run yet: exit 2.
	if r := run(t, kit, root, "--last"); r.code != 2 || !strings.Contains(r.stderr, "no recorded check run") {
		t.Fatalf("no run: exit %d %s", r.code, r.stderr)
	}
	// A terminal check numbers findings and records the run.
	args, flags, _ := cli.ParseArgs(checkcmd.Schema, nil)
	var out, errb bytes.Buffer
	_ = checkcmd.RunWith(args, flags, checkcmd.Env{Cwd: root, Home: kit.Home, Getenv: getenv, Stdout: &out, Stderr: &errb, StderrIsTerminal: true, StdoutIsTerminal: true})
	if !strings.Contains(out.String(), "[1] ") || !strings.Contains(out.String(), "[3] ") {
		t.Fatalf("check should number findings on a terminal:\n%s", out.String())
	}
	rec, ok := cache.ReadLastRun(cache.New(kit.CacheDir), root)
	if !ok || len(rec.Findings) != 4 || len(rec.Entries) != 2 || !rec.Completed {
		t.Fatalf("record: %+v %v", rec, ok)
	}
	// --last <n> indexes across entries in printed order: [1] a.yml, then
	// b.yml's findings by severity.
	r := run(t, kit, root, "--brief", "--last", "2")
	if r.code != 0 || !strings.Contains(r.stdout, "step-output-unwritten") || !strings.Contains(r.stdout, "b.yml:11") {
		t.Fatalf("--last 2: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = run(t, kit, root, "--brief", "--last", "3")
	if r.code != 0 || !strings.Contains(r.stdout, "bash-output-unredirected") {
		t.Fatalf("--last 3: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	r = run(t, kit, root, "--brief", "--last")
	if r.code != 0 || strings.Count(r.stdout, "context:") != 4 || !strings.HasPrefix(r.stdout, "4 finding(s) in the last run") {
		t.Fatalf("--last all: exit %d\n%s", r.code, r.stdout)
	}
	if strings.Index(r.stdout, "[1]") > strings.Index(r.stdout, "[2]") {
		t.Fatalf("errors first, printed order within:\n%s", r.stdout)
	}
	if r := run(t, kit, root, "--last", "9"); r.code != 2 {
		t.Fatalf("out of range: exit %d", r.code)
	}
	// Stale finding: the file was fixed since.
	_ = os.WriteFile(filepath.Join(root, ".github", "workflows", "a.yml"), []byte("on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\nname: T8\npermissions: read-all\n"), 0o644)
	r = run(t, kit, root, "--brief", "--last", "1")
	if r.code != 1 || !strings.Contains(r.stderr, "no longer occurs") {
		t.Fatalf("stale: exit %d %s\n%s", r.code, r.stderr, r.stdout)
	}
}

func TestContextKinds(t *testing.T) {
	kit := testkit.New(t, testdata)
	kinds := map[string]bool{}
	for _, r := range rules.Rules {
		data, err := os.ReadFile(filepath.Join(testdata, "golden", "explain", r.ID+".json"))
		if err != nil {
			t.Skip("goldens not generated")
		}
		var doc struct {
			Findings []struct {
				Context map[string]any `json:"context"`
			} `json:"findings"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		for _, f := range doc.Findings {
			kinds[f.Context["kind"].(string)] = true
		}
	}
	var got []string
	for k := range kinds {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"bash", "call", "callee", "checkout", "condition", "expression", "fetch", "generic", "inputs", "jobs", "performance", "reachability", "script", "secrets", "security", "shape", "step", "step-output", "structure", "suppress", "trigger", "values"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("context kinds = %v want %v", got, want)
	}
	_ = kit
}

// TestRunBlockStatementLines is explain review D1: inside a `run: |`
// block every statement is one file line below the indicator, and the
// arrow marker lands on the statement the finding is about.
func TestRunBlockStatementLines(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{".github/workflows/ci.yml": "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - id: meta\n        run: |\n          VERSION=$(git describe --tags)\n          echo \"version=$VERSION\"\n          echo \"sha=$(git rev-parse HEAD)\" >> \"$GITHUB_OUTPUT\"\n      - run: echo ${{ steps.meta.outputs.version }}\nname: T9\npermissions: read-all\n"})
	r := run(t, kit, root, "--brief", "--rule", "step-output-unwritten", ".github/workflows/ci.yml:11")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{":8     VERSION=", ":9     echo \"version=$VERSION\"", ":10    echo \"sha="} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.Contains(line, "←") && !strings.Contains(line, "echo \"version=$VERSION\"") {
			t.Errorf("arrow on the wrong statement: %s", line)
		}
	}
	if !strings.Contains(r.stdout, "←") {
		t.Errorf("no arrow marker:\n%s", r.stdout)
	}
	// The bash-family finding on the same step marks the unredirected
	// echo too (explain review 2 N1).
	r = run(t, kit, root, "--brief", "--rule", "bash-output-unredirected", ".github/workflows/ci.yml:9")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	marked := false
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.Contains(line, "←") {
			marked = true
			if !strings.Contains(line, "echo \"version=$VERSION\"") || !strings.Contains(line, "echoed here, not redirected") {
				t.Errorf("arrow on the wrong statement: %s", line)
			}
		}
	}
	if !marked {
		t.Errorf("bash-output-unredirected has no arrow:\n%s", r.stdout)
	}
}

// TestSecretCallersListed is explain review N1: the secrets block names
// the known callers and whether each passes secrets: inherit.
func TestSecretCallersListed(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := writeRepo(t, map[string]string{
		".github/workflows/deploy.yml":  "on:\n  workflow_call:\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ${{ secrets.TOKEN }}\nname: T10\n",
		".github/workflows/release.yml": "on: push\njobs:\n  deploy:\n    uses: ./.github/workflows/deploy.yml\n    secrets: inherit\nname: T11\npermissions: read-all\n",
		".github/workflows/pr.yml":      "on: pull_request\njobs:\n  check:\n    uses: ./.github/workflows/deploy.yml\nname: T12\npermissions: read-all\n",
	})
	r := run(t, kit, root, "--brief", ".github/workflows/deploy.yml:7")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{"callers        ", ".github/workflows/release.yml job deploy (secrets: inherit)", ".github/workflows/pr.yml job check (explicit secrets)", "warning  secret-undeclared"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	js := run(t, kit, root, "--brief", "--format", "json", ".github/workflows/deploy.yml:7")
	if !strings.Contains(js.stdout, `"inherit": true`) || !strings.Contains(js.stdout, `"callers"`) {
		t.Errorf("json should carry callers:\n%s", js.stdout)
	}
}

// TestCheckoutWrapperContext is checkout-inference §5: the checkout block
// shows a wrapper's chain and wiring, and the unknown case.
func TestCheckoutWrapperContext(t *testing.T) {
	kit := testkit.New(t, testdata)
	greet := "name: Greet\ndescription: D\ninputs:\n  name:\n    required: true\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hi\n"
	root := writeRepo(t, map[string]string{
		".github/actions/greet/action.yml": greet,
		".github/workflows/ci.yml":         "name: T\non: push\npermissions: read-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: acme/wrappers/checkout-cached@v1\n        with:\n          sparse-checkout: |\n            src\n      - uses: ./.github/actions/greet\n        with:\n          name: world\n",
	})
	r := run(t, kit, root, "--brief", ".github/workflows/ci.yml:12")
	if r.code > 1 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{"acme/wrappers/checkout-cached@v1", "via actions/checkout@v4 at its step 2", "path:         caller's with.path", "repository:   caller's with.repository", "(inputs.repository, default github.repository)", "sparse:       caller's with.sparse-checkout", "sparse list of active root checkout: [src]"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	golden(t, "checkout_wrapper.txt", r.stdout)

	root = writeRepo(t, map[string]string{
		".jobsworth.yml":                   "schema: 1\nrules: { checkout-ref-unresolvable: note }\n",
		".github/actions/greet/action.yml": greet,
		".github/workflows/ci.yml":         "name: T\non: push\npermissions: read-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: acme/forbidden/checkout@v1\n      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n      - uses: ./.github/actions/greet\n        with:\n          name: world\n",
	})
	r = run(t, kit, root, "--brief", "--rule", "checkout-ref-unresolvable", ".github/workflows/ci.yml:9")
	if r.code > 1 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "→ unknown  (") || !strings.Contains(r.stdout, "checkout state unknown after step #1; local-action rules are not applied to later steps") {
		t.Errorf("unknown case not explained:\n%s", r.stdout)
	}
	golden(t, "checkout_unknown.txt", r.stdout)
}

// TestExplainCheckouts is checkout-inference review 1 N2: the checkout
// block of a job without a finding to hang it on.
func TestExplainCheckouts(t *testing.T) {
	kit := testkit.New(t, testdata)
	greet := "name: Greet\ndescription: D\ninputs:\n  name:\n    required: true\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hi\n"
	root := writeRepo(t, map[string]string{
		".github/actions/greet/action.yml": greet,
		".github/workflows/ci.yml":         "name: T\non: push\npermissions: read-all\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: acme/wrappers/checkout-cached@v1\n      - uses: ./.github/actions/greet\n        with:\n          name: world\n  other:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make\n",
	})
	r := run(t, kit, root, "--checkouts", "build", ".github/workflows/ci.yml")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{"job 'build'", "acme/wrappers/checkout-cached@v1", "via actions/checkout@v4 at its step 2", "← active root checkout", "at the end of the job: root checkout from step #1"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	golden(t, "checkouts_job.txt", r.stdout)
	r = run(t, kit, root, "--checkouts", "other", ".github/workflows/ci.yml")
	if r.code != 0 || !strings.Contains(r.stdout, "no root checkout") {
		t.Fatalf("job without checkouts: exit %d\n%s", r.code, r.stdout)
	}
	r = run(t, kit, root, "--checkouts", "nope", ".github/workflows/ci.yml")
	if r.code != 1 || !strings.Contains(r.stderr, "no job named 'nope' (jobs: build, other)") {
		t.Fatalf("unknown job: exit %d\n%s", r.code, r.stderr)
	}
	r = run(t, kit, root, "--checkouts", "build", "--format", "json", ".github/workflows/ci.yml")
	if r.code != 0 || !strings.Contains(r.stdout, "\"via\"") || !strings.Contains(r.stdout, "\"active\"") {
		t.Fatalf("json: exit %d\n%s", r.code, r.stdout)
	}
}
