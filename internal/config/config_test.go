package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/najemi-software/jobsworth/internal/finding"
)

func known(id string) bool {
	return id == "input-unused" || id == "needs-output-not-needed"
}

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		check   func(t *testing.T, c Config)
		wantErr string
	}{
		{
			name: "every key",
			src: `schema: 1
repos:
  example/infra: ~/src/infra
skip:
  actions: [netlify/actions/cli, PyPA/gh-action-pypi-publish]
  workflows:
    - some-org/some-repo/.github/workflows/opaque.yml
checkout:
  actions: [acme/tools/git/checkout-with-cache]
bash:
  opaque_commands: [mytool]
shapes:
  github_event_open: false
fetch:
  api_url: https://ghe.example.com/api/v3/
  timeout: 30s
  parallel: 2
cache:
  dir: /tmp/c
  tag_ttl: 1h
  branch_ttl: 1m
rules:
  input-unused: off
  needs-output-not-needed: warning
fail_on: warning
entries: [wf/*.yml]
`,
			check: func(t *testing.T, c Config) {
				if c.Repos["example/infra"] != "~/src/infra" {
					t.Errorf("repos = %v", c.Repos)
				}
				if !c.SkipActions["netlify/actions/cli"] || !c.SkipActions["pypa/gh-action-pypi-publish"] {
					t.Errorf("skip actions = %v", c.SkipActions)
				}
				if !c.SkipWorkflows["some-org/some-repo/.github/workflows/opaque.yml"] {
					t.Errorf("skip workflows = %v", c.SkipWorkflows)
				}
				if !containsNote(c.Notes, "checkout.actions is no longer needed") {
					t.Errorf("checkout.actions should be accepted with a note, got notes %v", c.Notes)
				}
				if !reflect.DeepEqual(c.BashOpaqueCommands, []string{"mytool"}) || c.ShapesGitHubEventOpen {
					t.Errorf("bash/shapes = %v %v", c.BashOpaqueCommands, c.ShapesGitHubEventOpen)
				}
				if c.FetchAPIURL != "https://ghe.example.com/api/v3" || c.FetchTimeout != 30*time.Second || c.FetchParallel != 2 {
					t.Errorf("fetch = %q %v %d", c.FetchAPIURL, c.FetchTimeout, c.FetchParallel)
				}
				if c.CacheDir != "/tmp/c" || c.CacheTagTTL != time.Hour || c.CacheBranchTTL != time.Minute {
					t.Errorf("cache = %q %v %v", c.CacheDir, c.CacheTagTTL, c.CacheBranchTTL)
				}
				if !c.Rules["input-unused"].Off {
					t.Errorf("input-unused should be off: %+v", c.Rules)
				}
				if o := c.Rules["needs-output-not-needed"]; o.Off || o.Severity != finding.Warning {
					t.Errorf("override = %+v", o)
				}
				if c.FailOn != finding.Warning || !reflect.DeepEqual(c.Entries, []string{"wf/*.yml"}) {
					t.Errorf("fail_on/entries = %v %v", c.FailOn, c.Entries)
				}
				if len(c.Notes) != 1 || !containsNote(c.Notes, "checkout.actions is no longer needed") {
					t.Errorf("notes = %v (want only the checkout.actions deprecation)", c.Notes)
				}
			},
		},
		{name: "empty is defaults with a note", src: "", check: func(t *testing.T, c Config) {
			want := Defaults()
			want.Notes = []string{"schema missing, assuming 1"}
			if !reflect.DeepEqual(c, want) {
				t.Errorf("got %+v\nwant %+v", c, want)
			}
		}},
		{name: "schema missing noted", src: "fail_on: note\n", check: func(t *testing.T, c Config) {
			if len(c.Notes) != 1 || !strings.Contains(c.Notes[0], "schema missing") || c.FailOn != finding.Note {
				t.Errorf("got %+v", c)
			}
		}},
		{name: "schema too new", src: "schema: 2\n", wantErr: "newer than this binary supports (1)"},
		{name: "schema zero", src: "schema: 0\n", wantErr: "at least 1"},
		{name: "unknown top key with suggestion", src: "skip_validation: x\n", wantErr: `unknown key "skip_validation" (line 1); did you mean "skip"?`},
		{name: "unknown nested key with suggestion", src: "skip:\n  action: [a]\n", wantErr: "unknown key skip.action (line 2); did you mean skip.actions?"},
		{name: "unknown fetch key", src: "fetch:\n  timout: 1s\n", wantErr: "did you mean fetch.timeout?"},
		{name: "unknown rule", src: "rules:\n  input-unusd: off\n", wantErr: `unknown rule "input-unusd"`},
		{name: "bad severity", src: "rules:\n  input-unused: fatal\n", wantErr: "not off, error, warning or note"},
		{name: "rule to error", src: "rules:\n  input-unused: error\n", check: func(t *testing.T, c Config) {
			if o := c.Rules["input-unused"]; o.Off || o.Severity != finding.Error {
				t.Errorf("override = %+v", o)
			}
		}},
		{name: "bad repo slug", src: "repos:\n  nope: /x\n", wantErr: "not owner/name"},
		{name: "empty repo path", src: "repos:\n  a/b: \"\"\n", wantErr: "empty path"},
		{name: "skip action with ref", src: "skip:\n  actions: [acme/x@v1]\n", wantErr: "has an @ref"},
		{name: "skip workflow with ref", src: "skip:\n  workflows: [acme/x/.github/workflows/y.yml@main]\n", wantErr: "has an @ref"},
		{name: "bad timeout", src: "fetch:\n  timeout: soon\n", wantErr: `fetch.timeout: "soon" is not a duration`},
		{name: "negative ttl", src: "cache:\n  tag_ttl: -1h\n", wantErr: "cache.tag_ttl"},
		{name: "bad parallel", src: "fetch:\n  parallel: many\n", wantErr: "not a positive integer"},
		{name: "zero parallel", src: "fetch:\n  parallel: 0\n", wantErr: "not a positive integer"},
		{name: "bad fail_on", src: "fail_on: off\n", wantErr: `fail_on: "off" is not error, warning or note`},
		{name: "empty entries", src: "entries: []\n", wantErr: "at least one glob"},
		{name: "not a mapping", src: "- a\n", wantErr: "must be a mapping"},
		{name: "malformed", src: "rules: [\n", wantErr: "yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := Parse([]byte(tc.src), known)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, c)
		})
	}
}

