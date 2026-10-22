// Package jq parses jq filters and applies them at the shape level (medium
// §6.2.10): given the shape of the input, what shape do the outputs have.
// It never runs jq; unknown built-ins and unsupported constructs degrade
// to Any.
package jq

import (
	"fmt"
	"strconv"
	"strings"
)

// Error is a parse error with its byte offset in the filter.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("at %d: %s", e.Pos, e.Msg) }

// --- tokens --------------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tField // .name
	tNumber
	tString  // "..." (Interp holds interpolated segments)
	tVar     // $name
	tFormat  // @json
	tOp      // punctuation and operators
	tKeyword // if then elif else end as def reduce foreach try catch and or not
)

type token struct {
	kind tokKind
	text string
	pos  int
	// parts for interpolated strings: literal text, or filter source for
	// \( ... ) segments (marked by interp=true).
	parts  []string
	interp []bool
}

var keywords = map[string]bool{"if": true, "then": true, "elif": true, "else": true, "end": true, "as": true, "def": true, "reduce": true, "foreach": true, "try": true, "catch": true, "and": true, "or": true, "not": true, "import": true, "include": true, "label": true, "__loc__": true}

func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentChar(c byte) bool  { return isIdentStart(c) || (c >= '0' && c <= '9') }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }

func tokenize(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"':
			t, n, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, t)
			i = n
		case c == '.' && i+1 < len(src) && isIdentStart(src[i+1]):
			start := i
			i++
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			toks = append(toks, token{kind: tField, text: src[start+1 : i], pos: start})
		case c == '.' && i+1 < len(src) && src[i+1] == '"':
			t, n, err := lexString(src, i+1)
			if err != nil {
				return nil, err
			}
			t.kind = tField
			t.pos = i
			toks = append(toks, t)
			i = n
		case c == '$':
			start := i
			i++
			if i < len(src) && src[i] == '_' && strings.HasPrefix(src[i:], "__loc__") {
				i += len("__loc__")
			} else {
				for i < len(src) && isIdentChar(src[i]) {
					i++
				}
			}
			if i == start+1 {
				return nil, &Error{Pos: start, Msg: "expected variable name after $"}
			}
			toks = append(toks, token{kind: tVar, text: src[start+1 : i], pos: start})
		case c == '@':
			start := i
			i++
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			if i == start+1 {
				return nil, &Error{Pos: start + 1, Msg: "expected format name after @"}
			}
			toks = append(toks, token{kind: tFormat, text: src[start:i], pos: start})
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			start := i
			for i < len(src) && (isDigit(src[i]) || src[i] == '.' || src[i] == 'e' || src[i] == 'E' || ((src[i] == '-' || src[i] == '+') && i > start && (src[i-1] == 'e' || src[i-1] == 'E'))) {
				i++
			}
			if _, err := strconv.ParseFloat(src[start:i], 64); err != nil {
				return nil, &Error{Pos: start, Msg: "bad number " + src[start:i]}
			}
			toks = append(toks, token{kind: tNumber, text: src[start:i], pos: start})
		case isIdentStart(c):
			start := i
			for i < len(src) && (isIdentChar(src[i]) || (src[i] == ':' && i+1 < len(src) && src[i+1] == ':')) {
				if src[i] == ':' {
					i += 2
					continue
				}
				i++
			}
			text := src[start:i]
			kind := tIdent
			if keywords[text] {
				kind = tKeyword
			}
			toks = append(toks, token{kind: kind, text: text, pos: start})
		default:
			start := i
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch {
			case two == "//" || two == "==" || two == "!=" || two == "<=" || two == ">=" || two == "|=" || two == "+=" || two == "-=" || two == "*=" || two == "/=" || two == "%=" || two == "?/" || two == "..":
				if two == "//" && i+2 < len(src) && src[i+2] == '=' {
					toks = append(toks, token{kind: tOp, text: "//=", pos: start})
					i += 3
				} else {
					toks = append(toks, token{kind: tOp, text: two, pos: start})
					i += 2
				}
			case strings.ContainsRune(".|,()[]{}:;?+-*/%<>=", rune(c)):
				toks = append(toks, token{kind: tOp, text: string(c), pos: start})
				i++
			default:
				return nil, &Error{Pos: start, Msg: fmt.Sprintf("unexpected character %q", c)}
			}
		}
	}
	toks = append(toks, token{kind: tEOF, pos: len(src)})
	return toks, nil
}

