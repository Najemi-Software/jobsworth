package check

import (
	"testing"

	"github.com/najemi-software/jobsworth/internal/cli"
)

// TestSchemaAcceptsSpecInvocations pins §1.1–1.2: which invocations parse and
// validate, and which are rejected before Run is ever called.
func TestSchemaAcceptsSpecInvocations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		tokens  []string
		wantErr bool
	}{
		{name: "no args", tokens: nil},
		{name: "one path", tokens: []string{".github/workflows/ci.yml"}},
		{name: "many entries", tokens: []string{"a.yml", "b/*.yml", "c/**/action.yml"}},
		{name: "repo once", tokens: []string{"--repo", "acme/tools=/tmp/x"}},
		{name: "repo repeated", tokens: []string{"--repo", "a/b=/x", "--repo=c/d=/y"}},
		{name: "offline", tokens: []string{"--offline"}},
		{name: "no-cache", tokens: []string{"--no-cache"}},
		{name: "cache-dir", tokens: []string{"--cache-dir", "/tmp/c"}},
		{name: "fail-on error", tokens: []string{"--fail-on", "error"}},
		{name: "fail-on warning", tokens: []string{"--fail-on=warning"}},
		{name: "fail-on note", tokens: []string{"--fail-on", "note"}},
		{name: "format text", tokens: []string{"--format", "text"}},
		{name: "format json", tokens: []string{"--format", "json"}},
		{name: "config", tokens: []string{"--config", ".jobsworth.yml"}},
		{name: "show-suppressed", tokens: []string{"--show-suppressed"}},
		{name: "flags then args", tokens: []string{"--offline", "--format", "json", "x.yml", "y.yml"}},

		{name: "unknown flag", tokens: []string{"--verbose"}, wantErr: true},
		{name: "fail-on bad value", tokens: []string{"--fail-on", "fatal"}, wantErr: true},
		{name: "format bad value", tokens: []string{"--format", "sarif"}, wantErr: true},
		{name: "format twice", tokens: []string{"--format", "text", "--format", "json"}, wantErr: true},
		{name: "fail-on twice", tokens: []string{"--fail-on", "error", "--fail-on", "note"}, wantErr: true},
		{name: "cache-dir twice", tokens: []string{"--cache-dir", "/a", "--cache-dir", "/b"}, wantErr: true},
		{name: "config twice", tokens: []string{"--config", "a", "--config", "b"}, wantErr: true},
		{name: "repo missing value", tokens: []string{"--repo"}, wantErr: true},
		{name: "offline with value", tokens: []string{"--offline=yes"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args, flags, err := cli.ParseArgs(Schema, tc.tokens)
			if err == nil {
				err = cli.Validate(Schema, args, flags)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("tokens %q: err=%v wantErr=%v", tc.tokens, err, tc.wantErr)
			}
		})
	}
}

func TestSchemaRepoIsRepeatableAndKeepsOrder(t *testing.T) {
	t.Parallel()
	_, flags, err := cli.ParseArgs(Schema, []string{"--repo", "a/b=/x", "--repo", "c/d=/y"})
	if err != nil {
		t.Fatal(err)
	}
	got := flags["repo"]
	if len(got) != 2 || got[0] != "a/b=/x" || got[1] != "c/d=/y" {
		t.Fatalf("repo values = %q", got)
	}
}
