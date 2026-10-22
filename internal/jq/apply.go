package jq

import (
	"strings"

	"github.com/najemi-software/jobsworth/internal/shape"
)

// Options mirror the jq flags that change shapes (medium §6.2.10).
type Options struct {
	// Raw (-r): string results stay strings (they would be output without
	// quotes); no shape effect beyond keeping String.
	Raw bool
	// NullInput (-n): the input is null.
	NullInput bool
	// Slurp (--slurp): the input is wrapped in an array.
	Slurp bool
	// Vars holds --arg (String) and --argjson (parsed) bindings.
	Vars map[string]*shape.Shape
}

// MaxDepth caps recursion through user definitions.
const MaxDepth = 8

// Apply computes the shape of a filter's outputs for an input shape.
// Multiple outputs union; unknown constructs are Any.
func Apply(filter Node, input *shape.Shape, opts Options) *shape.Shape {
	if opts.NullInput {
		input = shape.NullShape()
	} else if opts.Slurp {
		input = shape.ArrayOf(input)
	}
	a := &applier{vars: map[string]*shape.Shape{}, defs: map[string]*Def{}}
	for k, v := range opts.Vars {
		a.vars[k] = v
	}
	a.vars["ENV"] = shape.UnknownObj()
	a.vars["__loc__"] = shape.UnknownObj()
	return a.eval(filter, input, 0)
}

// ApplyString parses and applies a filter source.
func ApplyString(src string, input *shape.Shape, opts Options) (*shape.Shape, error) {
	n, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return Apply(n, input, opts), nil
}

type applier struct {
	vars map[string]*shape.Shape
	defs map[string]*Def
}

func anyS() *shape.Shape  { return shape.AnyShape() }
func strS() *shape.Shape  { return shape.StringShape() }
func numS() *shape.Shape  { return shape.NumberShape() }
func boolS() *shape.Shape { return shape.BoolShape() }

func (a *applier) withVar(name string, s *shape.Shape, fn func() *shape.Shape) *shape.Shape {
	prev, had := a.vars[name]
	a.vars[name] = s
	out := fn()
	if had {
		a.vars[name] = prev
	} else {
		delete(a.vars, name)
	}
	return out
}

// elem is the element shape of an array-ish input, or Any.
func elem(s *shape.Shape) *shape.Shape {
	if s == nil {
		return anyS()
	}
	switch s.Kind {
	case shape.Array:
		return s.Elem
	case shape.Object:
		var u *shape.Shape
		for _, f := range s.Fields {
			u = shape.Union(u, f)
		}
		if s.Open || u == nil {
			return anyS()
		}
		return u
	}
	return anyS()
}

func isContainer(s *shape.Shape) bool {
	return s != nil && (s.Kind == shape.Array || s.Kind == shape.UnknownArray || s.Kind == shape.Object || s.Kind == shape.UnknownObject)
}

