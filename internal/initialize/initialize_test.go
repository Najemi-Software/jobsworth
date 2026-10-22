package initialize

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/config"
)

const sample = "../../testdata/repos/init-sample"

func TestIsWellKnownOwner(t *testing.T) {
	t.Parallel()
	for slug, want := range map[string]bool{"actions/checkout": true, "Actions/setup-node": true, "github/codeql-action": true, "acme/tools": false, "actionsx/y": false} {
		if got := IsWellKnownOwner(slug); got != want {
			t.Errorf("IsWellKnownOwner(%q) = %v", slug, got)
		}
	}
}

func TestInspectSample(t *testing.T) {
	t.Parallel()
	root, _ := filepath.Abs(sample)
	in := Inspect(root)
	if in.Workflows != 2 || in.Actions != 1 {
		t.Errorf("counts: %d workflows %d actions", in.Workflows, in.Actions)
	}
	if len(in.ParseErrors) != 1 || !strings.HasPrefix(in.ParseErrors[0], ".github/workflows/notes.yml") {
		t.Errorf("parse errors = %v", in.ParseErrors)
	}
	if ExternalRefCount(in) != 5 {
		t.Errorf("external refs = %d", ExternalRefCount(in))
	}
	var slugs []string
	for _, r := range SortedRepos(in) {
		slugs = append(slugs, r.Slug)
	}
	if !reflect.DeepEqual(slugs, []string{"acme/tools", "acme/forbidden", "acme/nope", "acme/shared", "actions/checkout"}) {
		t.Errorf("repos = %v", slugs)
	}
	if in.NonBashSteps != 1 {
		t.Errorf("non-bash steps = %d", in.NonBashSteps)
	}
	if r := in.Refs["acme/shared/.github/workflows/lint.yml"]; r == nil || !r.IsWorkflow || r.Refs[0] != "v2" {
		t.Errorf("workflow ref = %+v", r)
	}
}

func TestCountNonBashSteps(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]int{
		"jobs:\n  a:\n    steps:\n      - run: x\n":                                                                          0,
		"jobs:\n  a:\n    steps:\n      - run: x\n        shell: pwsh\n      - run: y\n        shell: bash\n":                1,
		"defaults:\n  run:\n    shell: pwsh\njobs:\n  a:\n    steps:\n      - run: x\n      - run: y\n        shell: bash\n": 1,
		"jobs:\n  a:\n    defaults:\n      run:\n        shell: python\n    steps:\n      - run: x\n      - uses: a/b@v1\n":  1,
		"runs:\n  using: composite\n  steps:\n    - run: x\n      shell: bash\n    - run: y\n      shell: python\n":          1,
	} {
		if got := countNonBashSteps([]byte(src)); got != want {
			t.Errorf("%q: got %d want %d", src, got, want)
		}
	}
}

