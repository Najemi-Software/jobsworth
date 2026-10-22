package initcmd

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

// copyTree copies src into dst recursively.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// sampleRepo builds <parent>/init-sample from testdata plus a git-initialised
// sibling <parent>/acme-tools with origin acme/tools. Skips without git.
func sampleRepo(t *testing.T) (root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	parent := t.TempDir()
	root = filepath.Join(parent, "init-sample")
	copyTree(t, filepath.Join(testdata, "repos", "init-sample"), root)
	sib := filepath.Join(parent, "acme-tools")
	copyTree(t, filepath.Join(testdata, "repos", "acme-tools"), sib)
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/acme/tools.git"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = sib
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return root
}

type result struct {
	stdout, stderr string
	code           int
}

func run(t *testing.T, kit *testkit.Kit, cwd string, stdin string, terminal bool, tokens ...string) result {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		t.Fatalf("parse %q: %v", tokens, err)
	}
	env := map[string]string{}
	if kit != nil {
		env["JOBSWORTH_CACHE_DIR"] = kit.CacheDir
		env["JOBSWORTH_API_URL"] = kit.Server.URL
		env["JOBSWORTH_TOKEN"] = "test-token"
	}
	var out, errb bytes.Buffer
	var in io.Reader = strings.NewReader(stdin)
	Version = "0.1.0"
	err = RunWith(args, flags, Env{
		Cwd: cwd, Home: t.TempDir(), Getenv: func(k string) string { return env[k] },
		Stdin: in, Stdout: &out, Stderr: &errb, StdinIsTerminal: terminal,
	})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	return result{stdout: out.String(), stderr: errb.String(), code: cli.ExitCode(err, cli.ExitFindings)}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", "init", name)
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

func TestPrintGoldens(t *testing.T) {
	root := sampleRepo(t)
	for _, tc := range []struct {
		name   string
		tokens []string
	}{
		{"default.yml", []string{"--print"}},
		{"minimal.yml", []string{"--print", "--minimal"}},
		{"full.yml", []string{"--print", "--full"}},
		{"no_detect.yml", []string{"--print", "--no-detect"}},
		{"workflow.yml", []string{"--print", "--workflow", "--minimal"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, nil, root, "", false, tc.tokens...)
			if r.code != 0 {
				t.Fatalf("exit %d:\n%s", r.code, r.stderr)
			}
			golden(t, tc.name, r.stdout)
			if _, err := os.Stat(filepath.Join(root, config.FileName)); err == nil {
				t.Fatal("--print wrote a file")
			}
		})
	}
}

func TestPrintOutputsParse(t *testing.T) {
	root := sampleRepo(t)
	for _, mode := range []string{"--minimal", "--full", ""} {
		tokens := []string{"--print"}
		if mode != "" {
			tokens = append(tokens, mode)
		}
		r := run(t, nil, root, "", false, tokens...)
		c, err := config.Parse([]byte(r.stdout), nil)
		if err != nil {
			t.Fatalf("%s output does not parse: %v", mode, err)
		}
		if c.Repos["acme/tools"] != "../acme-tools" {
			t.Errorf("%s: link not written: %v", mode, c.Repos)
		}
	}
}

func TestWritesFileAndWorkflow(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := sampleRepo(t)
	r := run(t, kit, root, "", false, "--workflow", "--probe")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "Wrote .jobsworth.yml") || !strings.Contains(r.stderr, "Next: jobsworth check") {
		t.Fatalf("stderr:\n%s", r.stderr)
	}
	cfg, err := config.LoadDefault(root, nil)
	if err != nil || cfg.Dir != root {
		t.Fatalf("written config: %+v %v", cfg, err)
	}
	wf := filepath.Join(root, ".github", "workflows", "jobsworth.yml")
	data, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "workflow_file.yml", string(data))

	// The generated workflow passes check with zero findings under the
	// shipped defaults: the tool's own action is SHA-pinned with a version
	// comment (test review 5 D4), so uses-unpinned has nothing to say.
	if !strings.Contains(string(data), "jobsworth-action@"+testkit.SHAFor("najemi-software", "jobsworth-action", "v0")+" # v0") {
		t.Fatalf("generated workflow is not SHA-pinned:\n%s", data)
	}
	cfg = config.Defaults()
	res := check.RunEntry(wf, check.Options{Fetcher: testkit.Fetcher(kit, false), RootOverride: root, Config: cfg})
	if fs := check.AllFindings(res); len(fs) != 0 {
		t.Fatalf("generated workflow has findings: %+v", fs)
	}

	// Existing files are errors without --force; nothing changes.
	r = run(t, kit, root, "", false)
	if r.code != 2 || !strings.Contains(r.stderr, "exists; use --force") {
		t.Fatalf("existing: exit %d %s", r.code, r.stderr)
	}
	r = run(t, kit, root, "", false, "--workflow", "--force")
	if r.code != 0 {
		t.Fatalf("force: exit %d %s", r.code, r.stderr)
	}
}

