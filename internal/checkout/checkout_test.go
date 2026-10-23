package checkout

import (
	"reflect"
	"testing"
)

func TestIsCheckoutAction(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		"actions/checkout@v4":                     true,
		"Actions/Checkout@v3":                     true,
		"actions/checkout":                        false,
		"acme/tools/git/checkout-with-cache@v1":   false, // wrappers come from their content, not a list
		"acme/tools/git/checkout-with-cache@main": false,
		"acme/tools/git/other@v1":                 false,
		"./.github/actions/checkout":              false,
		"docker://alpine":                         false,
		"${{ matrix.action }}":                    false,
	} {
		if got := IsCheckoutAction(raw); got != want {
			t.Errorf("IsCheckoutAction(%q) = %v", raw, got)
		}
	}
}

func TestClassifyPath(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Target{
		"": Root, ".": Root, "./": Root, "sub": Subdir, "./sub/": Subdir, "a/b": Subdir, "${{ inputs.path }}": Dynamic,
	} {
		if got, _ := ClassifyPath(in); got != want {
			t.Errorf("ClassifyPath(%q) = %v want %v", in, got, want)
		}
	}
	if _, p := ClassifyPath("./sub/"); p != "sub" {
		t.Errorf("path = %q", p)
	}
}

func TestSparseCovers(t *testing.T) {
	t.Parallel()
	cone := func(entries ...string) Sparse { return Sparse{Entries: entries, Cone: true} }
	for _, tc := range []struct {
		sp   Sparse
		path string
		want bool
	}{
		{cone(), ".github/actions/x", true},
		{cone(""), ".github/actions/x", true},
		{cone("."), ".github/actions/x", true},
		{cone(".github"), ".github/actions/x", true},
		{cone(".github/actions"), ".github/actions/x", true},
		{cone(".github/actions/x"), ".github/actions/x", true},
		{cone(".github/actions/x"), ".github/actions/xy", false},
		{cone(".github"), ".github-extra/actions/x", false},
		{cone("src"), ".github/actions/x", false},
		{cone("/.github/"), ".github/actions/x", true},
		{Sparse{Entries: []string{".github/act"}, Cone: false}, ".github/actions/x", true},
		{Sparse{Entries: []string{"src"}, Cone: false}, ".github/actions/x", false},
		{Sparse{Entries: []string{"src"}, Dynamic: true}, ".github/actions/x", true},
	} {
		if got := SparseCovers(tc.sp, tc.path); got != tc.want {
			t.Errorf("SparseCovers(%+v, %q) = %v", tc.sp, tc.path, got)
		}
	}
}

func TestParseSparseCheckout(t *testing.T) {
	t.Parallel()
	sp := ParseSparseCheckout(".github\nsrc\n# comment\n", false, "")
	if !reflect.DeepEqual(sp.Entries, []string{".github", "src"}) || !sp.Cone || sp.Dynamic {
		t.Errorf("block: %+v", sp)
	}
	sp = ParseSparseCheckout("${{ inputs.paths }}", true, "")
	if !sp.Dynamic {
		t.Error("expression must be dynamic")
	}
	sp = ParseSparseCheckout(".github\n${{ x }}", false, "")
	if !sp.Dynamic {
		t.Error("expression inside must be dynamic")
	}
	sp = ParseSparseCheckout("src/*", false, "false")
	if sp.Cone || !sp.Dynamic {
		t.Errorf("non-cone glob: %+v", sp)
	}
	sp = ParseSparseCheckout("src", false, "false")
	if sp.Cone || sp.Dynamic {
		t.Errorf("non-cone literal: %+v", sp)
	}
}
