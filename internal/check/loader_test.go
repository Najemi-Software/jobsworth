package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

const testdata = "../../testdata"

// entry writes a workflow into a fresh repo-like tree and returns its path.
func entry(t *testing.T, body string) (path, root string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, root
}

func rulesOf(r check.Result) []string {
	var out []string
	for _, f := range check.AllFindings(r) {
		out = append(out, f.Rule)
	}
	return out
}

func link(t *testing.T, slug, path string) *repolink.Links {
	t.Helper()
	l := repolink.New()
	lk, err := repolink.ParseExplicit(slug+"="+path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repolink.AddExplicit(l, lk); err != nil {
		t.Fatal(err)
	}
	return l
}

const callsLinked = `on: push
jobs:
  a:
    uses: acme/tools/.github/workflows/x.yml@v1
    with:
      tool: hammer
name: Linked
permissions: read-all
`

func TestLinkedBeatsFetch(t *testing.T) {
	kit := testkit.New(t, testdata)
	path, root := entry(t, callsLinked)
	links := link(t, "acme/tools", filepath.Join(kit.Testdata, "repos", "acme-tools"))
	r := check.RunEntry(path, check.Options{Config: hcfg(), Links: links, Fetcher: testkit.Fetcher(kit, false), RootOverride: root})
	if fs := check.AllFindings(r); len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
	if n := len(kit.Server.Requests()); n != 0 {
		t.Fatalf("linked reference made %d API calls", n)
	}
	// The callee is reported under its local path, not a display name.
	if len(r.Files) != 2 || !strings.HasSuffix(r.Files[1].Name, filepath.Join("acme-tools", ".github", "workflows", "x.yml")) {
		t.Fatalf("files = %+v", r.Files)
	}
}

func TestLinkedMissingIsErrorNotFetch(t *testing.T) {
	kit := testkit.New(t, testdata)
	path, root := entry(t, "on: push\njobs:\n  a:\n    uses: acme/shared/.github/workflows/lint.yml@v2\n    with:\n      env: x\nname: W3\npermissions: read-all\n")
	links := link(t, "acme/shared", filepath.Join(kit.Testdata, "repos", "acme-tools"))
	r := check.RunEntry(path, check.Options{Config: hcfg(), Links: links, Fetcher: testkit.Fetcher(kit, false), RootOverride: root})
	fs := check.AllFindings(r)
	if len(fs) != 1 || fs[0].Rule != "uses-not-found" || fs[0].Severity != finding.Error || !strings.Contains(fs[0].Message, "linked checkout") {
		t.Fatalf("findings: %+v", fs)
	}
	if n := len(kit.Server.Requests()); n != 0 {
		t.Fatalf("missing linked file fell back to %d API calls", n)
	}
}

func TestFetchedWhenNotLinked(t *testing.T) {
	kit := testkit.New(t, testdata)
	path, root := entry(t, "on: push\njobs:\n  a:\n    uses: acme/shared/.github/workflows/lint.yml@v2\n    with:\n      env: x\nname: W4\npermissions: read-all\n")
	r := check.RunEntry(path, check.Options{Config: hcfg(), Fetcher: testkit.Fetcher(kit, false), RootOverride: root})
	got := rulesOf(r)
	if len(got) != 1 || got[0] != "input-unused" {
		t.Fatalf("rules = %v", got)
	}
	// Callee and its nested ./ action are attributed by display name.
	var names []string
	for _, f := range r.Files {
		names = append(names, f.Name)
	}
	want := []string{path, "acme/shared/.github/workflows/lint.yml@v2", "actions/checkout@v4", "acme/shared/.github/actions/notify@v2"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v\nwant  %v", names, want)
	}
	if n := len(kit.Server.Requests()); n == 0 {
		t.Fatal("expected API calls")
	}
}

func TestSkipListShortCircuitsBeforeFetch(t *testing.T) {
	kit := testkit.New(t, testdata)
	path, root := entry(t, "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: Acme/Shared@v2\n  b:\n    uses: acme/shared/.github/workflows/lint.yml@v2\nname: W5\npermissions: read-all\n")
	cfg, err := config.Parse([]byte("skip:\n  actions: [acme/shared]\n  workflows: [acme/shared/.github/workflows/lint.yml]\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	testkit.HarnessConfig(&cfg, "")
	r := check.RunEntry(path, check.Options{Fetcher: testkit.Fetcher(kit, false), Config: cfg, RootOverride: root})
	if fs := check.AllFindings(r); len(fs) != 0 {
		t.Fatalf("findings: %+v", fs)
	}
	if n := len(kit.Server.Requests()); n != 0 {
		t.Fatalf("skip-listed references made %d API calls", n)
	}
}

func TestIdentityDedupesAcrossRefSpellings(t *testing.T) {
	kit := testkit.New(t, testdata)
	sha := testkit.SHAFor("actions", "checkout", "v4")
	path, root := entry(t, "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/checkout@"+sha+"\n      - uses: Actions/Checkout@v4\nname: W6\npermissions: read-all\n")
	r := check.RunEntry(path, check.Options{Config: hcfg(), Fetcher: testkit.Fetcher(kit, false), RootOverride: root})
	if len(r.Files) != 2 {
		t.Fatalf("expected entry + one callee, got %d files: %+v", len(r.Files), r.Files)
	}
	var contents int
	for _, rq := range kit.Server.Requests() {
		if strings.Contains(rq.Path, "/contents/") {
			contents++
		}
	}
	if contents != 1 {
		t.Fatalf("content fetched %d times for one identity: %+v", contents, kit.Server.Requests())
	}
}

func TestNilFetcherIsWarning(t *testing.T) {
	t.Parallel()
	path, root := entry(t, "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - run: make test\nname: W7\npermissions: read-all\n")
	r := check.RunEntry(path, check.Options{Config: hcfg(), RootOverride: root})
	fs := check.AllFindings(r)
	if len(fs) != 1 || fs[0].Rule != "fetch-failed" || fs[0].Severity != finding.Warning {
		t.Fatalf("findings: %+v", fs)
	}
}

func TestEntryErrors(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"malformed":      "jobs: [\n",
		"not a workflow": "foo: bar\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path, root := entry(t, body)
			r := check.RunEntry(path, check.Options{Config: hcfg(), RootOverride: root})
			fs := check.AllFindings(r)
			if len(fs) != 1 || fs[0].Rule != "file-invalid" || fs[0].File != path {
				t.Fatalf("findings: %+v", fs)
			}
		})
	}
	r := check.RunEntry(filepath.Join(t.TempDir(), "missing.yml"), check.Options{Config: hcfg()})
	if fs := check.AllFindings(r); len(fs) != 1 || fs[0].Rule != "file-invalid" {
		t.Fatalf("missing entry: %+v", check.AllFindings(r))
	}
}

