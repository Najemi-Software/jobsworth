package shape

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	obj := ObjectShape(map[string]*Shape{"name": StringShape(), "tags": ArrayOf(StringShape())})
	open := OpenObject(map[string]*Shape{"name": StringShape()})
	for _, tc := range []struct {
		s    *Shape
		want string
	}{
		{nil, "Any"},
		{AnyShape(), "Any"},
		{StringShape(), "String"},
		{NumberShape(), "Number"},
		{BoolShape(), "Bool"},
		{NullShape(), "Null"},
		{UnknownObj(), "Object{...}"},
		{UnknownArr(), "Array[...]"},
		{obj, "Object{name: String, tags: Array[String]}"},
		{open, "Object{name: String, +}"},
		{ArrayOf(ObjectShape(map[string]*Shape{"id": NumberShape(), "sha": StringShape()})), "Array[Object{id: Number, sha: String}]"},
		{WithValues(StringShape(), "success", "failure"), "String ∈ {success, failure}"},
		{StringOfJSON(obj), "String ⊂ JSON → Object{name: String, tags: Array[String]}"},
	} {
		if got := Render(tc.s, 4); got != tc.want {
			t.Errorf("Render = %q, want %q", got, tc.want)
		}
	}
}

func TestRenderDepthAndFieldCut(t *testing.T) {
	deep := ArrayOf(ObjectShape(map[string]*Shape{"a": ObjectShape(map[string]*Shape{"b": StringShape()})}))
	if got := Render(deep, 2); got != "Array[Object{a: Object{…}}]" {
		t.Errorf("depth 2: %s", got)
	}
	if got := Render(deep, 1); got != "Array[Object{…}]" {
		t.Errorf("depth 1: %s", got)
	}
	fields := map[string]*Shape{}
	for i := 0; i < 15; i++ {
		fields[string(rune('a'+i))] = StringShape()
	}
	got := Render(ObjectShape(fields), 4)
	if !strings.HasSuffix(got, "l: String, … (3 more)}") || strings.Count(got, "String") != 12 {
		t.Errorf("field cut: %s", got)
	}
}

func TestToJSONAndAccepted(t *testing.T) {
	s := StringOfJSON(ArrayOf(OpenObject(map[string]*Shape{"id": NumberShape()})))
	m := ToJSON(s)
	if m["kind"] != "string" || m["json"].(map[string]any)["kind"] != "array" {
		t.Errorf("ToJSON: %v", m)
	}
	elem := m["json"].(map[string]any)["elem"].(map[string]any)
	if elem["open"] != true || elem["fields"].(map[string]any)["id"].(map[string]any)["kind"] != "number" {
		t.Errorf("elem: %v", elem)
	}
	acc := Accepted(s.JSON)
	var names []string
	for _, a := range acc {
		names = append(names, a.Access)
	}
	if strings.Join(names, " ") != "[n] [n].id .*" {
		t.Errorf("accepted: %v", names)
	}
	if len(Accepted(StringShape())) != 0 || len(Accepted(AnyShape())) != 0 {
		t.Error("primitives accept nothing")
	}
}

func TestWrapThen(t *testing.T) {
	s := Opaque("x")
	s = Then(s, Prov{Kind: "jq", Transform: false})
	s = Wrap(s, Prov{Kind: "github_output_write"})
	kinds := []string{}
	for _, p := range s.Prov {
		kinds = append(kinds, p.Kind)
	}
	if strings.Join(kinds, " ") != "github_output_write opaque jq" || s.Prov[1].Depth != 1 || s.Prov[2].Depth != 1 || s.Prov[0].Depth != 0 {
		t.Errorf("prov: %+v", s.Prov)
	}
}
