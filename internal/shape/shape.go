// Package shape is the static value model of medium §6.1: what a JSON-ish
// value looks like (object with known fields, array of a shape, primitive,
// unknown container, or anything), the access rules over it, and shape
// construction from JSON and YAML literals.
package shape

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Kind classifies a shape.
type Kind int

const (
	Any Kind = iota
	String
	Number
	Bool
	Null
	Object
	Array
	UnknownObject
	UnknownArray
)

func (k Kind) String() string {
	return [...]string{"any", "string", "number", "boolean", "null", "object", "array", "object", "array"}[k]
}

// Shape is one static value description. Fields/Open apply to Object; Elem
// to Array. Source says where the shape came from, for messages.
type Shape struct {
	Kind   Kind
	Fields map[string]*Shape
	Open   bool
	Elem   *Shape
	Source string
	// JSON, on a String shape, is the shape of the string's content when it
	// is known to be JSON (an input default, a bash write, an env literal),
	// so fromJSON(x) can unwrap it (medium §6.3).
	JSON *Shape
	// Values is a known value set on a String (job.status, runner.os, ...).
	Values []string
	// Prov is the provenance chain (spec-shape §4.2), recorded when the
	// engine is asked to explain.
	Prov []Prov
}

// StringOfJSON is a string whose content is the JSON value inner describes.
func StringOfJSON(inner *Shape) *Shape {
	return &Shape{Kind: String, JSON: inner}
}

// Constructors.
func AnyShape() *Shape    { return &Shape{Kind: Any} }
func StringShape() *Shape { return &Shape{Kind: String} }
func NumberShape() *Shape { return &Shape{Kind: Number} }
func BoolShape() *Shape   { return &Shape{Kind: Bool} }
func NullShape() *Shape   { return &Shape{Kind: Null} }
func UnknownObj() *Shape  { return &Shape{Kind: UnknownObject} }
func UnknownArr() *Shape  { return &Shape{Kind: UnknownArray} }

// ObjectShape builds a closed object.
func ObjectShape(fields map[string]*Shape) *Shape {
	if fields == nil {
		fields = map[string]*Shape{}
	}
	return &Shape{Kind: Object, Fields: fields}
}

// OpenObject builds an object that may hold unlisted fields.
func OpenObject(fields map[string]*Shape) *Shape {
	s := ObjectShape(fields)
	s.Open = true
	return s
}

// ArrayOf builds an array shape.
func ArrayOf(elem *Shape) *Shape {
	if elem == nil {
		elem = AnyShape()
	}
	return &Shape{Kind: Array, Elem: elem}
}

// IsAny reports whether nothing is known.
func IsAny(s *Shape) bool { return s == nil || s.Kind == Any }

// IsPrimitive reports a string/number/bool/null kind.
func IsPrimitive(s *Shape) bool {
	return s != nil && (s.Kind == String || s.Kind == Number || s.Kind == Bool || s.Kind == Null)
}

// FieldNames lists an object's known fields, sorted.
func FieldNames(s *Shape) []string {
	var out []string
	for k := range s.Fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// String renders a shape compactly: object{a, b}, array<string>, string.
func (s *Shape) String() string {
	if s == nil {
		return "any"
	}
	switch s.Kind {
	case Object:
		names := FieldNames(s)
		if s.Open {
			names = append(names, "…")
		}
		return "object{" + strings.Join(names, ", ") + "}"
	case Array:
		return "array<" + s.Elem.String() + ">"
	}
	return s.Kind.String()
}

// WithSource returns a copy carrying provenance.
func WithSource(s *Shape, source string) *Shape {
	if s == nil {
		return nil
	}
	c := *s
	c.Source = source
	return &c
}

// --- construction from literals ------------------------------------------------

// FromJSON parses a JSON literal into an exact, closed shape.
func FromJSON(data []byte) (*Shape, error) {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	return fromValue(v), nil
}

// FromYAML parses a YAML document into a shape (mappings closed).
func FromYAML(data []byte) (*Shape, error) {
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return fromValue(normaliseYAML(v)), nil
}

// FromLiteral is the shape of a YAML/bash scalar literal as a runtime
// value: always a string, carrying the parsed JSON shape when the literal
// is a JSON object or array.
func FromLiteral(s string) *Shape {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		if sh, err := FromJSON([]byte(t)); err == nil {
			return StringOfJSON(sh)
		}
	}
	return StringShape()
}

// IsJSONLiteral reports whether s parses as a JSON object or array.
func IsJSONLiteral(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[") {
		return false
	}
	_, err := FromJSON([]byte(t))
	return err == nil
}

