package shape

import (
	"fmt"
	"sort"
	"strings"
)

// Prov is one step of a shape's provenance (spec-shape §4.2): where a
// value came from or what transformed it. Depth is the nesting level
// inside a source (a jq filter inside a bash variable inside a step
// write); Transform marks an expression-level step (fromJSON, a member
// access) printed after the source chain.
type Prov struct {
	// Kind: declared_input, input_default, caller_with, intersection,
	// static_schema, matrix, env_block, github_env_write,
	// github_output_write, bash_var, file_read, jq, builtin, lift, opaque,
	// fromjson, access, no_callers.
	Kind string `json:"kind"`
	// Text is the human line's payload: a statement, an assignment, a
	// declaration, a reason.
	Text string `json:"text,omitempty"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Step string `json:"step,omitempty"`
	Name string `json:"name,omitempty"`
	// Path and Format for file reads.
	Path   string `json:"path,omitempty"`
	Format string `json:"format,omitempty"`
	// Filter, Flags, Input and Output for jq.
	Filter string   `json:"filter,omitempty"`
	Flags  []string `json:"flags,omitempty"`
	Input  *Shape   `json:"input,omitempty"`
	Output *Shape   `json:"output,omitempty"`
	// Shape is the shape at this point, when it differs from the result.
	Shape *Shape `json:"shape,omitempty"`
	// Depth and Transform drive the text layout.
	Depth     int  `json:"-"`
	Transform bool `json:"-"`
}

// Wrap returns a copy of s whose provenance starts with p, the entries s
// already had nested one level deeper (a container: a step write around
// the variable it echoes).
func Wrap(s *Shape, p Prov) *Shape {
	if s == nil {
		s = AnyShape()
	}
	c := *s
	c.Prov = make([]Prov, 0, len(s.Prov)+1)
	c.Prov = append(c.Prov, p)
	for _, q := range s.Prov {
		if !q.Transform {
			q.Depth++
		}
		c.Prov = append(c.Prov, q)
	}
	return &c
}

// Then returns a copy of s with p appended: a transformation applied to
// the value (jq over its input, fromJSON, a member access).
func Then(s *Shape, p Prov) *Shape {
	if s == nil {
		s = AnyShape()
	}
	c := *s
	c.Prov = append(append([]Prov{}, s.Prov...), p)
	return &c
}

// Opaque is Any with the reason inference gave up.
func Opaque(reason string) *Shape {
	return &Shape{Kind: Any, Prov: []Prov{{Kind: "opaque", Text: reason + " → Any"}}}
}

// WithValues returns a copy of a string shape with a known value set.
func WithValues(s *Shape, values ...string) *Shape {
	c := *s
	c.Values = values
	return &c
}

// --- rendering (spec-shape §4.1) ----------------------------------------------

// MaxFields is how many object fields Render lists before "… (n more)".
const MaxFields = 12

// Render prints a shape in the compact notation; depth bounds nesting.
func Render(s *Shape, depth int) string {
	return render(s, depth, 0)
}

func render(s *Shape, depth, level int) string {
	if s == nil {
		return "Any"
	}
	switch s.Kind {
	case Any:
		return "Any"
	case String:
		out := "String"
		if len(s.Values) > 0 {
			out += " ∈ {" + strings.Join(s.Values, ", ") + "}"
		}
		if s.JSON != nil {
			out += " ⊂ JSON → " + render(s.JSON, depth, level)
		}
		return out
	case Number:
		return "Number"
	case Bool:
		return "Bool"
	case Null:
		return "Null"
	case UnknownObject:
		return "Object{...}"
	case UnknownArray:
		return "Array[...]"
	case Array:
		if level >= depth {
			return "Array[…]"
		}
		return "Array[" + render(s.Elem, depth, level+1) + "]"
	case Object:
		if level >= depth {
			return "Object{…}"
		}
		names := FieldNames(s)
		var parts []string
		for i, n := range names {
			if i == MaxFields {
				parts = append(parts, fmt.Sprintf("… (%d more)", len(names)-MaxFields))
				break
			}
			parts = append(parts, n+": "+render(s.Fields[n], depth, level+1))
		}
		if s.Open {
			parts = append(parts, "+")
		}
		return "Object{" + strings.Join(parts, ", ") + "}"
	}
	return "Any"
}

// ToJSON encodes a shape per spec-shape §4.3.
func ToJSON(s *Shape) map[string]any {
	if s == nil {
		return map[string]any{"kind": "any"}
	}
	switch s.Kind {
	case String:
		m := map[string]any{"kind": "string"}
		if len(s.Values) > 0 {
			m["values"] = s.Values
		}
		if s.JSON != nil {
			m["json"] = ToJSON(s.JSON)
		}
		return m
	case Number:
		return map[string]any{"kind": "number"}
	case Bool:
		return map[string]any{"kind": "bool"}
	case Null:
		return map[string]any{"kind": "null"}
	case UnknownObject:
		return map[string]any{"kind": "object", "open": true}
	case UnknownArray:
		return map[string]any{"kind": "array"}
	case Object:
		fields := map[string]any{}
		for k, v := range s.Fields {
			fields[k] = ToJSON(v)
		}
		return map[string]any{"kind": "object", "open": s.Open, "fields": fields}
	case Array:
		return map[string]any{"kind": "array", "elem": ToJSON(s.Elem)}
	}
	return map[string]any{"kind": "any"}
}

// Accept is one accepted access on a shape and what it yields.
type Accept struct {
	Access string `json:"access"`
	Shape  *Shape `json:"-"`
}

// Accepted lists the immediate accesses that would pass on s (§4.2
// "accepted"). Primitives and Any yield nothing.
func Accepted(s *Shape) []Accept {
	if s == nil {
		return nil
	}
	var out []Accept
	switch s.Kind {
	case Object:
		for _, n := range FieldNames(s) {
			out = append(out, Accept{"." + n, s.Fields[n]})
		}
		if s.Open {
			out = append(out, Accept{".<other>", AnyShape()})
		}
		out = append(out, Accept{".*", wildcardOf(s)})
	case UnknownObject:
		out = append(out, Accept{".<any>", AnyShape()}, Accept{".*", ArrayOf(AnyShape())})
	case Array:
		out = append(out, Accept{"[n]", s.Elem})
		if s.Elem != nil && s.Elem.Kind == Object {
			for _, n := range FieldNames(s.Elem) {
				out = append(out, Accept{"[n]." + n, s.Elem.Fields[n]})
			}
		}
		out = append(out, Accept{".*", ArrayOf(s.Elem)})
	case UnknownArray:
		out = append(out, Accept{"[n]", AnyShape()}, Accept{".*", ArrayOf(AnyShape())})
	}
	return out
}

func wildcardOf(s *Shape) *Shape {
	w, _ := Access(s, Seg{Wildcard: true})
	return w
}

// SortedValues returns a copy of the value set, sorted.
func SortedValues(s *Shape) []string {
	out := append([]string{}, s.Values...)
	sort.Strings(out)
	return out
}