func (a *applier) eval(n Node, in *shape.Shape, depth int) *shape.Shape {
	if depth > MaxDepth*4 {
		return anyS()
	}
	switch x := n.(type) {
	case Identity:
		return in
	case RecurseAll:
		return anyS()
	case Field:
		return field(in, x.Name)
	case Index:
		if x.Expr == nil {
			return elem(in)
		}
		idx := a.eval(x.Expr, in, depth+1)
		switch {
		case in == nil || in.Kind == shape.Any:
			return anyS()
		case idx != nil && idx.Kind == shape.String:
			if in.Kind == shape.Object || in.Kind == shape.UnknownObject {
				return anyS()
			}
			return anyS()
		case in.Kind == shape.Array:
			return in.Elem
		case in.Kind == shape.UnknownArray:
			return anyS()
		}
		return anyS()
	case Slice:
		if in != nil && (in.Kind == shape.Array || in.Kind == shape.String) {
			return in
		}
		return anyS()
	case Pipe:
		return a.eval(x.Right, a.eval(x.Left, in, depth+1), depth+1)
	case Comma:
		return shape.Union(a.eval(x.Left, in, depth+1), a.eval(x.Right, in, depth+1))
	case Alt:
		return shape.Union(a.eval(x.Left, in, depth+1), a.eval(x.Right, in, depth+1))
	case Binary:
		l := a.eval(x.Left, in, depth+1)
		r := a.eval(x.Right, in, depth+1)
		switch x.Op {
		case "==", "!=", "<", "<=", ">", ">=", "and", "or":
			return boolS()
		case "+":
			if l != nil && r != nil && l.Kind == r.Kind {
				switch l.Kind {
				case shape.String, shape.Number:
					return &shape.Shape{Kind: l.Kind}
				case shape.Array:
					return shape.ArrayOf(shape.Union(l.Elem, r.Elem))
				case shape.Object:
					return shape.Union(l, r)
				}
			}
			if l != nil && l.Kind == shape.Null {
				return r
			}
			if r != nil && r.Kind == shape.Null {
				return l
			}
			return anyS()
		case "-", "*", "/", "%":
			if l != nil && r != nil && l.Kind == shape.Number && r.Kind == shape.Number {
				return numS()
			}
			if x.Op == "*" && l != nil && r != nil && ((l.Kind == shape.String && r.Kind == shape.Number) || (l.Kind == shape.Number && r.Kind == shape.String)) {
				return strS()
			}
			if x.Op == "/" && l != nil && r != nil && l.Kind == shape.String && r.Kind == shape.String {
				return shape.ArrayOf(strS())
			}
			return anyS()
		}
		return anyS()
	case Neg:
		return numS()
	case Number:
		return numS()
	case Str:
		for _, p := range x.Interp {
			if p != nil {
				a.eval(p, in, depth+1)
			}
		}
		return strS()
	case Var:
		if s, ok := a.vars[x.Name]; ok {
			return s
		}
		return anyS()
	case Format:
		return strS()
	case ArrayCon:
		if x.Body == nil {
			return shape.ArrayOf(anyS())
		}
		return shape.ArrayOf(a.eval(x.Body, in, depth+1))
	case ObjectCon:
		fields := map[string]*shape.Shape{}
		open := false
		for _, e := range x.Entries {
			var v *shape.Shape
			switch {
			case e.Value != nil:
				v = a.eval(e.Value, in, depth+1)
			case e.KeyVar != "":
				v = a.vars[e.KeyVar]
				if v == nil {
					v = anyS()
				}
			default:
				v = field(in, e.Key)
			}
			switch {
			case e.KeyExpr != nil || e.Computed:
				open = true
			case e.KeyVar != "":
				fields[e.KeyVar] = v
			default:
				fields[e.Key] = v
			}
		}
		out := shape.ObjectShape(fields)
		out.Open = open
		return out
	case Call:
		return a.call(x, in, depth)
	case If:
		a.eval(x.Cond, in, depth+1)
		out := a.eval(x.Then, in, depth+1)
		for i := 0; i+1 < len(x.Elifs); i += 2 {
			a.eval(x.Elifs[i], in, depth+1)
			out = shape.Union(out, a.eval(x.Elifs[i+1], in, depth+1))
		}
		if x.Else != nil {
			out = shape.Union(out, a.eval(x.Else, in, depth+1))
		} else {
			out = shape.Union(out, in)
		}
		return out
	case Try:
		out := a.eval(x.Body, in, depth+1)
		if x.Catch != nil {
			out = shape.Union(out, a.eval(x.Catch, anyS(), depth+1))
		}
		return out
	case As:
		src := a.eval(x.Source, in, depth+1)
		return a.withVar(x.Var, src, func() *shape.Shape { return a.eval(x.Body, in, depth+1) })
	case Reduce, Foreach:
		return anyS()
	case Def:
		d := x
		prev, had := a.defs[d.Name]
		a.defs[d.Name] = &d
		out := a.eval(d.Rest, in, depth+1)
		if had {
			a.defs[d.Name] = prev
		} else {
			delete(a.defs, d.Name)
		}
		return out
	case Optional:
		return a.eval(x.X, in, depth+1)
	case Assign:
		return in
	}
	return anyS()
}

// field is jq's .name: null for a missing key, Any for open/unknown objects.
func field(in *shape.Shape, name string) *shape.Shape {
	if in == nil {
		return anyS()
	}
	switch in.Kind {
	case shape.Object:
		if f, ok := in.Fields[name]; ok {
			return f
		}
		if in.Open {
			return anyS()
		}
		return shape.NullShape()
	case shape.UnknownObject, shape.Any:
		return anyS()
	case shape.Null:
		return shape.NullShape()
	}
	return anyS()
}

