package jq

import (
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/shape"
)

func TestParseAccepts(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		".", "..", ".foo", ".foo.bar", ".[0]", ".[]", ".[1:3]", ".[:2]", ".[2:]", `."with space"`, `.["key"]`,
		".a | .b", ".a, .b", ".a // .b", ".a == .b", ".a != 1 and .b < 2 or not", ".a + .b - 1 * 2 / 3 % 4", "-.a",
		"[.[] | .x]", "{a: .x, b, \"c\": 1, (.k): .v, $v}", "{}", "[]", `"text \(.a) more"`, "$x", "$ENV.HOME", "$__loc__",
		"if .a then .b elif .c then .d else .e end", "if .a then .b end", "try .a catch .b", "try .a", ".a?", ".[]?",
		".x as $v | $v.y", "reduce .[] as $i (0; . + $i)", "foreach .[] as $i (0; . + $i; [., $i])",
		"def f: .a; f", "def f(x): x | .a; f(.b)", "def f($x): $x + 1; f(2)", "def f: def g: 1; g; f",
		"keys", "length", "map(.x)", "select(.a == 1)", "to_entries", "from_entries", "with_entries(.value |= tostring)",
		"has(\"a\")", "in({})", "add", "any", "all", "unique", "sort", "sort_by(.x)", "group_by(.x)", "reverse", "first", "last",
		"range(3)", "tostring", "tonumber", "ascii_downcase", "split(\",\")", "join(\",\")", "test(\"x\")", "match(\"x\")",
		"capture(\"(?<a>x)\")", "scan(\"x\")", "sub(\"a\"; \"b\")", "gsub(\"a\"; \"b\"; \"g\")", "walk(.)", "paths", "env", "input", "inputs",
		"@json", "@csv", "@base64", "@base64 \"x\\(.a)\"", ".a |= 1", ".a += 1", ".a = .b", ".a //= 1",
		"# comment\n.a", "limit(2; .[])", "[.[] | select(.x)] | length", "(.a, .b) | .c", ".a.b.c[0].d", "\"a\" + \"b\"",
		"[1,2][0]", "{a:1}.a", ".a[\"b\"]", "1e3", ".5", "label $out | .a", "ltrimstr(\"x\")",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(src); err != nil {
				t.Fatalf("%q: %v", src, err)
			}
		})
	}
}

func TestParseErrorsWithPositions(t *testing.T) {
	t.Parallel()
	for src, pos := range map[string]int{
		".a |":             4,
		"[.a":              3,
		"{a: 1":            5,
		"if .a then .b":    13,
		"try":              3,
		".a as x | .b":     6,
		"def f .a; f":      6,
		`"unterminated`:    0,
		".a ]":             3,
		"reduce .[] as $i": 16,
		"$":                0,
		"@":                1,
		".a + ":            5,
	} {
		_, err := Parse(src)
		if err == nil {
			t.Errorf("%q: expected error", src)
			continue
		}
		if e, ok := err.(*Error); !ok || e.Pos != pos {
			t.Errorf("%q: %v, want position %d", src, err, pos)
		}
	}
}

func obj(fields map[string]*shape.Shape) *shape.Shape { return shape.ObjectShape(fields) }

func input() *shape.Shape {
	return obj(map[string]*shape.Shape{
		"name":  shape.StringShape(),
		"count": shape.NumberShape(),
		"flag":  shape.BoolShape(),
		"items": shape.ArrayOf(obj(map[string]*shape.Shape{"id": shape.NumberShape(), "tag": shape.StringShape()})),
		"meta":  obj(map[string]*shape.Shape{"a": shape.StringShape(), "b": shape.NumberShape()}),
	})
}

