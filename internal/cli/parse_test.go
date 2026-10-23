package cli

import (
	"strings"
	"testing"
)

var testSchema = Schema{
	MinArgs: 1, MaxArgs: 2, ArgNames: []string{"target"},
	Flags: []Flag{
		{Name: "format", TakesValue: true, AllowedValues: []string{"text", "json"}},
		{Name: "repo", Aliases: []string{"r"}, TakesValue: true, Repeatable: true},
		{Name: "offline", Aliases: []string{"o"}},
		{Name: "token", TakesValue: true, Required: true},
	},
}

func TestSplitCommand(t *testing.T) {
	t.Parallel()
	if _, _, ok := SplitCommand([]string{"jobsworth"}); ok {
		t.Fatal("no command should not be ok")
	}
	cmd, rest, ok := SplitCommand([]string{"jobsworth", "--version"})
	if !ok || cmd != "--version" || len(rest) != 0 {
		t.Fatalf("flag-looking command: %q %v %v", cmd, rest, ok)
	}
	cmd, rest, _ = SplitCommand([]string{"jobsworth", "check", "a", "--json"})
	if cmd != "check" || strings.Join(rest, " ") != "a --json" {
		t.Fatalf("check: %q %v", cmd, rest)
	}
}

func TestParseArgs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		tokens []string
		args   string
		flags  string // name=v1|v2;... sorted by name
		err    string
	}{
		{"positional only", []string{"a", "b"}, "a b", "", ""},
		{"attached value", []string{"--format=json", "a"}, "a", "format=json", ""},
		{"separate value", []string{"--format", "json", "a"}, "a", "format=json", ""},
		{"repeatable keeps order", []string{"--repo", "x=1", "-r", "y=2", "-r", "z=3", "a"}, "a", "repo=x=1|y=2|z=3", ""},
		{"short alias with =", []string{"-r=x", "a"}, "a", "repo=x", ""},
		{"boolean never swallows", []string{"--offline", "a"}, "a", "offline=", ""},
		{"short boolean", []string{"-o", "a"}, "a", "offline=", ""},
		{"double dash", []string{"--", "--offline", "-x"}, "--offline -x", "", ""},
		{"bare dash is positional", []string{"-"}, "-", "", ""},
		{"unknown flag", []string{"--nope"}, "", "", "unknown flag: --nope"},
		{"boolean with value", []string{"--offline=yes"}, "", "", "takes no value"},
		{"missing value at end", []string{"a", "--format"}, "", "", "requires a value"},
		{"missing value before flag", []string{"--format", "--offline"}, "", "", "requires a value"},
	} {
		args, flags, err := ParseArgs(testSchema, tc.tokens)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := strings.Join(args, " "); got != tc.args {
			t.Errorf("%s: args %q want %q", tc.name, got, tc.args)
		}
		if got := renderFlags(flags); got != tc.flags {
			t.Errorf("%s: flags %q want %q", tc.name, got, tc.flags)
		}
	}
}

func renderFlags(flags map[string][]string) string {
	var names []string
	for n := range flags {
		names = append(names, n)
	}
	sortStrings(names)
	var parts []string
	for _, n := range names {
		parts = append(parts, n+"="+strings.Join(flags[n], "|"))
	}
	return strings.Join(parts, ";")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	ok := map[string][]string{"token": {"t"}}
	for _, tc := range []struct {
		name  string
		args  []string
		flags map[string][]string
		err   string
	}{
		{"valid", []string{"a"}, ok, ""},
		{"too few args", []string{}, ok, "at least 1"},
		{"too many args", []string{"a", "b", "c"}, ok, "at most 2"},
		{"missing required", []string{"a"}, map[string][]string{}, "missing required flag --token"},
		{"repeated non-repeatable", []string{"a"}, map[string][]string{"token": {"t"}, "format": {"json", "text"}}, "given 2 times"},
		{"disallowed value", []string{"a"}, map[string][]string{"token": {"t"}, "format": {"yaml"}}, "invalid value \"yaml\" for --format (allowed: text, json)"},
		{"repeatable many", []string{"a"}, map[string][]string{"token": {"t"}, "repo": {"x", "y"}}, ""},
	} {
		err := Validate(testSchema, tc.args, tc.flags)
		if tc.err == "" && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: err %v, want %q", tc.name, err, tc.err)
		}
	}
	unbounded := Schema{MaxArgs: -1}
	if err := Validate(unbounded, []string{"a", "b", "c", "d"}, nil); err != nil {
		t.Errorf("unbounded: %v", err)
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	h := Help("jobsworth check", testSchema)
	for _, want := range []string{"jobsworth check", "--format", "--repo", "-r", "--offline", "-o", "--token", "<target>"} {
		if !strings.Contains(h, want) {
			t.Errorf("help lacks %q:\n%s", want, h)
		}
	}
}
