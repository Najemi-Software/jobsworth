package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/cli"
)

// runIn executes check in cwd with a non-terminal environment and no
// network, returning stderr and the exit code.
func runIn(t *testing.T, cwd string, tokens ...string) (stdout, stderr string, code int) {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	err = RunWith(args, flags, Env{
		Cwd: cwd, Home: t.TempDir(),
		Getenv: func(k string) string { return map[string]string{"JOBSWORTH_CACHE_DIR": t.TempDir()}[k] },
		Stdout: &out, Stderr: &errb,
	})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	return out.String(), errb.String(), cli.ExitCode(err, cli.ExitFindings)
}

func repoWith(t *testing.T, cfg string) string {
	t.Helper()
	root := t.TempDir()
	wf := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: echo ok\npermissions: read-all\n"), 0o644)
	if cfg != "" {
		_ = os.WriteFile(filepath.Join(root, ".jobsworth.yml"), []byte(cfg), 0o644)
	}
	return root
}

// TestConfigValidationExitsTwo covers spec-init §4.3: each validation rule
// exits 2 with its message before anything is validated.
func TestConfigValidationExitsTwo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cfg, want string
	}{
		{"unknown key", "skip_validation: 1\n", `unknown key "skip_validation" (line 1); did you mean "skip"?`},
		{"schema too new", "schema: 99\n", "newer than this binary supports (1)"},
		{"repos path missing", "repos:\n  acme/tools: ../nope\n", "no such file"},
		{"unknown rule", "rules:\n  input-unusd: off\n", `unknown rule "input-unusd"; did you mean input-unused?`},
		{"bad severity", "rules:\n  input-unused: loud\n", "not off, error, warning or note"},
		{"skip with ref", "skip:\n  actions: [acme/x@v1]\n", "has an @ref"},
		{"bad duration", "cache:\n  tag_ttl: forever\n", "is not a duration"},
		{"bad integer", "fetch:\n  parallel: lots\n", "not a positive integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := repoWith(t, tc.cfg)
			stdout, stderr, code := runIn(t, root)
			if code != cli.ExitUsage {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr %q does not contain %q", stderr, tc.want)
			}
			if stdout != "" {
				t.Fatalf("validated something: %q", stdout)
			}
		})
	}
}

func TestConfigNotesAndWarnings(t *testing.T) {
	t.Parallel()
	linked := t.TempDir() // exists, but has no .github
	root := repoWith(t, "repos:\n  acme/tools: "+linked+"\n")
	_, stderr, code := runIn(t, root, "--offline")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "schema missing, assuming 1") {
		t.Errorf("missing schema note: %s", stderr)
	}
	if !strings.Contains(stderr, "has no .github directory; link is active anyway") {
		t.Errorf("missing no-.github warning: %s", stderr)
	}
	if !strings.Contains(stderr, "link: acme/tools → "+linked+" (explicit)") {
		t.Errorf("link should still be active: %s", stderr)
	}
}

func TestConfigEntriesAndFailOn(t *testing.T) {
	t.Parallel()
	root := repoWith(t, "schema: 1\nentries: [wf/*.yml]\nfail_on: warning\n")
	_ = os.MkdirAll(filepath.Join(root, "wf"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "wf", "x.yml"), []byte("on:\n  workflow_call:\n    inputs:\n      unused: {type: string}\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: echo ok\n"), 0o644)
	stdout, _, code := runIn(t, root, "--offline")
	if code != 1 || !strings.Contains(stdout, "FAIL wf/x.yml") || strings.Contains(stdout, ".github/workflows/ci.yml") {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
	// --fail-on overrides the config.
	_, _, code = runIn(t, root, "--offline", "--fail-on", "error")
	if code != 0 {
		t.Fatalf("flag should override config fail_on, exit %d", code)
	}
}