func TestRepoRootFallsBackToGithubDir(t *testing.T) {
	t.Parallel()
	_, root := entry(t, "on: push\njobs: {}\nname: W8\npermissions: read-all\n")
	got := check.RepoRoot(filepath.Join(root, ".github", "workflows"))
	if got != root {
		t.Fatalf("RepoRoot = %q want %q", got, root)
	}
	lone := t.TempDir()
	if got := check.RepoRoot(lone); got != lone {
		t.Fatalf("no .github: RepoRoot = %q want %q", got, lone)
	}
}

// TestActionOutputConsumedAcrossFiles is medium §1.6: an output consumed
// by a workflow in the entry repo (or a linked repo) is not reported.
func TestActionOutputConsumedAcrossFiles(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	action := write(".github/actions/meta/action.yml", "name: Meta\noutputs:\n  version:\n    value: ${{ steps.v.outputs.version }}\n  sha:\n    value: ${{ steps.v.outputs.sha }}\nruns:\n  using: composite\n  steps:\n    - id: v\n      shell: bash\n      run: |\n        echo \"version=1\" >> \"$GITHUB_OUTPUT\"\n        echo \"sha=abc\" >> \"$GITHUB_OUTPUT\"\ndescription: D\n")
	write(".github/workflows/ci.yml", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - id: m\n        uses: ./.github/actions/meta\n      - run: echo ${{ steps.m.outputs.version }}\nname: W9\npermissions: read-all\n")
	r := check.RunEntry(action, check.Options{Config: hcfg(), RootOverride: root})
	var unused []string
	for _, f := range check.AllFindings(r) {
		if f.Rule == "action-output-unused" {
			unused = append(unused, f.Message)
		}
	}
	if len(unused) != 1 || !strings.Contains(unused[0], "'sha'") {
		t.Fatalf("expected only sha unused, got %v\nall: %+v", unused, check.AllFindings(r))
	}
}

// hcfg is the corpus-wide harness config (trusted fake-remote owners,
// permissions-missing off).
func hcfg() config.Config {
	c := config.Defaults()
	testkit.HarnessConfig(&c, "")
	return c
}
