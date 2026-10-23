package graph

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

// appRepo copies testdata/golden/input's workflows into a temp directory
// (outside any git checkout, so the repo root and slug are deterministic)
// and adds a caller of the linked tools workflow.
func appRepo(t *testing.T, kit *testkit.Kit) string {
	t.Helper()
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(kit.Testdata, "golden", "input", ".github", "workflows")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wf, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func run(t *testing.T, kit *testkit.Kit, cwd string, tokens ...string) (stdout, stderr string, code int) {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		return "", err.Error(), cli.ExitUsage
	}
	env := map[string]string{
		"JOBSWORTH_CACHE_DIR": t.TempDir(), // fresh per invocation: cache status is deterministic
		"JOBSWORTH_API_URL":   kit.Server.URL,
		"JOBSWORTH_TOKEN":     "test-token",
	}
	var out, errb bytes.Buffer
	err = RunWith(args, flags, checkcmd.Env{Cwd: cwd, Home: kit.Home, Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &errb})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	stable := func(s string) string {
		s = strings.ReplaceAll(s, cwd, "<ROOT>")
		return strings.ReplaceAll(s, kit.Testdata, "<TESTDATA>")
	}
	return stable(out.String()), stable(errb.String()), cli.ExitCode(err, cli.ExitFindings)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", "graph", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func TestSchema(t *testing.T) {
	kit := testkit.New(t, testdata)
	cwd := appRepo(t, kit)
	for _, tc := range []struct {
		name   string
		tokens []string
		want   string
	}{
		{"no args", nil, "expected"},
		{"two args", []string{"a.yml", "b.yml"}, "expected"},
		{"glob", []string{".github/workflows/*.yml"}, "not a glob"},
		{"bad format", []string{"--format", "svg", "x.yml"}, "format"},
		{"bad depth", []string{"--depth", "two", ".github/workflows/ok.yml"}, "--depth"},
		{"negative depth", []string{"--depth", "-1", ".github/workflows/ok.yml"}, "--depth"},
		{"missing entry", []string{"nope.yml"}, "nope.yml"},
	} {
		_, stderr, code := run(t, kit, cwd, tc.tokens...)
		if code != cli.ExitUsage || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: code %d stderr %q (want usage error mentioning %q)", tc.name, code, stderr, tc.want)
		}
	}
}

func TestGoldens(t *testing.T) {
	kit := testkit.New(t, testdata)
	cwd := appRepo(t, kit)
	linked := filepath.Join(kit.Testdata, "repos", "acme-tools")
	for _, tc := range []struct {
		name   string
		tokens []string
	}{
		{"cli_ok_tree.txt", []string{".github/workflows/ok.yml"}},
		{"cli_ok_json.json", []string{"--format", "json", ".github/workflows/ok.yml"}},
		{"cli_ok_dot.txt", []string{"--format", "dot", ".github/workflows/ok.yml"}},
		{"cli_ok_mermaid.txt", []string{"--format", "mermaid", ".github/workflows/ok.yml"}},
		{"cli_ok_no_fetch.txt", []string{"--no-fetch", ".github/workflows/ok.yml"}},
		{"cli_ok_depth0.txt", []string{"--depth", "0", ".github/workflows/ok.yml"}},
		{"cli_note_dynamic.txt", []string{".github/workflows/note.yml"}},
		{"cli_linked.txt", []string{"--repo", "acme/tools=" + linked, ".github/workflows/linked.yml"}},
		{"cli_linked_with.json", []string{"--with", "--format", "json", "--repo", "acme/tools=" + linked, ".github/workflows/linked.yml"}},
		{"cli_linked_with.txt", []string{"--with", "--repo", "acme/tools=" + linked, ".github/workflows/linked.yml"}},
		{"cli_callers_x.txt", []string{"--callers", "--repo", "acme/tools=" + linked, filepath.Join(linked, ".github/workflows/x.yml")}},
		{"cli_callers_none.txt", []string{"--callers", ".github/workflows/ok.yml"}},
		{"cli_callers_json.json", []string{"--callers", "--format", "json", "--repo", "acme/tools=" + linked, filepath.Join(linked, ".github/workflows/x.yml")}},
	} {
		stdout, stderr, code := run(t, kit, cwd, tc.tokens...)
		if code != 0 {
			t.Errorf("%s: exit %d, stderr: %s", tc.name, code, stderr)
		}
		golden(t, tc.name, stdout)
		if strings.HasSuffix(tc.name, ".json") {
			var v map[string]any
			if err := json.Unmarshal([]byte(stdout), &v); err != nil {
				t.Errorf("%s: invalid json: %v", tc.name, err)
			}
			if v["schema"] != float64(1) {
				t.Errorf("%s: schema must be 1", tc.name)
			}
		}
	}
}