func TestNearestRuleSuggestion(t *testing.T) {
	NearestRule = func(string) []string { return []string{"input-unused"} }
	defer func() { NearestRule = nil }()
	_, err := Parse([]byte("rules:\n  input-unusd: off\n"), known)
	if err == nil || !strings.Contains(err.Error(), "did you mean input-unused?") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadDefault(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	c, err := LoadDefault(root, known)
	if err != nil || c.Dir != "" || !reflect.DeepEqual(c, Defaults()) {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("schema: 1\nrules:\n  input-unused: off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = LoadDefault(root, known)
	if err != nil || c.Dir != root || !c.Rules["input-unused"].Off {
		t.Fatalf("present file: %+v %v", c, err)
	}
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("bogus: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDefault(root, known); err == nil || !strings.Contains(err.Error(), FileName) {
		t.Fatalf("bad file err = %v", err)
	}
}

func TestApply(t *testing.T) {
	t.Parallel()
	c := Config{Rules: map[string]Override{
		"input-unused":            {Off: true},
		"needs-output-not-needed": {Severity: finding.Note},
	}}
	in := []finding.Finding{
		{Rule: "input-unused", Severity: finding.Warning},
		{Rule: "needs-output-not-needed", Severity: finding.Error},
		{Rule: "needs-unknown", Severity: finding.Error},
	}
	got := ApplyAll(c, in)
	if len(got) != 2 || got[0].Rule != "needs-output-not-needed" || got[0].Severity != finding.Note || got[1].Severity != finding.Error {
		t.Fatalf("got %+v", got)
	}
	if out := ApplyAll(Config{}, in); len(out) != 3 {
		t.Fatalf("empty config should pass through, got %d", len(out))
	}
}

func TestLoadRecordsPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "custom.jobsworth.yml")
	if err := os.WriteFile(p, []byte("schema: 1\ncheckout:\n  actions: [x/y]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != p || c.Dir != dir {
		t.Fatalf("path %q dir %q", c.Path, c.Dir)
	}
	if !containsNote(c.Notes, "checkout.actions") {
		t.Fatalf("notes = %v", c.Notes)
	}
}

func containsNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}