func TestApply(t *testing.T) {
	t.Parallel()
	cases := []struct {
		filter string
		in     *shape.Shape
		opts   Options
		want   string
	}{
		{".", input(), Options{}, "object{count, flag, items, meta, name}"},
		{".name", input(), Options{}, "string"},
		{".meta.b", input(), Options{}, "number"},
		{".missing", input(), Options{}, "null"},
		{".items[0]", input(), Options{}, "object{id, tag}"},
		{".items[]", input(), Options{}, "object{id, tag}"},
		{".items[] | .tag", input(), Options{}, "string"},
		{".items | map(.id)", input(), Options{}, "array<number>"},
		{"[.items[] | .id]", input(), Options{}, "array<number>"},
		{".items | length", input(), Options{}, "number"},
		{"keys", input(), Options{}, "array<string>"},
		{".items | first", input(), Options{}, "object{id, tag}"},
		{".items | sort_by(.id) | reverse", input(), Options{}, "array<object{id, tag}>"},
		{".items | group_by(.tag)", input(), Options{}, "array<array<object{id, tag}>>"},
		{".meta | to_entries", input(), Options{}, "array<object{key, value}>"},
		{".meta | map_values(tostring)", input(), Options{}, "object{a, b}"},
		{"{n: .name, c: .count}", input(), Options{}, "object{c, n}"},
		{"{name, count}", input(), Options{}, "object{count, name}"},
		{"{(.name): 1}", input(), Options{}, "object{…}"},
		{".name, .count", input(), Options{}, "any"},
		{".name // \"x\"", input(), Options{}, "string"},
		{".count + 1", input(), Options{}, "number"},
		{".name + \"!\"", input(), Options{}, "string"},
		{".items + .items", input(), Options{}, "array<object{id, tag}>"},
		{".count > 1", input(), Options{}, "boolean"},
		{"if .flag then .name else .name end", input(), Options{}, "string"},
		{"if .flag then .name else .count end", input(), Options{}, "any"},
		{"try .name catch \"x\"", input(), Options{}, "string"},
		{".name as $n | {n: $n}", input(), Options{}, "object{n}"},
		{"reduce .items[] as $i (0; . + $i.id)", input(), Options{}, "any"},
		{"def ids: .items | map(.id); ids", input(), Options{}, "array<number>"},
		{"def twice(f): f | f; twice(.meta)", input(), Options{}, "any"},
		{"def f($x): {v: $x}; f(.name)", input(), Options{}, "object{v}"},
		{"def loop: loop; loop", input(), Options{}, "any"},
		{"tostring", input(), Options{}, "string"},
		{"@json", input(), Options{}, "string"},
		{"\"v=\\(.count)\"", input(), Options{}, "string"},
		{"$v.x", input(), Options{Vars: map[string]*shape.Shape{"v": obj(map[string]*shape.Shape{"x": shape.BoolShape()})}}, "boolean"},
		{"$s", input(), Options{Vars: map[string]*shape.Shape{"s": shape.StringShape()}}, "string"},
		{".", input(), Options{Slurp: true}, "array<object{count, flag, items, meta, name}>"},
		{".", input(), Options{NullInput: true}, "null"},
		{"..", input(), Options{}, "any"},
		{"walk(.)", input(), Options{}, "any"},
		{"unknownbuiltin", input(), Options{}, "any"},
		{".items[] | select(.id > 1)", input(), Options{}, "object{id, tag}"},
		{".name | split(\",\")", input(), Options{}, "array<string>"},
		{".name | test(\"x\")", input(), Options{}, "boolean"},
		{"has(\"name\")", input(), Options{}, "boolean"},
		{"env.HOME", input(), Options{}, "any"},
		{"$ENV.HOME", input(), Options{}, "any"},
		{".x", shape.UnknownObj(), Options{}, "any"},
		{".x", shape.OpenObject(nil), Options{}, "any"},
		{".[0]", shape.UnknownArr(), Options{}, "any"},
		{".a.b.c", shape.AnyShape(), Options{}, "any"},
	}
	for _, tc := range cases {
		t.Run(tc.filter, func(t *testing.T) {
			t.Parallel()
			got, err := ApplyString(tc.filter, tc.in, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestInterpolationErrorsReported(t *testing.T) {
	t.Parallel()
	_, err := Parse(`"bad \(.a |) end"`)
	if err == nil || !strings.Contains(err.Error(), "interpolation") {
		t.Fatalf("err = %v", err)
	}
}
