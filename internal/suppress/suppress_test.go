package suppress

import (
	"reflect"
	"sort"
	"testing"

	"github.com/najemi-software/jobsworth/internal/finding"
)

func known(id string) bool {
	return id == "a-rule" || id == "b-rule" || id == "old-rule"
}

func resolve(id string) string {
	if id == "old-rule" {
		return "a-rule"
	}
	return id
}

func ids(fs []finding.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	sort.Strings(out)
	return out
}

func TestParseForms(t *testing.T) {
	t.Parallel()
	src := `on: push                       # jobsworth-ignore a-rule -- same line
# jobsworth-ignore b-rule, a-rule -- own line applies below

jobs:
  a:
    run: |
      # jobsworth-ignore a-rule   (inside a block scalar: bash, ignored)
      echo "# jobsworth-ignore a-rule"
    name: "quoted # jobsworth-ignore a-rule"
# jobsworth-ignore-file b-rule
# jobsworth-ignore-start a-rule
  b: {}
# jobsworth-ignore-end
# jobsworth-ignore-start b-rule -- never closed
  c: {}
  d: 1   # jobsworth-ignore *
  e: 1   # jobsworth-ignore typo-rule
`
	set := Parse([]byte(src), known, resolve)
	type want struct {
		kind     string
		rules    []string
		wild     bool
		reason   string
		anchor   int
		from, to int
	}
	got := make([]want, 0, len(set.Directives))
	for _, d := range set.Directives {
		got = append(got, want{d.Kind, d.Rules, d.Wildcard, d.Reason, d.Anchor, d.From, d.To})
	}
	exp := []want{
		{"line", []string{"a-rule"}, false, "same line", 1, 0, 0},
		{"line", []string{"b-rule", "a-rule"}, false, "own line applies below", 4, 0, 0},
		{"file", []string{"b-rule"}, false, "", 0, 1, 18},
		{"region", []string{"a-rule"}, false, "", 0, 11, 13},
		{"region", []string{"b-rule"}, false, "never closed", 0, 14, 18},
		{"line", nil, true, "", 16, 0, 0},
		{"line", nil, false, "", 17, 0, 0},
	}
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("directives:\n got %+v\nwant %+v", got, exp)
	}
	if p := ids(set.Problems); !reflect.DeepEqual(p, []string{"suppress-unclosed", "suppress-unknown-rule"}) {
		t.Fatalf("problems = %v", p)
	}
}

func TestBareAndStrayDirectives(t *testing.T) {
	t.Parallel()
	set := Parse([]byte("a: 1  # jobsworth-ignore\n# jobsworth-ignore-end\nb: 2 # jobsworth-ignore -- just a reason\n"), known, resolve)
	got := ids(set.Problems)
	if !reflect.DeepEqual(got, []string{"suppress-unclosed", "suppress-unknown-rule", "suppress-unknown-rule"}) {
		t.Fatalf("problems = %v", got)
	}
	kept, _ := Apply(set, []finding.Finding{{Rule: "a-rule", Line: 1}})
	if len(kept) != 4 {
		t.Fatalf("bare directive must not suppress anything: %+v", kept)
	}
}

func TestAliasResolves(t *testing.T) {
	t.Parallel()
	set := Parse([]byte("x: 1 # jobsworth-ignore old-rule\n"), known, resolve)
	if len(set.Directives) != 1 || set.Directives[0].Rules[0] != "a-rule" {
		t.Fatalf("directives = %+v", set.Directives)
	}
}

func TestApply(t *testing.T) {
	t.Parallel()
	src := `l1: 1  # jobsworth-ignore a-rule -- because
# jobsworth-ignore b-rule
l3: 1
l4: 1
# jobsworth-ignore-start a-rule
l6: 1
# jobsworth-ignore-end
l8: 1 # jobsworth-ignore *
l9: 1 # jobsworth-ignore a-rule
`
	set := Parse([]byte(src), known, resolve)
	in := []finding.Finding{
		{Rule: "a-rule", Line: 1},            // suppressed, with reason
		{Rule: "b-rule", Line: 1},            // kept: directive names a-rule only
		{Rule: "b-rule", Line: 3},            // suppressed by own-line comment
		{Rule: "a-rule", Line: 4},            // kept
		{Rule: "a-rule", Line: 6},            // suppressed by region
		{Rule: "b-rule", Line: 6},            // kept
		{Rule: "a-rule", Line: 8},            // suppressed by wildcard
		{Rule: "suppress-wildcard", Line: 8}, // meta-rules never suppressed
	}
	kept, suppressed := Apply(set, in)
	if got := ids(suppressed); !reflect.DeepEqual(got, []string{"a-rule", "a-rule", "a-rule", "b-rule"}) {
		t.Fatalf("suppressed = %v", got)
	}
	if suppressed[0].Reason != "because" {
		t.Fatalf("reason = %q", suppressed[0].Reason)
	}
	wantKept := []string{"a-rule", "b-rule", "b-rule", "suppress-unused", "suppress-wildcard", "suppress-wildcard"}
	if got := ids(kept); !reflect.DeepEqual(got, wantKept) {
		t.Fatalf("kept = %v want %v", got, wantKept)
	}
	for _, f := range kept {
		if f.Rule == "suppress-unused" && f.Line != 9 {
			t.Fatalf("unused should point at line 9, got %d", f.Line)
		}
	}
}

func TestNilSetPassesThrough(t *testing.T) {
	t.Parallel()
	in := []finding.Finding{{Rule: "x"}}
	kept, sup := Apply(nil, in)
	if len(kept) != 1 || len(sup) != 0 {
		t.Fatal("nil set should pass everything through")
	}
}

func TestSplitComment(t *testing.T) {
	t.Parallel()
	for in, want := range map[string][2]string{
		"a: b # c":                {"a: b ", " c"},
		"a: 'x # y' # c":          {"a: 'x # y' ", " c"},
		`a: "x # y" # c`:          {`a: "x # y" `, " c"},
		"a: b#c":                  {"a: b#c", ""},
		"# only":                  {"", " only"},
		"a: \"esc\\\" # no\" # c": {"a: \"esc\\\" # no\" ", " c"},
	} {
		code, comment := splitComment(in)
		if code != want[0] || comment != want[1] {
			t.Errorf("splitComment(%q) = %q,%q want %q,%q", in, code, comment, want[0], want[1])
		}
	}
}
