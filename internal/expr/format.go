package expr

import (
	"strconv"
	"strings"
)

// canonicalNames maps lower-cased built-in function names to their
// documented casing.
var canonicalNames = map[string]string{
	"fromjson": "fromJSON", "tojson": "toJSON", "hashfiles": "hashFiles",
	"startswith": "startsWith", "endswith": "endsWith", "contains": "contains",
	"join": "join", "format": "format", "success": "success", "always": "always",
	"cancelled": "cancelled", "failure": "failure",
}

// CanonicalName returns the documented casing of a built-in function, or
// the name unchanged.
func CanonicalName(name string) string {
	if c, ok := canonicalNames[strings.ToLower(name)]; ok {
		return c
	}
	return name
}

// Format renders a parsed expression back to text with normalised
// spacing and canonical function casing (spec-shape §4.2).
func Format(n Node) string {
	var b strings.Builder
	format(&b, n, 0)
	return b.String()
}

func format(b *strings.Builder, n Node, parentPrec int) {
	switch n := n.(type) {
	case *Literal:
		switch n.Kind {
		case LitNull:
			b.WriteString("null")
		case LitBool:
			b.WriteString(strconv.FormatBool(n.Bool))
		case LitNumber:
			b.WriteString(strconv.FormatFloat(n.Num, 'f', -1, 64))
		case LitString:
			b.WriteString("'" + strings.ReplaceAll(n.Str, "'", "''") + "'")
		}
	case *Ident:
		b.WriteString(n.Name)
	case *Member:
		format(b, n.X, 0)
		b.WriteString("." + n.Name)
	case *Index:
		format(b, n.X, 0)
		b.WriteString("[")
		format(b, n.Idx, 0)
		b.WriteString("]")
	case *Wildcard:
		format(b, n.X, 0)
		b.WriteString(".*")
	case *Call:
		b.WriteString(CanonicalName(n.Name) + "(")
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			format(b, a, 0)
		}
		b.WriteString(")")
	case *Unary:
		b.WriteString(n.Op)
		if x, ok := n.X.(*Binary); ok {
			b.WriteString("(")
			format(b, x, 0)
			b.WriteString(")")
		} else {
			format(b, n.X, 0)
		}
	case *Binary:
		prec := precedence[n.Op]
		wrap := prec < parentPrec
		if wrap {
			b.WriteString("(")
		}
		format(b, n.X, prec)
		b.WriteString(" " + n.Op + " ")
		format(b, n.Y, prec+1) // left-associative: an equal-precedence right operand keeps its parens
		if wrap {
			b.WriteString(")")
		}
	}
}