// workspace builds <parent>/app (the repo) and <parent>/<sibling> with an
// origin remote when git is available.
func workspace(t *testing.T, sibling, origin string) (root, sib string) {
	t.Helper()
	parent := t.TempDir()
	root = filepath.Join(parent, "app")
	sib = filepath.Join(parent, sibling)
	for _, d := range []string{root, sib} {
		if err := os.MkdirAll(filepath.Join(d, ".github"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if origin != "" {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not on PATH")
		}
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = sib
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
	}
	return root, sib
}

func TestFindCheckoutWithGit(t *testing.T) {
	t.Parallel()
	root, sib := workspace(t, "acme-tools", "git@github.com:Acme/Tools.git")
	co, ok := FindCheckout(root, "acme/tools", LookupEnv{})
	if !ok || !co.OriginVerified || co.Display != "../acme-tools" {
		t.Fatalf("got %+v %v", co, ok)
	}
	want, _ := filepath.EvalSymlinks(sib)
	got, _ := filepath.EvalSymlinks(co.Path)
	if got != want {
		t.Fatalf("path %q want %q", got, want)
	}
	// A sibling with the right name but the wrong origin is not a match.
	root2, _ := workspace(t, "tools", "git@github.com:other/tools.git")
	if _, ok := FindCheckout(root2, "acme/tools", LookupEnv{}); ok {
		t.Fatal("wrong origin should not match")
	}
	// A sibling without git metadata is not a match when git is available.
	root3, _ := workspace(t, "tools", "")
	yes := true
	if _, ok := FindCheckout(root3, "acme/tools", LookupEnv{GitAvailable: &yes}); ok {
		t.Fatal("unverifiable sibling should not match when git is available")
	}
}

func TestFindCheckoutWithoutGit(t *testing.T) {
	t.Parallel()
	no := false
	root, _ := workspace(t, "tools", "")
	co, ok := FindCheckout(root, "acme/tools", LookupEnv{GitAvailable: &no})
	if !ok || co.OriginVerified || co.Display != "../tools" {
		t.Fatalf("got %+v %v", co, ok)
	}
	if _, ok := FindCheckout(root, "acme/other", LookupEnv{GitAvailable: &no}); ok {
		t.Fatal("missing sibling should not match")
	}
}

func TestFindCheckoutReposPath(t *testing.T) {
	t.Parallel()
	no := false
	root, _ := workspace(t, "unrelated", "")
	home := t.TempDir()
	extra := filepath.Join(home, "src")
	if err := os.MkdirAll(filepath.Join(extra, "acme-tools", ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	co, ok := FindCheckout(root, "acme/tools", LookupEnv{Home: home, ReposPath: "/nope:~/src", GitAvailable: &no})
	if !ok || co.Display != "~/src/acme-tools" {
		t.Fatalf("got %+v %v", co, ok)
	}
}

func TestDisplayPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ root, path, home, want string }{
		{"/src/app", "/src/tools", "/home/me", "../tools"},
		{"/src/app", "/home/me/src/tools", "/home/me", "~/src/tools"},
		{"/src/app", "/opt/tools", "/home/me", "/opt/tools"},
		{"/src/app", "/home/me", "/home/me", "~"},
	} {
		if got := DisplayPath(tc.root, tc.path, tc.home); got != tc.want {
			t.Errorf("DisplayPath(%q,%q,%q) = %q want %q", tc.root, tc.path, tc.home, got, tc.want)
		}
	}
}

func TestRenderWorkflowPrivateComment(t *testing.T) {
	t.Parallel()
	plain := RenderWorkflow("0.1.0", nil)
	if strings.Contains(plain, "private repos") || !strings.Contains(plain, "jobsworth-action@v0 ") || !strings.Contains(plain, "(v0.1.0)") || !strings.Contains(plain, "pin to a commit SHA") {
		t.Fatalf("plain:\n%s", plain)
	}
	pinned := RenderWorkflowPinned("0.1.0", nil, "0123456789abcdef0123456789abcdef01234567")
	if !strings.Contains(pinned, "jobsworth-action@0123456789abcdef0123456789abcdef01234567 # v0, the release matching this binary (v0.1.0)") {
		t.Fatalf("pinned:\n%s", pinned)
	}
	priv := RenderWorkflow("1.2.3", []string{"acme/tools"})
	if !strings.Contains(priv, "Detected private repos: acme/tools") || !strings.Contains(priv, "@v1 ") {
		t.Fatalf("private:\n%s", priv)
	}
}

func TestRenderFullRoundTrip(t *testing.T) {
	t.Parallel()
	out := Render(Detected{}, ModeFull)
	c, err := config.Parse([]byte(out), nil)
	if err != nil {
		t.Fatalf("--full output does not parse: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(c, config.Defaults()) {
		t.Fatalf("--full output differs from defaults:\n got %+v\nwant %+v", c, config.Defaults())
	}
	if _, err := config.Parse([]byte(Render(Detected{}, ModeDefault)), nil); err != nil {
		t.Fatalf("default output does not parse: %v", err)
	}
	if _, err := config.Parse([]byte(Render(Detected{}, ModeMinimal)), nil); err != nil {
		t.Fatalf("minimal output does not parse: %v", err)
	}
}

func TestRenderMinimalOnlyNonDefault(t *testing.T) {
	t.Parallel()
	out := Render(Detected{
		Links:        []Checkout{{Slug: "acme/tools", Display: "../acme-tools", OriginVerified: true}},
		SkipActions:  []string{"acme/forbidden"},
		NonBashSteps: 3,
	}, ModeMinimal)
	want := "schema: 1\nrepos:\n  acme/tools: ../acme-tools\nskip:\n  actions:\n    - acme/forbidden\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "#") {
		t.Fatal("minimal must not contain comments")
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "x.yml")
	if err := WriteFile(p, []byte("one"), false); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(p, []byte("two"), false); err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "one" {
		t.Fatalf("overwritten without force: %q", got)
	}
	if err := WriteFile(p, []byte("two"), true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "two" {
		t.Fatalf("force write: %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestTargetRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	_ = os.MkdirAll(sub, 0o755)
	inRepo := func(d string) (string, bool) { return dir, true }
	if got, err := TargetRoot(sub, inRepo); err != nil || got != dir {
		t.Fatalf("in repo: %q %v", got, err)
	}
	if got, err := TargetRoot(sub, func(string) (string, bool) { return "", false }); err != nil || got != sub {
		t.Fatalf("not in repo: %q %v", got, err)
	}
	f := filepath.Join(dir, "file")
	_ = os.WriteFile(f, nil, 0o644)
	if _, err := TargetRoot(f, nil); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file: %v", err)
	}
	if _, err := TargetRoot(filepath.Join(dir, "missing"), nil); err == nil {
		t.Fatal("missing dir should error")
	}
}
