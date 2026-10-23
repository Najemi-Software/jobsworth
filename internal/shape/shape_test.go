package shape

import (
	"strings"
	"testing"
)

func obj(fields map[string]*Shape) *Shape { return ObjectShape(fields) }

func TestFromJSONAndYAML(t *testing.T) {
	t.Parallel()
	s, err := FromJSON([]byte(`{"a":1,"b":"x","c":[1,2],"d":{"e":true},"f":null,"g":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.String() != "object{a, b, c, d, f, g}" || s.Fields["a"].Kind != Number || s.Fields["b"].Kind != String ||
		s.Fields["c"].String() != "array<number>" || s.Fields["d"].Fields["e"].Kind != Bool || s.Fields["f"].Kind != Null || s.Fields["g"].String() != "array<any>" {
		t.Fatalf("shape = %s", s)
	}
	if _, err := FromJSON([]byte(`{"a":`)); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := FromJSON([]byte(`{} extra`)); err == nil {
		t.Fatal("expected trailing data error")
	}
	y, err := FromYAML([]byte("a: 1\nb:\n  - x\n  - y\nc:\n  d: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if y.String() != "object{a, b, c}" || y.Fields["b"].String() != "array<string>" {
		t.Fatalf("yaml shape = %s", y)
	}
	if l := FromLiteral(`{"a":1}`); l.Kind != String || l.JSON == nil || l.JSON.Kind != Object {
		t.Fatalf("FromLiteral json = %+v", l)
	}
	if FromLiteral("plain").JSON != nil || FromLiteral("{broken").JSON != nil {
		t.Fatal("FromLiteral non-json")
	}
	if !IsJSONLiteral(`[1,2]`) || IsJSONLiteral("x") {
		t.Fatal("IsJSONLiteral")
	}
}

func TestAccessTable(t *testing.T) {
	t.Parallel()
	closed := obj(map[string]*Shape{"a": NumberShape(), "b": obj(map[string]*Shape{"c": StringShape()})})
	open := OpenObject(map[string]*Shape{"a": NumberShape()})
	arr := ArrayOf(StringShape())
	cases := []struct {
		name    string
		s       *Shape
		seg     Seg
		want    string // resulting shape, or "" for error
		wantErr string
	}{
		{"field present", closed, Seg{Field: "a"}, "number", ""},
		{"nested", closed.Fields["b"], Seg{Field: "c"}, "string", ""},
		{"field missing closed", closed, Seg{Field: "z"}, "", `field "z" not in object (declared: a, b)`},
		{"field missing open", open, Seg{Field: "z"}, "any", ""},
		{"field on unknown object", UnknownObj(), Seg{Field: "z"}, "any", ""},
		{"field on array", arr, Seg{Field: "z"}, "", `field "z" on array`},
		{"field on unknown array", UnknownArr(), Seg{Field: "z"}, "", `field "z" on array`},
		{"field on string", StringShape(), Seg{Field: "z"}, "", `field "z" on string`},
		{"field on number", NumberShape(), Seg{Field: "z"}, "", `field "z" on number`},
		{"field on bool", BoolShape(), Seg{Field: "z"}, "", `field "z" on boolean`},
		{"field on null", NullShape(), Seg{Field: "z"}, "", `field "z" on null`},
		{"index on array", arr, Seg{IsIndex: true, Index: 0}, "string", ""},
		{"index on unknown array", UnknownArr(), Seg{IsIndex: true}, "any", ""},
		{"index on object", closed, Seg{IsIndex: true, Index: 1}, "", "integer index [1] on object"},
		{"index on primitive", StringShape(), Seg{IsIndex: true}, "", "integer index [0] on string"},
		{"wildcard on array", arr, Seg{Wildcard: true}, "array<string>", ""},
		{"wildcard on object", obj(map[string]*Shape{"x": NumberShape(), "y": NumberShape()}), Seg{Wildcard: true}, "array<number>", ""},
		{"wildcard on mixed object", obj(map[string]*Shape{"x": NumberShape(), "y": StringShape()}), Seg{Wildcard: true}, "array<any>", ""},
		{"wildcard on string", StringShape(), Seg{Wildcard: true}, "any", ""},
		{"anything on any", AnyShape(), Seg{Field: "z"}, "any", ""},
		{"nil shape", nil, Seg{Field: "z"}, "any", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Access(tc.s, tc.seg)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Msg, tc.wantErr) {
					t.Fatalf("err = %v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestUnionAndIntersect(t *testing.T) {
	t.Parallel()
	a := obj(map[string]*Shape{"x": NumberShape(), "y": StringShape()})
	b := obj(map[string]*Shape{"x": NumberShape(), "z": BoolShape()})
	u := Union(a, b)
	if u.String() != "object{x, y, z, …}" || !u.Open {
		t.Fatalf("union = %s", u)
	}
	i := Intersect(a, b)
	if i.String() != "object{x, …}" || !i.Open {
		t.Fatalf("intersect = %s", i)
	}
	if Intersect(a, a).Open || Intersect(a, a).String() != "object{x, y}" {
		t.Fatalf("intersect same = %s", Intersect(a, a))
	}
	if Union(StringShape(), NumberShape()).Kind != Any || Intersect(StringShape(), NumberShape()).Kind != Any {
		t.Fatal("different kinds degrade to any")
	}
	if Union(NullShape(), StringShape()).Kind != String {
		t.Fatal("null unions with a primitive to the primitive")
	}
	if Union(ArrayOf(NumberShape()), ArrayOf(StringShape())).String() != "array<any>" {
		t.Fatal("array union")
	}
	if Union(nil, a) != a || Intersect(a, nil) != a {
		t.Fatal("nil identity")
	}
}

func TestRoundTripSource(t *testing.T) {
	t.Parallel()
	s := WithSource(NumberShape(), "input 'n'")
	if s.Source != "input 'n'" || NumberShape().Source != "" {
		t.Fatal("WithSource must copy")
	}
}
