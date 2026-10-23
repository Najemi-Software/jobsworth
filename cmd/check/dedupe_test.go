package check

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

// TestCalleeFindingsPrintedOnce is medium review 3 N11: a local action
// reached from two entries has its finding printed under the first entry
// only, counted once, and carried once in json with reached_from.
func TestCalleeFindingsPrintedOnce(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := t.TempDir()
	files := map[string]string{
		".github/workflows/a.yml":          "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./.github/actions/dirty\n        with:\n          used: 1\nname: T1\npermissions: read-all\n",
		".github/workflows/b.yml":          "on: push\njobs:\n  b:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./.github/actions/dirty\n        with:\n          used: 1\nname: T2\npermissions: read-all\n",
		".github/actions/dirty/action.yml": "name: Dirty\ninputs:\n  used: {}\n  unused: {}\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ${{ inputs.used }}\ndescription: D\n",
		// A reusable workflow whose entry sorts after its caller (review 4 D11).
		".github/workflows/zz-reuse.yml": "on:\n  workflow_call:\njobs:\n  d:\n    runs-on: ubuntu-latest\n    steps:\n      - run: x=${{ secrets.MISSING }}\nname: T3\n",
		".github/workflows/0-caller.yml": "on: push\njobs:\n  c:\n    uses: ./.github/workflows/zz-reuse.yml\nname: T4\npermissions: read-all\n",
	}
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"JOBSWORTH_CACHE_DIR": kit.CacheDir, "JOBSWORTH_API_URL": kit.Server.URL, "JOBSWORTH_TOKEN": "test-token"}
	exec := func(tokens ...string) (string, int) {
		args, flags, err := cli.ParseArgs(Schema, tokens)
		if err == nil {
			err = cli.Validate(Schema, args, flags)
		}
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err = RunWith(args, flags, Env{Cwd: root, Home: kit.Home, Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &bytes.Buffer{}, StdoutIsTerminal: true})
		return out.String(), cli.ExitCode(err, cli.ExitFindings)
	}
	text, _ := exec("--fail-on", "warning", ".github/workflows/a.yml", ".github/workflows/b.yml")
	if strings.Count(text, "input-unused") != 1 {
		t.Errorf("the callee's finding should print once:\n%s", text)
	}
	if !strings.Contains(text, "(1 finding reported under .github/workflows/a.yml)") {
		t.Errorf("the repeat should point at the first entry:\n%s", text)
	}
	if !strings.Contains(text, "[1] ") || strings.Contains(text, "[2] ") {
		t.Errorf("only the printed finding is numbered:\n%s", text)
	}
	if !strings.Contains(text, "2 entries, 2 failed, 0 errors, 1 warning") {
		t.Errorf("summary should count the finding once and both entries as failed:\n%s", text)
	}

	// With the action itself among the entries, its own listing seeds the
	// memory and no entry repeats it.
	all, _ := exec("--fail-on", "warning")
	if strings.Count(all, "input-unused") != 1 || strings.Count(all, "secret-undeclared") != 1 || !strings.Contains(all, "5 entries, 5 failed, 1 error, 1 warning") {
		t.Errorf("every entry, callees once:\n%s", all)
	}

	// A callee entry sorting after its caller (review 4 D11): the finding
	// is reported under the callee's own listing, once, and the caller
	// points at it.
	ordered, _ := exec("--fail-on", "warning", ".github/workflows/0-caller.yml", ".github/workflows/zz-reuse.yml")
	if strings.Count(ordered, "secret-undeclared") != 1 {
		t.Errorf("callee entry after its caller should print once:\n%s", ordered)
	}
	if !strings.Contains(ordered, "(1 finding reported under .github/workflows/zz-reuse.yml)") || !strings.Contains(ordered, "FAIL .github/workflows/zz-reuse.yml\n  [1] .github/workflows/zz-reuse.yml:7:") {
		t.Errorf("the finding belongs to the callee's own listing:\n%s", ordered)
	}
	if !strings.Contains(ordered, "2 entries, 2 failed, 1 error, 0 warnings") {
		t.Errorf("summary should count it once:\n%s", ordered)
	}

	js, _ := exec("--format", "json", ".github/workflows/a.yml", ".github/workflows/b.yml")
	var doc struct {
		Entries []struct {
			Path     string
			Findings []struct {
				Rule        string
				ReachedFrom []string `json:"reached_from"`
			}
		}
		Summary struct{ Warnings int }
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 2 || len(doc.Entries[0].Findings) != 1 || len(doc.Entries[1].Findings) != 0 || doc.Summary.Warnings != 1 {
		t.Fatalf("json: %s", js)
	}
	if from := doc.Entries[0].Findings[0].ReachedFrom; len(from) != 2 || filepath.Base(from[0]) != "a.yml" || filepath.Base(from[1]) != "b.yml" {
		t.Errorf("reached_from = %v", from)
	}
}

// TestInputSeeds is advanced §4.3: `check --input k=v` seeds the entry's
// workflow_dispatch inputs so dead conditions show for that dispatch.
func TestInputSeeds(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := t.TempDir()
	wf := filepath.Join(root, ".github", "workflows", "d.yml")
	if err := os.MkdirAll(filepath.Dir(wf), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: D\non:\n  workflow_dispatch:\n    inputs:\n      dry-run:\n        type: string\npermissions: read-all\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - if: inputs.dry-run == 'false'\n        run: echo real ${{ inputs.dry-run }}\n"
	if err := os.WriteFile(wf, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"JOBSWORTH_CACHE_DIR": kit.CacheDir, "JOBSWORTH_API_URL": kit.Server.URL, "JOBSWORTH_TOKEN": "test-token"}
	exec := func(tokens ...string) string {
		args, flags, err := cli.ParseArgs(Schema, tokens)
		if err == nil {
			err = cli.Validate(Schema, args, flags)
		}
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		_ = RunWith(args, flags, Env{Cwd: root, Home: kit.Home, Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &bytes.Buffer{}})
		return out.String()
	}
	if got := exec(".github/workflows/d.yml"); strings.Contains(got, "if-dead-for-all-callers") {
		t.Fatalf("no seed, no finding:\n%s", got)
	}
	got := exec("--input", "dry-run=true", ".github/workflows/d.yml")
	if !strings.Contains(got, "if-dead-for-all-callers") || !strings.Contains(got, "the seeded inputs") {
		t.Fatalf("seeded dispatch should show the dead condition:\n%s", got)
	}
	if got := exec("--input", "dry-run=false", ".github/workflows/d.yml"); strings.Contains(got, "if-dead-for-all-callers") {
		t.Fatalf("satisfying seed:\n%s", got)
	}
}
