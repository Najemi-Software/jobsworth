package repolink

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestParseRemote(t *testing.T) {
	t.Parallel()
	for url, want := range map[string]string{
		"https://github.com/acme/tools":          "acme/tools",
		"https://github.com/acme/tools.git":      "acme/tools",
		"https://github.com/acme/tools/":         "acme/tools",
		"http://github.com/acme/tools.git":       "acme/tools",
		"git@github.com:acme/tools.git":          "acme/tools",
		"git@github.com:acme/tools":              "acme/tools",
		"ssh://git@github.com/acme/tools.git":    "acme/tools",
		"ssh://git@github.com/acme/tools":        "acme/tools",
		"https://ghe.example.com/acme/tools.git": "",
		"git@gitlab.com:acme/tools.git":          "",
		"https://github.com/acme":                "",
		"":                                       "",
	} {
		got, ok := ParseRemote(url)
		if ok != (want != "") || got != want {
			t.Errorf("ParseRemote(%q) = %q,%v want %q", url, got, ok, want)
		}
	}
}

func TestParseExplicit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "checkout")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		arg     string
		want    Link
		wantErr string
	}{
		{arg: "acme/tools=" + dir, want: Link{Slug: "acme/tools", Display: "acme/tools", Path: dir}},
		{arg: "Acme/Tools=checkout", want: Link{Slug: "acme/tools", Display: "Acme/Tools", Path: dir}},
		{arg: "acme/tools=~", want: Link{Slug: "acme/tools", Display: "acme/tools", Path: home}},
		{arg: "acme/tools=" + dir + "/../checkout", want: Link{Slug: "acme/tools", Display: "acme/tools", Path: dir}},
		{arg: "acme/tools", wantErr: "expected owner/name=path"},
		{arg: "acme=" + dir, wantErr: "expected owner/name"},
		{arg: "acme/tools/extra=" + dir, wantErr: "expected owner/name"},
		{arg: "/tools=" + dir, wantErr: "expected owner/name"},
		{arg: "acme/tools=", wantErr: "empty path"},
		{arg: "acme/tools=" + filepath.Join(root, "missing"), wantErr: "no such file"},
		{arg: "acme/tools=" + file, wantErr: "not a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			t.Parallel()
			got, err := ParseExplicit(tc.arg, root, home)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v want containing %q", err, tc.wantErr)
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

func TestLinkPrecedence(t *testing.T) {
	t.Parallel()
	l := New()
	AddImplicit(l, Link{Slug: "Acme/Tools", Display: "Acme/Tools", Path: "/cwd", Source: CWD})
	AddImplicit(l, Link{Slug: "acme/tools", Display: "acme/tools", Path: "/entry", Source: Entry})
	if lk, _ := Lookup(l, "ACME", "tools"); lk.Path != "/cwd" {
		t.Fatalf("first implicit should win, got %+v", lk)
	}
	if err := AddExplicit(l, Link{Slug: "acme/tools", Display: "acme/tools", Path: "/explicit"}); err != nil {
		t.Fatal(err)
	}
	if lk, _ := Lookup(l, "acme", "tools"); lk.Path != "/explicit" || lk.Source != Explicit {
		t.Fatalf("explicit should replace implicit, got %+v", lk)
	}
	AddImplicit(l, Link{Slug: "acme/tools", Path: "/late", Source: Entry})
	if lk, _ := Lookup(l, "acme", "tools"); lk.Path != "/explicit" {
		t.Fatalf("implicit must not override explicit, got %+v", lk)
	}
	if err := AddExplicit(l, Link{Slug: "acme/tools", Path: "/explicit"}); err != nil {
		t.Fatalf("same path twice is fine: %v", err)
	}
	if err := AddExplicit(l, Link{Slug: "acme/tools", Path: "/other"}); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("different path should conflict, got %v", err)
	}
	if _, ok := Lookup(l, "nobody", "home"); ok {
		t.Fatal("unexpected link")
	}
	if all := All(l); len(all) != 1 {
		t.Fatalf("All = %+v", all)
	}
	if _, ok := Lookup(nil, "a", "b"); ok {
		t.Fatal("nil registry lookup should be false")
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if _, ok := Detect(sub, Entry); ok {
		t.Fatal("no remote yet, should not detect")
	}
	run("remote", "add", "origin", "git@github.com:Acme/Tools.git")
	ResetMemo() // the negative lookup is cached for the process (review 4 N12)
	lk, ok := Detect(sub, Entry)
	if !ok {
		t.Fatal("expected detection")
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRoot, _ := filepath.EvalSymlinks(lk.Path)
	if lk.Slug != "acme/tools" || lk.Display != "Acme/Tools" || gotRoot != wantRoot || lk.Source != Entry {
		t.Fatalf("got %+v", lk)
	}
	if _, ok := Detect(t.TempDir(), CWD); ok {
		t.Fatal("non-repo should not detect")
	}
}

// TestDetectNegativeMemo is medium review 4 N12: a repo with no origin is
// answered from the memo after the first lookup, and ResetMemo forgets it.
func TestDetectNegativeMemo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	root, ok := GitRoot(dir)
	if !ok {
		t.Fatal("not a git root")
	}
	if _, ok := Detect(dir, CWD); ok {
		t.Fatal("no origin yet")
	}
	if _, cached := gitMemo.Load("origin:" + root); !cached {
		t.Fatal("negative origin lookup should be memoised")
	}
	cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/acme/app.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v %s", err, out)
	}
	if _, ok := Detect(dir, CWD); ok {
		t.Fatal("the memo should still answer no until reset")
	}
	ResetMemo()
	if lk, ok := Detect(dir, CWD); !ok || lk.Display != "acme/app" {
		t.Fatalf("after reset: %+v %v", lk, ok)
	}
}

// TestMemoSingleFlight is medium review 5 N13: concurrent lookups of one
// key run the subprocess once.
func TestMemoSingleFlight(t *testing.T) {
	ResetMemo()
	var calls int32
	var wg, entered sync.WaitGroup
	release := make(chan struct{}) // the first fn parks here until every goroutine is at the door
	entered.Add(16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entered.Done()
			memoised("single-flight-test", true, func() (string, bool) {
				atomic.AddInt32(&calls, 1)
				<-release
				return "x", true
			})
		}()
	}
	entered.Wait()
	close(release)
	wg.Wait()
	if calls != 1 {
		t.Fatalf("fn ran %d times, want 1", calls)
	}
}
