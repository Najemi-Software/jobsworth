package expr

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func kinds(toks []Token) []string {
	var out []string
	for _, t := range toks {
		if t.Kind == TokEOF {
			break
		}
		out = append(out, t.Kind.String()+":"+t.Text)
	}
	return out
}

func TestTokenize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		want []string
	}{
		{"github.ref", []string{"ident:github", "op:.", "ident:ref"}},
		{"'it''s'", []string{"string:it's"}},
		{"42 0x1F 1.5e3 -3", []string{"number:42", "number:0x1F", "number:1.5e3", "number:-3"}},
		{"a == b && !c || d != e", []string{"ident:a", "op:==", "ident:b", "op:&&", "op:!", "ident:c", "op:||", "ident:d", "op:!=", "ident:e"}},
		{"x <= 1 && y >= 2 && z < 3 && w > 4", []string{"ident:x", "op:<=", "number:1", "op:&&", "ident:y", "op:>=", "number:2", "op:&&", "ident:z", "op:<", "number:3", "op:&&", "ident:w", "op:>", "number:4"}},
		{"f(a, b)[0].*", []string{"ident:f", "op:(", "ident:a", "op:,", "ident:b", "op:)", "op:[", "number:0", "op:]", "op:.", "op:*"}},
		{"inputs.my-input", []string{"ident:inputs", "op:.", "ident:my-input"}},
		{"a - 1", nil}, // '-' is not an operator; a binary minus is an error
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			toks, err := Tokenize(tc.src)
			if tc.want == nil {
				if err == nil {
					t.Fatalf("expected error, got %v", kinds(toks))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := kinds(toks); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	if _, err := Tokenize("'open"); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("unterminated string: %v", err)
	}
	if _, err := Tokenize("a $ b"); err == nil || !strings.Contains(err.Error(), "at 2") {
		t.Fatalf("error position: %v", err)
	}
}

// render prints an AST in a compact prefix form for assertions.
func render(n Node) string {
	switch x := n.(type) {
	case *Literal:
		switch x.Kind {
		case LitString:
			return "'" + x.Str + "'"
		case LitNumber:
			return strconv.FormatFloat(x.Num, 'g', -1, 64)
		case LitBool:
			if x.Bool {
				return "true"
			}
			return "false"
		}
		return "null"
	case *Ident:
		return x.Name
	case *Member:
		return render(x.X) + "." + x.Name
	case *Index:
		return render(x.X) + "[" + render(x.Idx) + "]"
	case *Wildcard:
		return render(x.X) + ".*"
	case *Call:
		var args []string
		for _, a := range x.Args {
			args = append(args, render(a))
		}
		return x.Name + "(" + strings.Join(args, ", ") + ")"
	case *Unary:
		return "(" + x.Op + render(x.X) + ")"
	case *Binary:
		return "(" + render(x.X) + " " + x.Op + " " + render(x.Y) + ")"
	}
	return "?"
}

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		want string
	}{
		{"a || b && c", "(a || (b && c))"},
		{"a == b || c != d", "((a == b) || (c != d))"},
		{"a < b == c", "((a < b) == c)"},
		{"!a && b", "((!a) && b)"},
		{"!(a && b)", "(!(a && b))"},
		{"(a || b) && c", "((a || b) && c)"},
		{"github.event.inputs['x'].y", "github.event.inputs['x'].y"},
		{"steps.meta.outputs.version", "steps.meta.outputs.version"},
		{"matrix.os[0]", "matrix.os[0]"},
		{"a[b.c]", "a[b.c]"},
		{"fromJSON(inputs.cfg).items.*", "fromJSON(inputs.cfg).items.*"},
		{"contains(github.ref, 'refs/tags/') && startsWith(a, 'x')", "(contains(github.ref, 'refs/tags/') && startsWith(a, 'x'))"},
		{"success()", "success()"},
		{"'it''s' == x", "('it's' == x)"},
		{"true && FALSE || null", "((true && false) || null)"},
		{"-1 == a", "(-1 == a)"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			n, err := Parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := render(n); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	for src, pos := range map[string]int{
		"a &&":          4,
		"(a":            2,
		"a[1":           3,
		"f(a":           3,
		"a.":            1,
		"a b":           2,
		"== a":          0,
		"a..b":          1,
		"fromJSON(a) )": 12,
	} {
		_, err := Parse(src)
		if err == nil {
			t.Errorf("%q: expected error", src)
			continue
		}
		if e, ok := err.(*Error); !ok || e.Pos != pos {
			t.Errorf("%q: error %v, want position %d", src, err, pos)
		}
	}
}

func chainStrings(src string) []string {
	n, err := Parse(src)
	if err != nil {
		return []string{"ERR " + err.Error()}
	}
	var out []string
	var render func(c Chain) string
	render = func(c Chain) string {
		s := c.String()
		if c.Inner != nil {
			s += " <- " + render(*c.Inner)
		}
		return s
	}
	for _, c := range Chains(n) {
		out = append(out, render(c))
	}
	return out
}

func TestChains(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		want []string
	}{
		{"inputs.env", []string{"inputs.env"}},
		{"inputs['my-input'].x", []string{"inputs.my-input.x"}},
		{"matrix.os[0].name", []string{"matrix.os[0].name"}},
		{"a[b.c].d", []string{"a[…].d", "b.c"}},
		{"github.event.commits.*.message", []string{"github.event.commits.*.message"}},
		{"fromJSON(inputs.cfg).items[0]", []string{"fromJSON(inputs.cfg).items[0] <- inputs.cfg"}},
		{"fromJSON('{\"a\":1}').a", []string{"fromJSON('{\"a\":1}').a <- '{\"a\":1}'"}},
		{"toJSON(matrix)", []string{"toJSON(matrix) <- matrix"}},
		{"contains(github.ref, inputs.x) && !failure()", []string{"github.ref", "inputs.x"}},
		{"steps.a.outputs.x == needs.b.outputs.y", []string{"steps.a.outputs.x", "needs.b.outputs.y"}},
		{"'literal'", nil},
		{"format('{0}', env.A, env.B)", []string{"env.A", "env.B"}},
		{"fromJSON(toJSON(matrix)).os", []string{"fromJSON(toJSON(matrix)).os <- toJSON(matrix) <- matrix"}},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			if got := chainStrings(tc.src); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"fromjson( inputs.x )", "fromJSON(inputs.x)"},
		{"steps.a.outputs.b[ 0 ] . c", "steps.a.outputs.b[0].c"},
		{"github.event_name=='push'&&!cancelled()", "github.event_name == 'push' && !cancelled()"},
		{"(a || b) && c", "(a || b) && c"},
		{"a || (b && c)", "a || b && c"},
		{"a == (b == c)", "a == (b == c)"},
		{"format('{0} it''s', 1.50, true, null)", "format('{0} it''s', 1.5, true, null)"},
		{"matrix.*.name", "matrix.*.name"},
		{"inputs[github.ref]", "inputs[github.ref]"},
	} {
		n, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got := Format(n); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.in, got, tc.want)
		}
		if _, err := Parse(tc.want); err != nil {
			t.Errorf("%q: formatted text does not parse: %v", tc.want, err)
		}
	}
}