func TestProbe(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := sampleRepo(t)
	r := run(t, kit, root, "", false, "--print", "--probe", "--minimal")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "403  acme/forbidden@v1") || !strings.Contains(r.stderr, "404  acme/nope@v1") {
		t.Fatalf("probe report:\n%s", r.stderr)
	}
	if !strings.Contains(r.stderr, "404 is probably a bug") {
		t.Fatalf("404 note missing:\n%s", r.stderr)
	}
	c, err := config.Parse([]byte(r.stdout), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SkipActions["acme/forbidden"] {
		t.Errorf("403 not offered as skip: %v", c.SkipActions)
	}
	if c.SkipActions["acme/nope"] {
		t.Error("404 must not be a skip entry")
	}
	if c.SkipActions["acme/tools/git/checkout-with-cache"] {
		t.Error("linked repo must not become a skip entry")
	}
	if !strings.Contains(r.stderr, "403  acme/tools/git/checkout-with-cache@v1") || !strings.Contains(r.stderr, "[linked locally]") {
		t.Fatalf("linked repo should be probed and tagged:\n%s", r.stderr)
	}
	if len(kit.Server.Requests()) == 0 {
		t.Fatal("probe made no requests")
	}
}

// TestProbePrivateLinkedRepoNamedInWorkflow is spec-init review G1: a
// linked repo the token cannot fetch is named above token: in the
// generated workflow, and is not offered as a skip.
func TestProbePrivateLinkedRepoNamedInWorkflow(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := sampleRepo(t)
	r := run(t, kit, root, "", false, "--print", "--probe", "--workflow", "--minimal")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "# Detected private repos: acme/tools.") {
		t.Fatalf("private comment missing:\n%s", r.stdout)
	}
	cfgText := strings.SplitN(r.stdout, "\n# --- ", 2)[0]
	c, err := config.Parse([]byte(cfgText), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.SkipActions["acme/tools/git/checkout-with-cache"] || !c.SkipActions["acme/forbidden"] {
		t.Fatalf("skip list wrong: %v", c.SkipActions)
	}
	if !strings.Contains(r.stderr, "Linked repos that need more than GITHUB_TOKEN in CI: acme/tools") {
		t.Fatalf("stderr:\n%s", r.stderr)
	}
}

func TestProbeAbortsOnRateLimit(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := t.TempDir()
	wf := filepath.Join(root, ".github", "workflows")
	_ = os.MkdirAll(wf, 0o755)
	_ = os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: acme/limited@v1\n      - uses: acme/forbidden@v1\npermissions: read-all\n"), 0o644)
	r := run(t, kit, root, "", false, "--print", "--probe", "--minimal")
	if r.code != 0 || !strings.Contains(r.stderr, "probe aborted") {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, "skip:") {
		t.Fatalf("skip written after abort:\n%s", r.stdout)
	}
}

