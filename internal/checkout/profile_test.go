package checkout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/workflow"
)

const wrappers = "../../testdata/remote/acme/wrappers/v1"

func loadAction(t *testing.T, name string) *workflow.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(wrappers, name, "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := workflow.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// resolver loads nested wrappers from the fixture tree by base path.
func resolver(t *testing.T) Resolver {
	return func(raw string) (*workflow.File, string, bool) {
		base := strings.SplitN(raw, "@", 2)[0]
		name := strings.TrimPrefix(base, "acme/wrappers/")
		if name == base {
			return nil, "", false
		}
		if _, err := os.Stat(filepath.Join(wrappers, name, "action.yml")); err != nil {
			return nil, "", false
		}
		return loadAction(t, name), "remote:" + base, true
	}
}

func step(uses string, with map[string]string, ifv string) workflow.Step {
	st := workflow.Step{Uses: uses, If: workflow.Scalar{Value: ifv}}
	for k, v := range with {
		st.With = append(st.With, workflow.With{Name: k, Value: workflow.Scalar{Value: v}})
	}
	return st
}

func TestProfileWiring(t *testing.T) {
	t.Parallel()
	p := Profile(loadAction(t, "checkout-cached"), "remote:acme/wrappers/checkout-cached", resolver(t))
	if len(p) != 1 || p[0].Conditional || p[0].ViaStep != 2 || strings.Join(p[0].Via, ",") != "actions/checkout@v4" {
		t.Fatalf("profile = %+v", p)
	}
	w := p[0].Wiring
	for k, want := range map[string]Kind{"path": WirePassthrough, "repository": WirePassthrough, "ref": WirePassthrough, "sparse-checkout": WirePassthrough, "persist-credentials": WirePassthrough, "token": WirePassthrough} {
		if w[k].Kind != want || w[k].Input != k {
			t.Errorf("%s wired %+v", k, w[k])
		}
	}
	if w["repository"].Default.Kind != WireAbsent { // default github.repository is own repo
		t.Errorf("repository default = %+v", w["repository"].Default)
	}
	if w["persist-credentials"].Default.Kind != WireLiteral || w["persist-credentials"].Default.Value != "true" {
		t.Errorf("persist-credentials default = %+v", w["persist-credentials"].Default)
	}
	if w["token"].Default.Kind != WireDynamic {
		t.Errorf("token default = %+v", w["token"].Default)
	}

	src := Profile(loadAction(t, "checkout-src"), "x", nil)
	if len(src) != 1 || src[0].Wiring["path"].Kind != WireLiteral || src[0].Wiring["path"].Value != "src" {
		t.Fatalf("checkout-src = %+v", src)
	}
	two := Profile(loadAction(t, "checkout-two"), "x", nil)
	if len(two) != 2 || len(two[0].Wiring) != 0 || two[1].Wiring["repository"].Value != "acme/shared" || two[1].Wiring["ref"].Kind != WirePassthrough || two[1].Wiring["path"].Value != "tools" {
		t.Fatalf("checkout-two = %+v", two)
	}
	maybe := Profile(loadAction(t, "checkout-maybe"), "x", nil)
	if len(maybe) != 1 || !maybe[0].Conditional {
		t.Fatalf("checkout-maybe = %+v", maybe)
	}
	dyn := Profile(loadAction(t, "checkout-dynamic"), "x", nil)
	if len(dyn) != 1 || dyn[0].Wiring["path"].Kind != WireDynamic {
		t.Fatalf("checkout-dynamic = %+v", dyn)
	}
	if p := Profile(loadAction(t, "checkout-node"), "x", nil); p != nil {
		t.Fatalf("node action profiled: %+v", p)
	}
}

// TestTrailingClean is review 1 D2: whether the wrapper's own steps after
// the checkout read the tree.
func TestTrailingClean(t *testing.T) {
	t.Parallel()
	// Without a cleaner every later step counts as a reader.
	if p := Profile(loadAction(t, "checkout-cached"), "c", nil); !p[0].TrailingClean {
		t.Fatalf("checkout as the last step is trailing-clean: %+v", p[0])
	}
	if p := Profile(loadAction(t, "checkout-reads"), "r", nil); p[0].TrailingClean {
		t.Fatalf("a later run step without a cleaner is a reader: %+v", p[0])
	}
	echoOnly := func(st workflow.Step, _ *workflow.File) bool {
		return !st.HasRun || strings.HasPrefix(strings.TrimSpace(st.Run), "echo")
	}
	if p := ProfileWith(loadAction(t, "checkout-reads"), "r", nil, echoOnly); p[0].TrailingClean {
		t.Fatalf("ls -la reads the tree: %+v", p[0])
	}
	two := ProfileWith(loadAction(t, "checkout-two"), "t", nil, echoOnly)
	if !two[0].TrailingClean || !two[1].TrailingClean { // the second checkout is itself clean
		t.Fatalf("two: %+v", two)
	}
	// Composition: the outer wrapper's steps after its wrapper step count.
	outer := ProfileWith(loadAction(t, "checkout-outer"), "o", resolver(t), echoOnly)
	if !outer[0].TrailingClean {
		t.Fatalf("outer with nothing after the wrapper step: %+v", outer[0])
	}
	f, _ := workflow.Parse([]byte("name: O\ndescription: D\nruns:\n  using: composite\n  steps:\n    - uses: acme/wrappers/checkout-cached@v1\n    - shell: bash\n      run: make build\n"))
	if p := ProfileWith(f, "o2", resolver(t), echoOnly); p[0].TrailingClean {
		t.Fatalf("outer reading the tree after its wrapper step: %+v", p[0])
	}
	if got := Effective(outer[0], step("acme/wrappers/checkout-outer@v1", nil, "")); !got.TrailingClean {
		t.Fatal("Effective must carry TrailingClean")
	}
}

func TestProfileComposition(t *testing.T) {
	t.Parallel()
	outer := Profile(loadAction(t, "checkout-outer"), "remote:acme/wrappers/checkout-outer", resolver(t))
	if len(outer) != 1 || strings.Join(outer[0].Via, ",") != "acme/wrappers/checkout-cached@v1,actions/checkout@v4" {
		t.Fatalf("outer = %+v", outer)
	}
	w := outer[0].Wiring
	if w["repository"].Kind != WireFallback || w["repository"].Input != "repo" || w["repository"].Value != "" {
		t.Errorf("repository = %+v", w["repository"])
	}
	if w["path"].Kind != WirePassthrough || w["path"].Input != "where" || w["path"].Default == nil || w["path"].Default.Kind != WireLiteral || w["path"].Default.Value != "" {
		t.Errorf("path = %+v", w["path"])
	}
	// The inner's own defaults survive when the outer passes nothing.
	if w["persist-credentials"].Kind != WireLiteral || w["persist-credentials"].Value != "true" {
		t.Errorf("persist-credentials = %+v", w["persist-credentials"])
	}
	if _, ok := w["ref"]; ok { // inner default '' → absent
		t.Errorf("ref should be absent, got %+v", w["ref"])
	}

	cycle := Profile(loadAction(t, "checkout-cycle-a"), "remote:acme/wrappers/checkout-cycle-a", resolver(t))
	if len(cycle) != 1 || !cycle[0].Unknown || !strings.Contains(cycle[0].Reason, "cycle") {
		t.Fatalf("cycle = %+v", cycle)
	}

	// Depth cap: three wrappers around checkout-cached is one too many.
	deep := func(n int) *workflow.File {
		f, _ := workflow.Parse([]byte("name: D\ndescription: D\nruns:\n  using: composite\n  steps:\n    - uses: deep/" + string(rune('0'+n)) + "@v1\n"))
		return f
	}
	res := func(raw string) (*workflow.File, string, bool) {
		switch raw {
		case "deep/1@v1":
			return deep(2), "deep/1", true
		case "deep/2@v1":
			return deep(3), "deep/2", true
		case "deep/3@v1":
			return loadAction(t, "checkout-cached"), "cached", true
		}
		return nil, "", false
	}
	// deep0 → deep1 → deep2 → deep3 → cached: four wrapper levels is one too many.
	capped := profile(deep(1), res, nil, 0, map[string]bool{"deep/0": true})
	if len(capped) != 1 || !capped[0].Unknown || !strings.Contains(capped[0].Reason, "deeper") {
		t.Fatalf("depth cap = %+v", capped)
	}
	// deep1 → deep2 → deep3 → cached: three levels resolve.
	ok := profile(deep(2), res, nil, 0, map[string]bool{"deep/1": true})
	if len(ok) != 1 || ok[0].Unknown {
		t.Fatalf("three levels should resolve: %+v", ok)
	}
}

func TestProfileOfCheckoutItselfIsEmpty(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/remote/actions/checkout/v4/action.yml")
	if err != nil {
		t.Skip("no checkout fixture")
	}
	f, err := workflow.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p := Profile(f, "actions/checkout", nil); p != nil {
		t.Fatalf("actions/checkout is not a wrapper of itself: %+v", p)
	}
}

func TestEffectiveTable(t *testing.T) {
	t.Parallel()
	cached := Profile(loadAction(t, "checkout-cached"), "c", nil)[0]
	outer := Profile(loadAction(t, "checkout-outer"), "o", resolver(t))[0]
	src := Profile(loadAction(t, "checkout-src"), "s", nil)[0]
	dyn := Profile(loadAction(t, "checkout-dynamic"), "d", nil)[0]
	for _, tc := range []struct {
		name   string
		entry  Entry
		with   map[string]string
		ifv    string
		target Target
		path   string
		repo   string
		dyn    bool // RepoDyn || RefDyn
		cond   bool
		pc     string
	}{
		{"passthrough with no caller value: defaults", cached, nil, "", Root, "", "", false, false, "true"},
		{"caller path", cached, map[string]string{"path": "src"}, "", Subdir, "src", "", false, false, "true"},
		{"caller repository", cached, map[string]string{"repository": "acme/shared"}, "", Root, "", "acme/shared", false, false, "true"},
		{"caller dynamic ref", cached, map[string]string{"ref": "${{ github.sha }}"}, "", Root, "", "", true, false, "true"},
		{"caller persist false", cached, map[string]string{"persist-credentials": "false"}, "", Root, "", "", false, false, "false"},
		{"literal path ignores caller", src, map[string]string{"path": "other"}, "", Subdir, "src", "", false, false, ""},
		{"fallback with nothing: own repo", outer, nil, "", Root, "", "", false, false, "true"},
		{"fallback with empty literal: own repo", outer, map[string]string{"repo": ""}, "", Root, "", "", false, false, "true"},
		{"fallback with value", outer, map[string]string{"repo": "acme/shared", "where": "lib"}, "", Subdir, "lib", "acme/shared", false, false, "true"},
		{"dynamic path", dyn, nil, "", Dynamic, dynamicMarker, "", false, false, ""},
		{"caller step if", cached, nil, "github.ref == 'x'", Root, "", "", false, true, "true"},
	} {
		got := Effective(tc.entry, step("acme/wrappers/x@v1", tc.with, tc.ifv))
		if got.Target != tc.target || got.Path != tc.path || got.Repository != tc.repo || (got.RepoDyn || got.RefDyn) != tc.dyn || got.Conditional != tc.cond || got.PersistCredentials != tc.pc {
			t.Errorf("%s: %+v", tc.name, got)
		}
		if !got.IsWrapper() {
			t.Errorf("%s: not marked as a wrapper", tc.name)
		}
	}
	// Sparse list through the wrapper, cone mode from the caller.
	got := Effective(cached, step("w@v1", map[string]string{"sparse-checkout": "src\ndocs"}, ""))
	if got.Sparse == nil || strings.Join(got.Sparse.Entries, ",") != "src,docs" || !got.Sparse.Cone {
		t.Fatalf("sparse = %+v", got.Sparse)
	}
	unknown := Effective(Entry{Unknown: true, Reason: "r"}, step("w@v1", nil, ""))
	if !unknown.Unknown || unknown.Reason != "r" {
		t.Fatalf("unknown = %+v", unknown)
	}
}