// TestFailuresExitZero: fetch and parse failures are part of the graph,
// not an error (spec-graph §2.2).
func TestFailuresExitZero(t *testing.T) {
	kit := testkit.New(t, testdata)
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wf, "f.yml")
	if err := os.WriteFile(path, []byte("on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: acme/forbidden@v1\n      - uses: acme/broken@v1\nname: T1\npermissions: read-all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, kit, dir, path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "(failed: ") || !strings.Contains(stdout, "2 failed") {
		t.Errorf("failures should be nodes:\n%s", stdout)
	}
}

// TestLocalInGitCheckout is spec-graph review D1: in a real checkout with
// a GitHub remote the implicit link for the entry's own repository must
// not turn its files into "linked" nodes.
func TestLocalInGitCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	kit := testkit.New(t, testdata)
	dir := t.TempDir()
	for p, body := range map[string]string{
		".github/workflows/ci.yml":         "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./.github/actions/greet\nname: T2\npermissions: read-all\n",
		".github/actions/greet/action.yml": "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hi\nname: N\ndescription: D\n",
	} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/acme/app.git"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	stdout, stderr, code := run(t, kit, dir, ".github/workflows/ci.yml")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "./.github/actions/greet  (composite-action, local)") || strings.Contains(stdout, "linked") {
		t.Errorf("own-repo file should be local:\n%s", stdout)
	}
	js, _, _ := run(t, kit, dir, "--format", "json", ".github/workflows/ci.yml")
	var doc struct {
		Nodes   map[string]struct{ Source, Repo string }
		Summary struct {
			BySource map[string]int `json:"by_source"`
		}
	}
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Summary.BySource["local"] < 1 || doc.Summary.BySource["linked"] != 0 {
		t.Errorf("by_source: %v", doc.Summary.BySource)
	}
	for _, n := range doc.Nodes {
		if n.Source == "local" && n.Repo != "acme/app" {
			t.Errorf("local node should carry the repo slug, got %q", n.Repo)
		}
	}
}

// TestRateLimitedWalk is spec-graph §7: a rate-limited fetch becomes a
// failed node, the warning goes to stderr, and the exit code stays 0.
func TestRateLimitedWalk(t *testing.T) {
	kit := testkit.New(t, testdata)
	dir := t.TempDir()
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wf, "r.yml")
	if err := os.WriteFile(path, []byte("on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: acme/limited@v1\n      - uses: actions/checkout@v4\nname: T3\npermissions: read-all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, kit, dir, path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "acme/limited@v1  (failed: ") || !strings.Contains(strings.ToLower(stdout), "rate limit") {
		t.Errorf("rate-limited node should be failed with the rate-limit message:\n%s", stdout)
	}
	if !strings.Contains(stderr, "rate limit exceeded") {
		t.Errorf("stderr should carry the rate-limit warning: %s", stderr)
	}
	// Once the limit is hit, the remaining external nodes fail too (§7).
	if !strings.Contains(stdout, "2 failed") || !strings.Contains(stdout, "actions/checkout@v4  (failed: ") {
		t.Errorf("remaining external nodes should fail after the limit:\n%s", stdout)
	}
}