func fromValue(v any) *Shape {
	switch x := v.(type) {
	case nil:
		return NullShape()
	case bool:
		return BoolShape()
	case json.Number, float64, int, int64:
		return NumberShape()
	case string:
		return StringShape()
	case []any:
		var elem *Shape
		for _, e := range x {
			elem = Union(elem, fromValue(e))
		}
		if elem == nil {
			elem = AnyShape()
		}
		return ArrayOf(elem)
	case map[string]any:
		fields := map[string]*Shape{}
		for k, e := range x {
			fields[k] = fromValue(e)
		}
		return ObjectShape(fields)
	}
	return AnyShape()
}

func normaliseYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normaliseYAML(e)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, e := range x {
			out[fmt.Sprint(k)] = normaliseYAML(e)
		}
		return out
	case []any:
		for i := range x {
			x[i] = normaliseYAML(x[i])
		}
		return x
	}
	return v
}

// --- combination --------------------------------------------------------------------

// Union is the shape of "either a or b": same kind merges structurally
// (objects become open where fields differ, arrays union their elements);
// different kinds degrade to Any. nil is the identity.
func Union(a, b *Shape) *Shape {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.Kind != b.Kind {
		if a.Kind == Null && IsPrimitive(b) {
			return b
		}
		if b.Kind == Null && IsPrimitive(a) {
			return a
		}
		return AnyShape()
	}
	switch a.Kind {
	case Object:
		fields := map[string]*Shape{}
		open := a.Open || b.Open
		for k, s := range a.Fields {
			if t, ok := b.Fields[k]; ok {
				fields[k] = Union(s, t)
			} else {
				fields[k] = s
				open = true
			}
		}
		for k, s := range b.Fields {
			if _, ok := a.Fields[k]; !ok {
				fields[k] = s
				open = true
			}
		}
		out := ObjectShape(fields)
		out.Open = open
		return out
	case Array:
		return ArrayOf(Union(a.Elem, b.Elem))
	}
	return &Shape{Kind: a.Kind}
}

// Intersect is the shape of "both a and b are possible" for caller-supplied
// values (medium §6.2.4): a field present in every caller is known; the
// object is open when callers disagree. One caller is exact.
func Intersect(a, b *Shape) *Shape {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.Kind != b.Kind {
		return AnyShape()
	}
	switch a.Kind {
	case Object:
		fields := map[string]*Shape{}
		open := a.Open || b.Open
		for k, s := range a.Fields {
			if t, ok := b.Fields[k]; ok {
				fields[k] = Intersect(s, t)
			} else {
				open = true
			}
		}
		for k := range b.Fields {
			if _, ok := a.Fields[k]; !ok {
				open = true
			}
		}
		out := ObjectShape(fields)
		out.Open = open
		return out
	case Array:
		return ArrayOf(Intersect(a.Elem, b.Elem))
	}
	return &Shape{Kind: a.Kind}
}

// --- access -----------------------------------------------------------------------

// Seg is one access step: a field name, an integer index, or a wildcard.
type Seg struct {
	Field    string
	Index    int
	IsIndex  bool
	Wildcard bool
}

// AccessError describes a chain step that cannot succeed.
type AccessError struct {
	Msg string
}

func (e *AccessError) Error() string { return e.Msg }

// Access applies one step to a shape per the medium §6.1 table. Unknown
// shapes yield Any and never an error.
func Access(s *Shape, seg Seg) (*Shape, *AccessError) {
	if IsAny(s) {
		return AnyShape(), nil
	}
	switch {
	case seg.Wildcard:
		switch s.Kind {
		case Array:
			return ArrayOf(s.Elem), nil
		case UnknownArray:
			return ArrayOf(AnyShape()), nil
		case Object:
			var elem *Shape
			for _, f := range s.Fields {
				elem = Union(elem, f)
			}
			if s.Open || elem == nil {
				elem = AnyShape()
			}
			return ArrayOf(elem), nil
		case UnknownObject:
			return ArrayOf(AnyShape()), nil
		}
		return AnyShape(), nil
	case seg.IsIndex:
		switch s.Kind {
		case Array:
			return s.Elem, nil
		case UnknownArray:
			return AnyShape(), nil
		case Object, UnknownObject:
			return nil, &AccessError{Msg: fmt.Sprintf("integer index [%d] on object", seg.Index)}
		}
		return nil, &AccessError{Msg: fmt.Sprintf("integer index [%d] on %s", seg.Index, s.Kind)}
	default:
		switch s.Kind {
		case Object:
			if f, ok := s.Fields[seg.Field]; ok {
				return f, nil
			}
			if s.Open {
				return AnyShape(), nil
			}
			return nil, &AccessError{Msg: fmt.Sprintf("field %q not in object (declared: %s)", seg.Field, listOrNone(FieldNames(s)))}
		case UnknownObject:
			return AnyShape(), nil
		case Array, UnknownArray:
			return nil, &AccessError{Msg: fmt.Sprintf("field %q on array", seg.Field)}
		}
		return nil, &AccessError{Msg: fmt.Sprintf("field %q on %s", seg.Field, s.Kind)}
	}
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
