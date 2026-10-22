package uses

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		want    Ref
		wantErr string
	}{
		{raw: "./.github/actions/foo", want: Ref{Kind: Local, Path: ".github/actions/foo"}},
		{raw: "./.github/actions/foo/", want: Ref{Kind: Local, Path: ".github/actions/foo"}},
		{raw: "./.github/workflows/foo.yml", want: Ref{Kind: Local, Path: ".github/workflows/foo.yml", IsWorkflow: true}},
		{raw: "./.github/workflows/foo.yaml", want: Ref{Kind: Local, Path: ".github/workflows/foo.yaml", IsWorkflow: true}},
		{raw: "./", want: Ref{Kind: Local, Path: ""}},
		{raw: ".", want: Ref{Kind: Local, Path: ""}},
		{raw: "./../x", wantErr: "escapes"},
		{raw: "actions/checkout@v4", want: Ref{Kind: External, Owner: "actions", Repo: "checkout", Ref: "v4"}},
		{raw: "Actions/CheckOut@v4", want: Ref{Kind: External, Owner: "Actions", Repo: "CheckOut", Ref: "v4"}},
		{raw: "owner/repo/sub/dir@main", want: Ref{Kind: External, Owner: "owner", Repo: "repo", Path: "sub/dir", Ref: "main"}},
		{raw: "owner/repo/.github/workflows/foo.yml@v3", want: Ref{Kind: External, Owner: "owner", Repo: "repo", Path: ".github/workflows/foo.yml", Ref: "v3", IsWorkflow: true}},
		{raw: "owner/repo@0123456789abcdef0123456789abcdef01234567", want: Ref{Kind: External, Owner: "owner", Repo: "repo", Ref: "0123456789abcdef0123456789abcdef01234567"}},
		{raw: "owner/repo/sub@dir@v1", want: Ref{Kind: External, Owner: "owner", Repo: "repo", Path: "sub@dir", Ref: "v1"}},
		{raw: "owner/repo@feature/x", want: Ref{Kind: External, Owner: "owner", Repo: "repo", Ref: "feature/x"}},
		{raw: "owner/repo", wantErr: "no @ref"},
		{raw: "owner/repo@", wantErr: "no @ref"},
		{raw: "owner@v1", wantErr: "not owner/repo"},
		{raw: "/repo@v1", wantErr: "not owner/repo"},
		{raw: "owner//sub@v1", wantErr: "not owner/repo"},
		{raw: "owner/repo//sub@v1", wantErr: "empty path segment"},
		{raw: "owner/repo/../x@v1", wantErr: "invalid path"},
		{raw: "docker://alpine:3", want: Ref{Kind: Docker}},
		{raw: "${{ matrix.action }}", want: Ref{Kind: Dynamic}},
		{raw: "owner/repo@${{ inputs.ref }}", want: Ref{Kind: Dynamic}},
		{raw: "", wantErr: "empty"},
		{raw: "   ", wantErr: "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.want.Raw = tc.raw
			if got != tc.want {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDisplayAndSlug(t *testing.T) {
	t.Parallel()
	r, _ := Parse("Acme/Shared/.github/workflows/lint.yml@v2")
	if got := Display(r); got != "Acme/Shared/.github/workflows/lint.yml@v2" {
		t.Errorf("Display = %q", got)
	}
	if got := SlugNoRef(r); got != "acme/shared/.github/workflows/lint.yml" {
		t.Errorf("SlugNoRef = %q", got)
	}
	l, _ := Parse("./x")
	if got := Display(l); got != "./x" {
		t.Errorf("Display local = %q", got)
	}
}