func TestInteractive(t *testing.T) {
	t.Run("accept all", func(t *testing.T) {
		root := sampleRepo(t)
		r := run(t, nil, root, "y\n", true)
		if r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		c, _ := config.LoadDefault(root, nil)
		if c.Repos["acme/tools"] != "../acme-tools" {
			t.Fatalf("config: %+v", c)
		}
	})
	t.Run("enter accepts defaults", func(t *testing.T) {
		root := sampleRepo(t)
		r := run(t, nil, root, "\n", true)
		if r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		c, _ := config.LoadDefault(root, nil)
		if c.Repos["acme/tools"] != "../acme-tools" {
			t.Fatalf("config: %+v", c)
		}
	})
	t.Run("decline link", func(t *testing.T) {
		root := sampleRepo(t)
		r := run(t, nil, root, "n\n", true)
		if r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		data, _ := os.ReadFile(filepath.Join(root, config.FileName))
		if !strings.Contains(string(data), "# acme/tools: <path>") || strings.Contains(string(data), "acme/tools: ../acme-tools") {
			t.Fatalf("declined link should be commented out:\n%s", data)
		}
		if strings.Contains(string(data), "checkout:") {
			t.Fatalf("init must not write a checkout: section (checkout-inference §4):\n%s", data)
		}
	})
	t.Run("eof writes nothing", func(t *testing.T) {
		root := sampleRepo(t)
		r := run(t, nil, root, "", true)
		if r.code != 1 || !strings.Contains(r.stderr, "nothing written") {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		if _, err := os.Stat(filepath.Join(root, config.FileName)); err == nil {
			t.Fatal("file written after EOF")
		}
	})
	t.Run("force decline", func(t *testing.T) {
		root := sampleRepo(t)
		_ = os.WriteFile(filepath.Join(root, config.FileName), []byte("schema: 1\n# hand edited\n"), 0o644)
		r := run(t, nil, root, "y\nn\n", true, "--force")
		if r.code != 1 {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		if !strings.Contains(r.stderr, "Hand edits will be lost") {
			t.Fatalf("missing warning:\n%s", r.stderr)
		}
		data, _ := os.ReadFile(filepath.Join(root, config.FileName))
		if !strings.Contains(string(data), "hand edited") {
			t.Fatal("file was overwritten after declining")
		}
	})
	t.Run("force accept", func(t *testing.T) {
		root := sampleRepo(t)
		_ = os.WriteFile(filepath.Join(root, config.FileName), []byte("schema: 1\n# hand edited\n"), 0o644)
		r := run(t, nil, root, "y\ny\n", true, "--force")
		if r.code != 0 {
			t.Fatalf("exit %d:\n%s", r.code, r.stderr)
		}
		data, _ := os.ReadFile(filepath.Join(root, config.FileName))
		if strings.Contains(string(data), "hand edited") {
			t.Fatal("file was not regenerated")
		}
	})
}

func TestEmptyRepoGetsTemplateWithoutPrompts(t *testing.T) {
	root := t.TempDir()
	r := run(t, nil, root, "", true)
	if r.code != 0 || strings.Contains(r.stderr, "[Y/n]") {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	data, _ := os.ReadFile(filepath.Join(root, config.FileName))
	if !strings.HasPrefix(string(data), "# .jobsworth.yml") || strings.Contains(string(data), "repos:") {
		t.Fatalf("template:\n%s", data)
	}
}

func TestDirArgumentAndTargetRoot(t *testing.T) {
	root := sampleRepo(t)
	sub := filepath.Join(root, ".github")
	r := run(t, nil, t.TempDir(), "", false, sub)
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	// init-sample is not a git repo, so the config lands in <dir> itself.
	if _, err := os.Stat(filepath.Join(sub, config.FileName)); err != nil {
		t.Fatalf("config not written to dir: %v", err)
	}
}

func TestUsageErrors(t *testing.T) {
	root := t.TempDir()
	for _, tokens := range [][]string{
		{"--minimal", "--full"},
		{filepath.Join(root, "missing")},
	} {
		r := run(t, nil, root, "", false, tokens...)
		if r.code != 2 {
			t.Errorf("%q: exit %d (%s)", tokens, r.code, r.stderr)
		}
	}
	f := filepath.Join(root, "file")
	_ = os.WriteFile(f, nil, 0o644)
	if r := run(t, nil, root, "", false, f); r.code != 2 || !strings.Contains(r.stderr, "not a directory") {
		t.Errorf("file target: exit %d %s", r.code, r.stderr)
	}
}

func TestPreCommit(t *testing.T) {
	root := t.TempDir()
	r := run(t, nil, root, "", false, "--pre-commit")
	if r.code != 0 {
		t.Fatal(r.stderr)
	}
	golden(t, "pre_commit.yml", r.stdout)
	if _, err := os.Stat(filepath.Join(root, config.FileName)); err == nil {
		t.Fatal("--pre-commit wrote a file")
	}
}

// TestWorkflowWithoutProbeKeepsTag: no network means no SHA; the tag stays
// and the user is told how to pin it (test review 5 D4).
func TestWorkflowWithoutProbeKeepsTag(t *testing.T) {
	kit := testkit.New(t, testdata)
	root := sampleRepo(t)
	r := run(t, kit, root, "", false, "--workflow")
	if r.code != 0 {
		t.Fatalf("exit %d:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "run init with --probe to pin the commit SHA") {
		t.Fatalf("missing note:\n%s", r.stderr)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "jobsworth.yml"))
	if !strings.Contains(string(data), "jobsworth-action@v0 ") {
		t.Fatalf("tag not kept:\n%s", data)
	}
}