// call applies a built-in or a user definition.
func (a *applier) call(c Call, in *shape.Shape, depth int) *shape.Shape {
	if d, ok := a.defs[c.Name]; ok && len(d.Params) == len(c.Args) {
		if depth/4 >= MaxDepth {
			return anyS()
		}
		// Parameters are bound as variables to the argument shapes; a filter
		// parameter used as a filter degrades to Any.
		saved := map[string]*shape.Shape{}
		for i, p := range d.Params {
			name := strings.TrimPrefix(p, "$")
			if prev, had := a.vars[name]; had {
				saved[name] = prev
			}
			a.vars[name] = a.eval(c.Args[i], in, depth+1)
		}
		out := a.eval(d.Body, in, depth+4)
		for _, p := range d.Params {
			name := strings.TrimPrefix(p, "$")
			if prev, had := saved[name]; had {
				a.vars[name] = prev
			} else {
				delete(a.vars, name)
			}
		}
		return out
	}
	arg := func(i int) *shape.Shape {
		if i < len(c.Args) {
			return a.eval(c.Args[i], in, depth+1)
		}
		return anyS()
	}
	switch c.Name {
	case "empty", "error", "halt", "halt_error", "input", "inputs", "debug", "stderr", "input_filename", "env", "builtins", "now", "getpath", "setpath", "delpaths", "walk", "paths", "leaf_paths", "splits", "ascii", "implode", "explode", "tojson_", "transpose", "combinations", "limit", "until", "while", "repeat", "recurse", "recurse_down", "range", "infinite", "nan", "ltrimstr", "rtrimstr":
		switch c.Name {
		case "env":
			return shape.UnknownObj()
		case "range":
			return numS()
		case "paths", "leaf_paths":
			return shape.ArrayOf(shape.ArrayOf(anyS()))
		case "ltrimstr", "rtrimstr":
			return strS()
		case "limit":
			return arg(1)
		case "recurse", "recurse_down", "walk", "until", "while", "repeat":
			return anyS()
		case "splits":
			return strS()
		case "explode":
			return shape.ArrayOf(numS())
		case "implode", "input_filename":
			return strS()
		case "transpose", "combinations":
			return shape.ArrayOf(anyS())
		case "infinite", "nan", "now":
			return numS()
		}
		return anyS()
	case "keys", "keys_unsorted":
		return shape.ArrayOf(strS())
	case "length", "utf8bytelength", "tonumber", "floor", "ceil", "round", "sqrt", "fabs", "abs", "index", "rindex", "indices", "min_by", "max_by", "tostring", "ascii_downcase", "ascii_upcase", "ltrimstr_", "@json":
		switch c.Name {
		case "tostring", "ascii_downcase", "ascii_upcase":
			return strS()
		case "indices":
			return shape.ArrayOf(numS())
		case "min_by", "max_by":
			return elem(in)
		}
		return numS()
	case "type":
		return strS()
	case "select":
		arg(0)
		return in
	case "map":
		if in != nil && (in.Kind == shape.Object || in.Kind == shape.UnknownObject) {
			return shape.ArrayOf(a.eval(c.Args[0], elem(in), depth+1))
		}
		if len(c.Args) == 1 {
			return shape.ArrayOf(a.eval(c.Args[0], elem(in), depth+1))
		}
		return anyS()
	case "map_values":
		if len(c.Args) == 1 && in != nil && in.Kind == shape.Object {
			fields := map[string]*shape.Shape{}
			for k, f := range in.Fields {
				fields[k] = a.eval(c.Args[0], f, depth+1)
			}
			out := shape.ObjectShape(fields)
			out.Open = in.Open
			return out
		}
		if len(c.Args) == 1 && in != nil && in.Kind == shape.Array {
			return shape.ArrayOf(a.eval(c.Args[0], in.Elem, depth+1))
		}
		return anyS()
	case "to_entries":
		return shape.ArrayOf(shape.ObjectShape(map[string]*shape.Shape{"key": strS(), "value": elem(in)}))
	case "from_entries", "with_entries":
		if c.Name == "with_entries" && len(c.Args) == 1 {
			a.eval(c.Args[0], shape.ObjectShape(map[string]*shape.Shape{"key": strS(), "value": elem(in)}), depth+1)
		}
		return shape.UnknownObj()
	case "has", "in", "inside", "contains", "startswith", "endswith", "test", "any", "all", "isvalid", "isnan", "isinfinite", "isnormal", "not":
		for i := range c.Args {
			arg(i)
		}
		return boolS()
	case "add":
		if in != nil && in.Kind == shape.Array {
			return in.Elem
		}
		return anyS()
	case "unique", "sort", "reverse", "flatten", "unique_by", "sort_by", "del", "to_array", "arrays", "objects", "iterables", "booleans", "numbers", "strings", "nulls", "values", "scalars":
		for i := range c.Args {
			arg(i)
		}
		switch c.Name {
		case "flatten":
			return shape.ArrayOf(anyS())
		case "del":
			return in
		case "arrays", "objects", "iterables", "booleans", "numbers", "strings", "nulls", "values", "scalars":
			return in
		}
		if in != nil && in.Kind == shape.String && c.Name == "reverse" {
			return strS()
		}
		return in
	case "group_by":
		arg(0)
		return shape.ArrayOf(in)
	case "first", "last", "nth", "min", "max":
		if len(c.Args) >= 1 && (c.Name == "first" || c.Name == "last" || c.Name == "nth") {
			return arg(len(c.Args) - 1)
		}
		return elem(in)
	case "split", "match", "capture", "scan", "sub", "gsub", "ascii_", "join", "tojson", "fromjson", "todate", "fromdate", "strftime", "strptime", "dateadd", "datesub", "date", "gmtime", "localtime", "mktime", "@base64", "@base64d", "@uri", "@csv", "@tsv", "@html", "@sh", "@text", "tostream", "fromstream", "truncate_stream", "getpath_", "splits_", "ascii_downcase_", "test_":
		for i := range c.Args {
			arg(i)
		}
		switch c.Name {
		case "split":
			return shape.ArrayOf(strS())
		case "match":
			return shape.UnknownObj()
		case "capture":
			return shape.UnknownObj()
		case "scan":
			return shape.ArrayOf(anyS())
		case "sub", "gsub", "join", "tojson", "todate", "strftime", "date":
			return strS()
		case "fromjson":
			return anyS()
		case "fromdate", "mktime":
			return numS()
		case "gmtime", "localtime", "strptime":
			return shape.ArrayOf(numS())
		}
		return anyS()
	}
	for i := range c.Args {
		arg(i)
	}
	return anyS()
}
