package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/config"
)

const (
	wfYAML     = "on: push\njobs:\n  a:\n    runs-on: ubuntu\n    steps: []\nname: W2\npermissions: read-all\n"
	actionYAML = "runs:\n  using: composite\n  steps: []\nname: N\ndescription: D\n"
	configYAML = "repos:\n  a: b\n"
)

// tree builds a directory tree from relative path → content.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func rel(root string, paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		r, _ := filepath.Rel(root, p)
		out[i] = filepath.ToSlash(r)
	}
	return out
}

func TestExpandEntries(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{
		".github/workflows/ci.yml":            wfYAML,
		".github/workflows/release.yaml":      wfYAML,
		".github/workflows/notes.md":          "# nope",
		".github/workflows/config.yml":        configYAML,
		".github/actions/a/action.yml":        actionYAML,
		".github/actions/nested/b/action.yml": actionYAML,
		".github/actions/data.yml":            configYAML,
		"other/x.yml":                         wfYAML,
		"home/w.yml":                          wfYAML,
	})
	home := filepath.Join(root, "home")

	cases := []struct {
		name    string
		args    []string
		want    []string
		wantErr string
	}{
		{
			name: "defaults",
			args: nil,
			want: []string{
				".github/actions/a/action.yml",
				".github/actions/nested/b/action.yml",
				".github/workflows/ci.yml",
				".github/workflows/release.yaml",
			},
		},
		{
			name: "literal is used as-is even when not a workflow",
			args: []string{".github/workflows/config.yml"},
			want: []string{".github/workflows/config.yml"},
		},
		{
			name: "glob filters non-workflow yaml",
			args: []string{".github/workflows/*.yml"},
			want: []string{".github/workflows/ci.yml"},
		},
		{
			name: "doublestar",
			args: []string{".github/**/*.yml"},
			want: []string{
				".github/actions/a/action.yml",
				".github/actions/nested/b/action.yml",
				".github/workflows/ci.yml",
			},
		},
		{
			name: "tilde",
			args: []string{"~/w.yml"},
			want: []string{"home/w.yml"},
		},
		{
			name: "dedupe and sort",
			args: []string{"other/x.yml", ".github/workflows/ci.yml", "other/x.yml", ".github/workflows/*.yml"},
			want: []string{".github/workflows/ci.yml", "other/x.yml"},
		},
		{
			name: "absolute literal",
			args: []string{filepath.Join(root, "other", "x.yml")},
			want: []string{"other/x.yml"},
		},
		{name: "missing literal", args: []string{"nope.yml"}, wantErr: "entry nope.yml"},
		{name: "directory literal", args: []string{".github/workflows"}, wantErr: "is a directory"},
		{name: "glob matches nothing", args: []string{"nothing/*.yml"}, wantErr: "no workflow or action files match"},
		{name: "glob matches only non-workflows", args: []string{".github/workflows/*.md"}, wantErr: "no workflow or action files match"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExpandEntriesWith(tc.args, config.DefaultEntries, root, home)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r := rel(root, got); !reflect.DeepEqual(r, tc.want) {
				t.Fatalf("got %v want %v", r, tc.want)
			}
		})
	}
}

func TestExpandEntriesDefaultsAreASet(t *testing.T) {
	t.Parallel()
	onlyWorkflows := tree(t, map[string]string{".github/workflows/ci.yml": wfYAML})
	got, err := ExpandEntriesWith(nil, config.DefaultEntries, onlyWorkflows, "")
	if err != nil || len(got) != 1 {
		t.Fatalf("workflows without actions dir: %v %v", got, err)
	}
	onlyActions := tree(t, map[string]string{".github/actions/a/action.yml": actionYAML})
	got, err = ExpandEntriesWith(nil, config.DefaultEntries, onlyActions, "")
	if err != nil || len(got) != 1 {
		t.Fatalf("actions without workflows dir: %v %v", got, err)
	}
	empty := t.TempDir()
	if _, err := ExpandEntriesWith(nil, config.DefaultEntries, empty, ""); err == nil || !strings.Contains(err.Error(), "no workflow or action files found") {
		t.Fatalf("empty dir err = %v", err)
	}
}

func TestExpandTilde(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"~":        "/h",
		"~/x":      "/h/x",
		"~x":       "~x",
		"a/~/b":    "a/~/b",
		"/abs/~":   "/abs/~",
		"relative": "relative",
	} {
		if got := ExpandTilde(in, "/h"); got != want {
			t.Errorf("ExpandTilde(%q) = %q want %q", in, got, want)
		}
	}
}

func TestIsGlob(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{
		"a.yml": false, "*.yml": true, "a?.yml": true, "[ab].yml": true, "a.{yml,yaml}": true, "**/x": true,
	} {
		if got := IsGlob(in); got != want {
			t.Errorf("IsGlob(%q) = %v", in, got)
		}
	}
}