// lexString reads a "..." string with escapes and \( ) interpolation.
func lexString(src string, start int) (token, int, error) {
	i := start + 1
	t := token{kind: tString, pos: start}
	var b strings.Builder
	flush := func() {
		t.parts = append(t.parts, b.String())
		t.interp = append(t.interp, false)
		b.Reset()
	}
	for {
		if i >= len(src) {
			return t, i, &Error{Pos: start, Msg: "unterminated string"}
		}
		c := src[i]
		switch c {
		case '"':
			flush()
			t.text = strings.Join(t.parts, "")
			return t, i + 1, nil
		case '\\':
			if i+1 >= len(src) {
				return t, i, &Error{Pos: i, Msg: "bad escape"}
			}
			i++
			switch src[i] {
			case '(':
				// interpolation: find the matching paren
				depth := 1
				j := i + 1
				for j < len(src) && depth > 0 {
					switch src[j] {
					case '(':
						depth++
					case ')':
						depth--
					case '"':
						// nested string: skip it
						_, n, err := lexString(src, j)
						if err != nil {
							return t, j, err
						}
						j = n
						continue
					}
					j++
				}
				if depth != 0 {
					return t, i, &Error{Pos: i, Msg: "unterminated interpolation"}
				}
				flush()
				t.parts = append(t.parts, src[i+1:j-1])
				t.interp = append(t.interp, true)
				i = j
				continue
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'u':
				b.WriteString("\\u")
			default:
				b.WriteByte(src[i])
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
}

// --- AST --------------------------------------------------------------------------

// Node is a filter AST node.
type Node interface{ node() }

type (
	Identity   struct{}
	RecurseAll struct{}
	Field      struct {
		Name string
		Opt  bool
	}
	Index struct {
		Expr Node // nil for .[]
		Opt  bool
	}
	Slice  struct{ From, To Node }
	Pipe   struct{ Left, Right Node }
	Comma  struct{ Left, Right Node }
	Alt    struct{ Left, Right Node }
	Binary struct {
		Op          string
		Left, Right Node
	}
	Neg    struct{ X Node }
	Number struct{ Value float64 }
	Str    struct {
		Parts  []string
		Interp []Node // nil entries for literal parts
	}
	Var       struct{ Name string }
	Format    struct{ Name string }
	ArrayCon  struct{ Body Node } // [ ... ], Body nil for []
	ObjectCon struct {
		Entries []ObjEntry
	}
	ObjEntry struct {
		Key      string // literal key when KeyExpr is nil
		KeyExpr  Node
		KeyVar   string // {$x}
		Value    Node   // nil means {key} shorthand
		Computed bool   // (expr): value
	}
	Call struct {
		Name string
		Args []Node
	}
	If struct {
		Cond, Then Node
		Elifs      []Node // pairs cond, then
		Else       Node   // may be nil
	}
	Try struct{ Body, Catch Node }
	As  struct {
		Source Node
		Var    string
		Body   Node
	}
	Reduce struct {
		Source       Node
		Var          string
		Init, Update Node
	}
	Foreach struct {
		Source                Node
		Var                   string
		Init, Update, Extract Node
	}
	Def struct {
		Name   string
		Params []string
		Body   Node
		Rest   Node
	}
	Optional struct{ X Node } // x?
	Assign   struct {
		Op          string
		Left, Right Node
	}
)

func (Identity) node()   {}
func (RecurseAll) node() {}
func (Field) node()      {}
func (Index) node()      {}
func (Slice) node()      {}
func (Pipe) node()       {}
func (Comma) node()      {}
func (Alt) node()        {}
func (Binary) node()     {}
func (Neg) node()        {}
func (Number) node()     {}
func (Str) node()        {}
func (Var) node()        {}
func (Format) node()     {}
func (ArrayCon) node()   {}
func (ObjectCon) node()  {}
func (Call) node()       {}
func (If) node()         {}
func (Try) node()        {}
func (As) node()         {}
func (Reduce) node()     {}
func (Foreach) node()    {}
func (Def) node()        {}
func (Optional) node()   {}
func (Assign) node()     {}

// --- parser ---------------------------------------------------------------------------

type parser struct {
	toks []token
	i    int
}

// Parse parses a jq filter.
func Parse(src string) (Node, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.pipe()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("unexpected %q", t.text)}
	}
	return n, nil
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}
func (p *parser) isOp(text string) bool {
	t := p.peek()
	return t.kind == tOp && t.text == text
}
func (p *parser) isKw(text string) bool {
	t := p.peek()
	return t.kind == tKeyword && t.text == text
}
func (p *parser) expectOp(text string) error {
	if !p.isOp(text) {
		return &Error{Pos: p.peek().pos, Msg: fmt.Sprintf("expected %q", text)}
	}
	p.next()
	return nil
}
func (p *parser) expectKw(text string) error {
	if !p.isKw(text) {
		return &Error{Pos: p.peek().pos, Msg: fmt.Sprintf("expected %q", text)}
	}
	p.next()
	return nil
}

// pipe := def* comma ('as' $v '|' pipe | '|' pipe)?
func (p *parser) pipe() (Node, error) {
	if p.isKw("def") {
		return p.def()
	}
	left, err := p.comma()
	if err != nil {
		return nil, err
	}
	if p.isKw("as") {
		p.next()
		v := p.next()
		if v.kind != tVar {
			return nil, &Error{Pos: v.pos, Msg: "expected $variable after 'as'"}
		}
		if err := p.expectOp("|"); err != nil {
			return nil, err
		}
		body, err := p.pipe()
		if err != nil {
			return nil, err
		}
		return As{Source: left, Var: v.text, Body: body}, nil
	}
	if p.isOp("|") {
		p.next()
		right, err := p.pipe()
		if err != nil {
			return nil, err
		}
		return Pipe{Left: left, Right: right}, nil
	}
	return left, nil
}

func (p *parser) def() (Node, error) {
	p.next() // def
	name := p.next()
	if name.kind != tIdent {
		return nil, &Error{Pos: name.pos, Msg: "expected function name after 'def'"}
	}
	var params []string
	if p.isOp("(") {
		p.next()
		for {
			t := p.next()
			if t.kind != tIdent && t.kind != tVar {
				return nil, &Error{Pos: t.pos, Msg: "expected parameter name"}
			}
			params = append(params, t.text)
			if p.isOp(";") {
				p.next()
				continue
			}
			break
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
	}
	if err := p.expectOp(":"); err != nil {
		return nil, err
	}
	body, err := p.pipe()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(";"); err != nil {
		return nil, err
	}
	rest, err := p.pipe()
	if err != nil {
		return nil, err
	}
	return Def{Name: name.text, Params: params, Body: body, Rest: rest}, nil
}

func (p *parser) comma() (Node, error) {
	left, err := p.assign()
	if err != nil {
		return nil, err
	}
	for p.isOp(",") {
		p.next()
		right, err := p.assign()
		if err != nil {
			return nil, err
		}
		left = Comma{Left: left, Right: right}
	}
	return left, nil
}

var assignOps = map[string]bool{"=": true, "|=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "//=": true}

func (p *parser) assign() (Node, error) {
	left, err := p.alt()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind == tOp && assignOps[t.text] {
		p.next()
		right, err := p.alt()
		if err != nil {
			return nil, err
		}
		return Assign{Op: t.text, Left: left, Right: right}, nil
	}
	return left, nil
}

func (p *parser) alt() (Node, error) {
	left, err := p.or()
	if err != nil {
		return nil, err
	}
	for p.isOp("//") {
		p.next()
		right, err := p.or()
		if err != nil {
			return nil, err
		}
		left = Alt{Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) or() (Node, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.isKw("or") {
		p.next()
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		left = Binary{Op: "or", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) and() (Node, error) {
	left, err := p.cmp()
	if err != nil {
		return nil, err
	}
	for p.isKw("and") {
		p.next()
		right, err := p.cmp()
		if err != nil {
			return nil, err
		}
		left = Binary{Op: "and", Left: left, Right: right}
	}
	return left, nil
}

var cmpOps = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *parser) cmp() (Node, error) {
	left, err := p.add()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind == tOp && cmpOps[t.text] {
		p.next()
		right, err := p.add()
		if err != nil {
			return nil, err
		}
		return Binary{Op: t.text, Left: left, Right: right}, nil
	}
	return left, nil
}

func (p *parser) add() (Node, error) {
	left, err := p.mul()
	if err != nil {
		return nil, err
	}
	for p.isOp("+") || p.isOp("-") {
		op := p.next().text
		right, err := p.mul()
		if err != nil {
			return nil, err
		}
		left = Binary{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) mul() (Node, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.isOp("*") || p.isOp("/") || p.isOp("%") {
		op := p.next().text
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		left = Binary{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) unary() (Node, error) {
	if p.isOp("-") {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Neg{X: x}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (Node, error) {
	n, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.peek().kind == tField:
			t := p.next()
			n = Pipe{Left: n, Right: Field{Name: t.text}}
		case p.isOp("[") || (p.isOp(".") && p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tOp && p.toks[p.i+1].text == "["):
			if p.isOp(".") {
				p.next()
			}
			suffix, err := p.bracket()
			if err != nil {
				return nil, err
			}
			n = Pipe{Left: n, Right: suffix}
		case p.isOp("?"):
			p.next()
			n = Optional{X: n}
		case p.isKw("as"):
			return n, nil
		default:
			return n, nil
		}
	}
}

// bracket parses [ ... ] after the opening bracket is current.
func (p *parser) bracket() (Node, error) {
	if err := p.expectOp("["); err != nil {
		return nil, err
	}
	if p.isOp("]") {
		p.next()
		return Index{}, nil
	}
	if p.isOp(":") {
		p.next()
		to, err := p.pipe()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp("]"); err != nil {
			return nil, err
		}
		return Slice{To: to}, nil
	}
	from, err := p.pipe()
	if err != nil {
		return nil, err
	}
	if p.isOp(":") {
		p.next()
		var to Node
		if !p.isOp("]") {
			to, err = p.pipe()
			if err != nil {
				return nil, err
			}
		}
		if err := p.expectOp("]"); err != nil {
			return nil, err
		}
		return Slice{From: from, To: to}, nil
	}
	if err := p.expectOp("]"); err != nil {
		return nil, err
	}
	return Index{Expr: from}, nil
}

func (p *parser) primary() (Node, error) {
	t := p.next()
	switch t.kind {
	case tField:
		return Field{Name: t.text}, nil
	case tNumber:
		v, _ := strconv.ParseFloat(t.text, 64)
		return Number{Value: v}, nil
	case tString:
		return p.str(t)
	case tVar:
		return Var{Name: t.text}, nil
	case tFormat:
		if p.peek().kind == tString {
			s, err := p.str(p.next())
			if err != nil {
				return nil, err
			}
			return Pipe{Left: s, Right: Format{Name: t.text}}, nil
		}
		return Format{Name: t.text}, nil
	case tOp:
		switch t.text {
		case ".":
			if p.isOp("[") {
				return p.bracket()
			}
			if p.peek().kind == tString {
				s := p.next()
				return Field{Name: s.text}, nil
			}
			return Identity{}, nil
		case "..":
			return RecurseAll{}, nil
		case "(":
			n, err := p.pipe()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			return n, nil
		case "[":
			if p.isOp("]") {
				p.next()
				return ArrayCon{}, nil
			}
			body, err := p.pipe()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp("]"); err != nil {
				return nil, err
			}
			return ArrayCon{Body: body}, nil
		case "{":
			return p.object()
		}
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("unexpected %q", t.text)}
	case tKeyword:
		switch t.text {
		case "if":
			return p.ifExpr()
		case "try":
			body, err := p.postfix()
			if err != nil {
				return nil, err
			}
			var catch Node
			if p.isKw("catch") {
				p.next()
				catch, err = p.postfix()
				if err != nil {
					return nil, err
				}
			}
			return Try{Body: body, Catch: catch}, nil
		case "reduce", "foreach":
			return p.reduce(t.text)
		case "not":
			return Call{Name: "not"}, nil
		case "label":
			v := p.next()
			if v.kind != tVar {
				return nil, &Error{Pos: v.pos, Msg: "expected $label"}
			}
			if err := p.expectOp("|"); err != nil {
				return nil, err
			}
			return p.pipe()
		case "def":
			p.i--
			return p.def()
		}
		return nil, &Error{Pos: t.pos, Msg: fmt.Sprintf("unexpected keyword %q", t.text)}
	case tIdent:
		var args []Node
		if p.isOp("(") {
			p.next()
			for {
				a, err := p.pipe()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if p.isOp(";") {
					p.next()
					continue
				}
				break
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
		}
		return Call{Name: t.text, Args: args}, nil
	case tEOF:
		return nil, &Error{Pos: t.pos, Msg: "unexpected end of filter"}
	}
	return nil, &Error{Pos: t.pos, Msg: "unexpected token"}
}

func (p *parser) str(t token) (Node, error) {
	s := Str{Parts: t.parts, Interp: make([]Node, len(t.parts))}
	for i, part := range t.parts {
		if !t.interp[i] {
			continue
		}
		n, err := Parse(part)
		if err != nil {
			return nil, &Error{Pos: t.pos, Msg: "bad interpolation: " + err.Error()}
		}
		s.Interp[i] = n
	}
	return s, nil
}

func (p *parser) object() (Node, error) {
	var entries []ObjEntry
	if p.isOp("}") {
		p.next()
		return ObjectCon{}, nil
	}
	for {
		var e ObjEntry
		t := p.next()
		switch t.kind {
		case tIdent, tKeyword:
			e.Key = t.text
		case tString:
			e.Key = t.text
			if strings.Contains(strings.Join(t.parts, ""), "\\(") {
				e.Computed = true
			}
		case tVar:
			e.KeyVar = t.text
		case tNumber:
			e.Key = t.text
		case tOp:
			if t.text != "(" {
				return nil, &Error{Pos: t.pos, Msg: "expected object key"}
			}
			k, err := p.pipe()
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			e.KeyExpr = k
			e.Computed = true
		default:
			return nil, &Error{Pos: t.pos, Msg: "expected object key"}
		}
		if p.isOp(":") {
			p.next()
			v, err := p.objValue()
			if err != nil {
				return nil, err
			}
			e.Value = v
		} else if e.KeyExpr != nil {
			return nil, &Error{Pos: p.peek().pos, Msg: "expected ':' after computed key"}
		}
		entries = append(entries, e)
		if p.isOp(",") {
			p.next()
			continue
		}
		break
	}
	if err := p.expectOp("}"); err != nil {
		return nil, err
	}
	return ObjectCon{Entries: entries}, nil
}

// objValue parses an object value: a pipe without commas at this level.
func (p *parser) objValue() (Node, error) {
	left, err := p.alt()
	if err != nil {
		return nil, err
	}
	for p.isOp("|") {
		p.next()
		right, err := p.alt()
		if err != nil {
			return nil, err
		}
		left = Pipe{Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) ifExpr() (Node, error) {
	cond, err := p.pipe()
	if err != nil {
		return nil, err
	}
	if err := p.expectKw("then"); err != nil {
		return nil, err
	}
	then, err := p.pipe()
	if err != nil {
		return nil, err
	}
	n := If{Cond: cond, Then: then}
	for p.isKw("elif") {
		p.next()
		c, err := p.pipe()
		if err != nil {
			return nil, err
		}
		if err := p.expectKw("then"); err != nil {
			return nil, err
		}
		t, err := p.pipe()
		if err != nil {
			return nil, err
		}
		n.Elifs = append(n.Elifs, c, t)
	}
	if p.isKw("else") {
		p.next()
		e, err := p.pipe()
		if err != nil {
			return nil, err
		}
		n.Else = e
	}
	if err := p.expectKw("end"); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) reduce(kind string) (Node, error) {
	src, err := p.postfix()
	if err != nil {
		return nil, err
	}
	if err := p.expectKw("as"); err != nil {
		return nil, err
	}
	v := p.next()
	if v.kind != tVar {
		return nil, &Error{Pos: v.pos, Msg: "expected $variable"}
	}
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	init, err := p.pipe()
	if err != nil {
		return nil, err
	}
	if err := p.expectOp(";"); err != nil {
		return nil, err
	}
	update, err := p.pipe()
	if err != nil {
		return nil, err
	}
	var extract Node
	if kind == "foreach" && p.isOp(";") {
		p.next()
		extract, err = p.pipe()
		if err != nil {
			return nil, err
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	if kind == "reduce" {
		return Reduce{Source: src, Var: v.text, Init: init, Update: update}, nil
	}
	return Foreach{Source: src, Var: v.text, Init: init, Update: update, Extract: extract}, nil
}
