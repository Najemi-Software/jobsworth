package shape

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checkcmd "github.com/najemi-software/jobsworth/cmd/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

var update = flag.Bool("update", false, "rewrite golden files")

const testdata = "../../testdata"

// repo copies testdata/shape into a temp dir (outside any git checkout).
func repo(t *testing.T, kit *testkit.Kit) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(kit.Testdata, "shape")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func run(t *testing.T, kit *testkit.Kit, cwd string, tokens ...string) (stdout, stderr string, code int) {
	t.Helper()
	args, flags, err := cli.ParseArgs(Schema, tokens)
	if err == nil {
		err = cli.Validate(Schema, args, flags)
	}
	if err != nil {
		return "", err.Error(), cli.ExitUsage
	}
	env := map[string]string{"JOBSWORTH_CACHE_DIR": kit.CacheDir, "JOBSWORTH_API_URL": kit.Server.URL, "JOBSWORTH_TOKEN": "test-token"}
	var out, errb bytes.Buffer
	err = RunWith(args, flags, checkcmd.Env{Cwd: cwd, Home: kit.Home, Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &errb})
	if err != nil && err.Error() != "" {
		errb.WriteString(err.Error())
	}
	stable := strings.ReplaceAll(out.String(), cwd, "<ROOT>")
	return stable, strings.ReplaceAll(errb.String(), cwd, "<ROOT>"), cli.ExitCode(err, cli.ExitFindings)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(testdata, "golden", "shape", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func TestUsageErrors(t *testing.T) {
	kit := testkit.New(t, testdata)
	cwd := repo(t, kit)
	for _, tc := range []struct {
		name   string
		tokens []string
		want   string
	}{
		{"no expression", []string{".github/workflows/release.yml"}, "expected <path>"},
		{"three args", []string{"a", "b", "c"}, "expected"},
		{"glob", []string{".github/workflows/*.yml", "x"}, "not a glob"},
		{"bad depth", []string{"--depth", "0", ".github/workflows/release.yml", "x"}, "--depth"},
		{"bad format", []string{"--format", "yaml", ".github/workflows/release.yml", "x"}, "format"},
		{"missing file", []string{"nope.yml", "x"}, "nope.yml"},
		{"action with job", []string{"--at", "job/s", ".github/actions/meta/action.yml", "steps.s.outputs.sha"}, "actions have no jobs"},
	} {
		_, stderr, code := run(t, kit, cwd, tc.tokens...)
		if code != cli.ExitUsage || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: code %d stderr %q (want usage error with %q)", tc.name, code, stderr, tc.want)
		}
	}
}

func TestExitOne(t *testing.T) {
	kit := testkit.New(t, testdata)
	cwd := repo(t, kit)
	for _, tc := range []struct {
		name   string
		tokens []string
		want   string
	}{
		{"parse error", []string{".github/workflows/release.yml", "github.event_name == \"push\""}, "does not parse"},
		{"needs --at", []string{".github/workflows/release.yml", "steps.x.outputs.y"}, "pass --at"},
		{"unknown job", []string{"--at", "nope", ".github/workflows/release.yml", "env.X"}, "no job named"},
		{"unknown step id", []string{"--at", "set-vars", ".github/workflows/release.yml", "steps.nope.outputs.x"}, "no step has id"},
		{"forward step", []string{"--at", "set-vars/build-clusters-matrix", ".github/workflows/release.yml", "steps.build-clusters-matrix.outputs.canary"}, "unknown at this point"},
		{"not needed", []string{"--at", "set-vars", ".github/workflows/release.yml", "needs.deploy.result"}, "not in its needs"},
		{"mismatch", []string{"--at", "deploy", ".github/workflows/release.yml", "fromJSON(needs.set-vars.outputs.canary)[0].region"}, "✗ field \"region\" not in object"},
		{"value set", []string{".github/workflows/release.yml", "github.event_name == 'schedule'"}, "value-set-mismatch"},
	} {
		stdout, stderr, code := run(t, kit, cwd, tc.tokens...)
		if code != cli.ExitFindings || !strings.Contains(stdout+stderr, tc.want) {
			t.Errorf("%s: code %d, output %q %q (want exit 1 with %q)", tc.name, code, stdout, stderr, tc.want)
		}
	}
}

func TestGoldens(t *testing.T) {
	kit := testkit.New(t, testdata)
	cwd := repo(t, kit)
	rel := ".github/workflows/release.yml"
	for _, tc := range []struct {
		name   string
		tokens []string
		code   int
	}{
		{"steps_json_chain.txt", []string{"--at", "set-vars", rel, "fromJSON(steps.build-clusters-matrix.outputs.canary)"}, 0},
		{"steps_raw.txt", []string{"--at", "set-vars", rel, "steps.build-clusters-matrix.outputs.canary"}, 0},
		{"needs_lift.txt", []string{"--at", "deploy", rel, "fromJSON(needs.set-vars.outputs.canary)[0].cluster"}, 0},
		{"needs_mismatch.txt", []string{"--at", "deploy", rel, "fromJSON(needs.set-vars.outputs.canary)[0].region"}, 1},
		{"needs_result.txt", []string{"--at", "deploy", rel, "needs.set-vars.result"}, 0},
		{"matrix.txt", []string{"--at", "deploy", rel, "matrix.os"}, 0},
		{"env_write.txt", []string{"--at", "set-vars/#3", rel, "env.VERSION"}, 0},
		{"env_block.txt", []string{"--at", "set-vars", rel, "env.REGION"}, 0},
		{"env_not_yet.txt", []string{"--at", "set-vars/build-clusters-matrix", rel, "env.VERSION"}, 0},
		{"static_bool.txt", []string{rel, "github.event_name == 'push'"}, 0},
		{"step_lift.txt", []string{"--at", "deploy", rel, "steps.meta.outputs.sha"}, 0},
		{"inputs_default.txt", []string{".github/workflows/reuse.yml", "fromJSON(inputs.cfg).replicas"}, 0},
		{"inputs_mismatch.txt", []string{".github/workflows/reuse.yml", "fromJSON(inputs.cfg).replicas.count"}, 1},
		{"inputs_number.txt", []string{".github/workflows/reuse.yml", "inputs.count"}, 0},
		{"literal.txt", []string{rel, "'hello'"}, 0},
		{"dynamic_index.txt", []string{rel, "inputs[github.ref]"}, 0},
		{"action_step.txt", []string{".github/actions/meta/action.yml", "steps.s.outputs.sha"}, 0},
		{"action_forward.txt", []string{"--at", "s", ".github/actions/meta/action.yml", "steps.s.outputs.sha"}, 1},
		{"no_why.txt", []string{"--no-why", "--at", "set-vars", rel, "fromJSON(steps.build-clusters-matrix.outputs.canary)"}, 0},
		{"depth1.txt", []string{"--depth", "1", "--at", "set-vars", rel, "fromJSON(steps.build-clusters-matrix.outputs.canary)"}, 0},
		{"mix_values.txt", []string{"--at", "build", ".github/workflows/mix.yml", "inputs.level"}, 0},
		{"inputs_two_triggers.txt", []string{".github/workflows/two.yml", "inputs.level"}, 0},
		{"spacing.txt", []string{"--no-why", "--at", "set-vars", rel, "fromjson( steps.build-clusters-matrix.outputs.canary )[ 0 ] . cluster"}, 0},
		{"all_release.txt", []string{"--all", rel}, 0},
		{"all_mix_job.txt", []string{"--all", "--at", "build", ".github/workflows/mix.yml"}, 0},
		{"steps_json_chain.json", []string{"--format", "json", "--at", "set-vars", rel, "fromJSON(steps.build-clusters-matrix.outputs.canary)"}, 0},
		{"needs_mismatch.json", []string{"--format", "json", "--at", "deploy", rel, "fromJSON(needs.set-vars.outputs.canary)[0].region"}, 1},
		{"all_mix.json", []string{"--all", "--format", "json", ".github/workflows/mix.yml"}, 0},
		{"all_mix_why.json", []string{"--all", "--why", "--format", "json", "--at", "build", ".github/workflows/mix.yml"}, 0},
	} {
		stdout, stderr, code := run(t, kit, cwd, tc.tokens...)
		if code != tc.code {
			t.Errorf("%s: exit %d want %d: %s", tc.name, code, tc.code, stderr)
		}
		golden(t, tc.name, stdout)
		if strings.HasSuffix(tc.name, ".json") {
			var v any
			if err := json.Unmarshal([]byte(stdout), &v); err != nil {
				t.Errorf("%s: invalid json: %v", tc.name, err)
			}
		}
	}
}

// TestJSONSchema pins the §4.3 field names so a rename fails loudly.
func TestJSONSchema(t *testing.T) {
	kit := testkit.New(t, testdata)
	cwd := repo(t, kit)
	stdout, _, _ := run(t, kit, cwd, "--format", "json", "--at", "set-vars", ".github/workflows/release.yml", "fromJSON(steps.build-clusters-matrix.outputs.canary)")
	var doc struct {
		Schema int `json:"schema"`
		File   string
		At     struct {
			Job  *string `json:"job"`
			Step *string `json:"step"`
		}
		Expression string
		OK         bool `json:"ok"`
		Shape      struct {
			Kind string
			Elem struct {
				Kind   string
				Open   bool
				Fields map[string]struct{ Kind string }
			}
		}
		Render   string
		Why      []struct{ Kind string }
		Accepted []struct {
			Access string
			Shape  struct{ Kind string }
		}
		Error *struct{ Message, At, Rule string }
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != 1 || doc.At.Job == nil || *doc.At.Job != "set-vars" || doc.At.Step != nil || !doc.OK || doc.Error != nil {
		t.Errorf("header: %+v", doc)
	}
	if doc.Shape.Kind != "array" || doc.Shape.Elem.Kind != "object" || doc.Shape.Elem.Open || doc.Shape.Elem.Fields["cluster"].Kind != "string" {
		t.Errorf("shape: %+v", doc.Shape)
	}
	if doc.Render != "Array[Object{cluster: String, namespace: String}]" || len(doc.Why) < 5 || len(doc.Accepted) != 4 || doc.Accepted[0].Access != "[n]" {
		t.Errorf("render %q why %d accepted %+v", doc.Render, len(doc.Why), doc.Accepted)
	}
}
