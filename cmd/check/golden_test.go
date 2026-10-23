package check

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

// run executes RunWith against testdata/golden/input as cwd and returns
// stdout, stderr and the exit code.
func run(t *testing.T, kit *testkit.Kit, terminal bool, tokens ...string) (stdout, stderr string, code int) {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		t.Fatalf("parse %q: %v", tokens, err)
	}
	cwd := filepath.Join(kit.Testdata, "golden", "input")
	env := map[string]string{
		"JOBSWORTH_CACHE_DIR": kit.CacheDir,
		"JOBSWORTH_API_URL":   kit.Server.URL,
		"JOBSWORTH_TOKEN":     "test-token",
	}
	var out, errb bytes.Buffer
	err = RunWith(args, flags, Env{
		Cwd:              cwd,
		Home:             kit.Home,
		Getenv:           func(k string) string { return env[k] },
		GHToken:          nil,
		Stdout:           &out,
		Stderr:           &errb,
		StderrIsTerminal: terminal,
		StdoutIsTerminal: terminal,
	})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	root := cwd
	stable := func(s string) string { return strings.ReplaceAll(s, root, "<ROOT>") }
	return stable(out.String()), stable(errb.String()), cli.ExitCode(err, cli.ExitFindings)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", name)
	if *update {
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

func TestGolden(t *testing.T) {
	kit := testkit.New(t, testdata)
	cases := []struct {
		name   string
		tokens []string
		code   int
		golden string
	}{
		{"single_ok", []string{".github/workflows/ok.yml"}, 0, "single_ok.txt"},
		{"single_fail", []string{".github/workflows/fail.yml"}, 1, "single_fail.txt"},
		{"multi_entry", nil, 1, "multi_entry.txt"},
		{"json", []string{"--format", "json"}, 1, "multi_entry.json"},
		{"fail_on_note", []string{"--fail-on", "note", ".github/workflows/note.yml"}, 1, "fail_on_note.txt"},
		{"note_passes_by_default", []string{".github/workflows/note.yml"}, 0, "note_default.txt"},
		{"fail_on_warning", []string{"--fail-on", "warning", ".github/workflows/fail.yml", ".github/workflows/ok.yml"}, 1, "fail_on_warning.txt"},
		{"suppressed_hidden", []string{".github/workflows/suppressed.yml"}, 0, "suppressed_hidden.txt"},
		{"suppressed_shown", []string{"--show-suppressed", ".github/workflows/suppressed.yml"}, 0, "suppressed_shown.txt"},
		{"suppressed_json", []string{"--format", "json", ".github/workflows/suppressed.yml"}, 0, "suppressed.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := run(t, kit, false, tc.tokens...)
			if code != tc.code {
				t.Errorf("exit = %d want %d\nstderr:\n%s", code, tc.code, stderr)
			}
			golden(t, tc.golden, stdout)
		})
	}
}

func TestProgressOnlyOnTerminal(t *testing.T) {
	kit := testkit.New(t, testdata)
	_, stderr, _ := run(t, kit, false, "--no-cache", ".github/workflows/ok.yml")
	if strings.Contains(stderr, "fetching") || strings.Contains(stderr, "resolving") {
		t.Fatalf("progress printed to a non-terminal:\n%s", stderr)
	}
	_, stderr, _ = run(t, kit, true, "--no-cache", ".github/workflows/ok.yml")
	if !strings.Contains(stderr, "resolving actions/checkout@v4") {
		t.Fatalf("no progress on a terminal:\n%s", stderr)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	kit := testkit.New(t, testdata)
	for _, tokens := range [][]string{
		{"nope.yml"},
		{"missing/*.yml"},
		{"--repo", "acme/tools=/does/not/exist", ".github/workflows/ok.yml"},
		{"--repo", "acme/tools=.", "--repo", "acme/tools=..", ".github/workflows/ok.yml"},
		{"--config", "nope.yml", ".github/workflows/ok.yml"},
	} {
		stdout, stderr, code := run(t, kit, false, tokens...)
		if code != cli.ExitUsage {
			t.Errorf("%q: exit %d want 2 (stderr %q)", tokens, code, stderr)
		}
		if stdout != "" {
			t.Errorf("%q: validated something: %q", tokens, stdout)
		}
	}
}

func TestOfflineAndNoTokenWarning(t *testing.T) {
	kit := testkit.New(t, testdata)
	_, stderr, _ := run(t, kit, false, "--offline", ".github/workflows/ok.yml")
	if strings.Contains(stderr, "no GitHub token") {
		t.Fatalf("offline should not warn about tokens:\n%s", stderr)
	}
	// Drop the token from the environment: warning expected.
	args, flags, _ := cli.ParseArgs(Schema, []string{".github/workflows/ok.yml"})
	var out, errb bytes.Buffer
	_ = RunWith(args, flags, Env{
		Cwd:  filepath.Join(kit.Testdata, "golden", "input"),
		Home: kit.Home,
		Getenv: func(k string) string {
			return map[string]string{"JOBSWORTH_API_URL": kit.Server.URL, "JOBSWORTH_CACHE_DIR": kit.CacheDir}[k]
		},
		Stdout: &out, Stderr: &errb,
	})
	if !strings.Contains(errb.String(), "no GitHub token") {
		t.Fatalf("expected token warning:\n%s", errb.String())
	}
}

func TestExplicitLinkIsReported(t *testing.T) {
	kit := testkit.New(t, testdata)
	linked := filepath.Join(kit.Testdata, "repos", "acme-tools")
	_, stderr, code := run(t, kit, false, "--repo", "acme/tools="+linked, ".github/workflows/linked.yml")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "link: acme/tools → "+linked+" (explicit)") {
		t.Fatalf("link not reported:\n%s", stderr)
	}
}
