package rules

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/cli"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

func run(t *testing.T, tokens ...string) (stdout, stderr string, code int) {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		t.Fatalf("parse %q: %v", tokens, err)
	}
	var out, errb bytes.Buffer
	cwd := t.TempDir() // no config unless --config is given
	err = RunWith(args, flags, Env{Cwd: cwd, Home: t.TempDir(), Stdout: &out, Stderr: &errb})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	abs, _ := filepath.Abs(filepath.Join(testdata, "rules"))
	return strings.ReplaceAll(out.String(), abs, "<ROOT>"), errb.String(), cli.ExitCode(err, cli.ExitFindings)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", "rules", name)
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

func TestGolden(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(testdata, "rules", ".jobsworth.yml")
	cases := []struct {
		name   string
		tokens []string
		golden string
	}{
		{"list_no_config", []string{"--no-config"}, "list_no_config.txt"},
		{"list_json_no_config", []string{"--no-config", "--format", "json"}, "list_no_config.json"},
		{"list_with_config", []string{"--config", cfg}, "list_with_config.txt"},
		{"list_json_with_config", []string{"--config", cfg, "--format", "json"}, "list_with_config.json"},
		{"list_family", []string{"--no-config", "--family", "jobs"}, "list_family.txt"},
		{"list_two_families", []string{"--no-config", "--family", "inputs", "--family", "calls"}, "list_two_families.txt"},
		{"list_severity_off", []string{"--config", cfg, "--severity", "off"}, "list_severity_off.txt"},
		{"list_disabled", []string{"--config", cfg, "--disabled"}, "list_disabled.txt"},
		{"list_changed", []string{"--config", cfg, "--changed"}, "list_changed.txt"},
		{"list_markdown", []string{"--config", cfg, "--format", "markdown"}, "list_markdown.md"},
		{"explain", []string{"--no-config", "needs-output-undeclared"}, "explain.txt"},
		{"explain_with_config", []string{"--config", cfg, "input-unused"}, "explain_with_config.txt"},
		{"explain_alias", []string{"--no-config", "uses-fetch-failed"}, "explain_alias.txt"},
		{"explain_json", []string{"--no-config", "--format", "json", "needs-self"}, "explain.json"},
		{"explain_markdown", []string{"--no-config", "--format", "markdown", "needs-self"}, "explain.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := run(t, tc.tokens...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			golden(t, tc.golden, stdout)
		})
	}
}

func TestUnknownRuleExitsOne(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := run(t, "--no-config", "needs-unkown")
	if code != 1 || stdout != "" {
		t.Fatalf("exit %d stdout %q", code, stdout)
	}
	if !strings.Contains(stderr, `unknown rule "needs-unkown"`) || !strings.Contains(stderr, "needs-unknown") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()
	_, _, code := run(t, "--no-config", "--family", "nope")
	if code != 2 {
		t.Fatalf("unknown family: exit %d", code)
	}
	_, _, code = run(t, "--config", filepath.Join(testdata, "nope.yml"))
	if code != 2 {
		t.Fatalf("missing config: exit %d", code)
	}
}

func TestFiltersAgree(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(testdata, "rules", ".jobsworth.yml")
	off, _, _ := run(t, "--config", cfg, "--severity", "off")
	disabled, _, _ := run(t, "--config", cfg, "--disabled")
	if off != disabled {
		t.Fatalf("--severity off and --disabled differ:\n%s\n%s", off, disabled)
	}
	enabled, _, _ := run(t, "--config", cfg, "--enabled")
	if strings.Contains(enabled, "uses-dynamic") {
		t.Fatal("--enabled listed a disabled rule")
	}
	changed, _, _ := run(t, "--config", cfg, "--changed")
	for _, id := range []string{"input-unused", "needs-unknown", "uses-dynamic"} {
		if !strings.Contains(changed, id) {
			t.Errorf("--changed missing %s", id)
		}
	}
	if strings.Contains(changed, "needs-self") {
		t.Error("--changed listed an unchanged rule")
	}
}

// TestDocMatchesCommitted is the drift check from spec-rules §4.4: the
// committed docs/rules.md must equal `rules --doc --no-config`.
func TestDocMatchesCommitted(t *testing.T) {
	t.Parallel()
	got, _, code := run(t, "--doc", "--no-config")
	if code != 0 {
		t.Fatal("--doc failed")
	}
	path := filepath.Join("..", "..", "docs", "rules.md")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test ./cmd/rules -update`)", err)
	}
	if string(want) != got {
		t.Fatalf("docs/rules.md is stale: run `go test ./cmd/rules -update` and commit the result")
	}
}
