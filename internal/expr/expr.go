// Package expr tokenises and parses GitHub Actions expressions (the inside
// of ${{ }} and bare if: values) into an AST, and collects the context
// access chains the shape checker validates (medium §6.3).
package expr

import (
	"fmt"
	"strconv"
	"strings"
)

// TokenKind classifies a token.
type TokenKind int

const (
	TokEOF TokenKind = iota
	TokIdent
	TokString
	TokNumber
	TokOp // operators and punctuation: ! < <= > >= == != && || ( ) [ ] . , *
	TokInvalid
)

func (k TokenKind) String() string {
	return [...]string{"EOF", "ident", "string", "number", "op", "invalid"}[k]
}

// Token is one lexical token. Pos is the byte offset in the expression.
type Token struct {
	Kind TokenKind
	Text string
	Pos  int
}

// Error is a parse error with its byte offset.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("at %d: %s", e.Pos, e.Msg) }

// Tokenize splits an expression into tokens.
func Tokenize(src string) ([]Token, error) {
	var toks []Token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			start := i
			i++
			var b strings.Builder
			for {
				if i >= len(src) {
					return nil, &Error{Pos: start, Msg: "unterminated string"}
				}
				if src[i] == '\'' {
					if i+1 < len(src) && src[i+1] == '\'' {
						b.WriteByte('\'')
						i += 2
						continue
					}
					i++
					break
				}
				b.WriteByte(src[i])
				i++
			}
			toks = append(toks, Token{Kind: TokString, Text: b.String(), Pos: start})
		case isDigit(c) || (c == '-' && i+1 < len(src) && isDigit(src[i+1])):
			// GitHub expressions have no binary minus, so '-' before a digit
			// is always a sign.
			start := i
			i++
			for i < len(src) && (isDigit(src[i]) || src[i] == '.' || src[i] == 'x' || src[i] == 'X' || src[i] == 'e' || src[i] == 'E' || isHex(src[i]) || ((src[i] == '-' || src[i] == '+') && (src[i-1] == 'e' || src[i-1] == 'E'))) {
				i++
			}
			text := src[start:i]
			if _, err := parseNumber(text); err != nil {
				return nil, &Error{Pos: start, Msg: "bad number " + text}
			}
			toks = append(toks, Token{Kind: TokNumber, Text: text, Pos: start})
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			toks = append(toks, Token{Kind: TokIdent, Text: src[start:i], Pos: start})
		default:
			start := i
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch {
			case two == "<=" || two == ">=" || two == "==" || two == "!=" || two == "&&" || two == "||":
				toks = append(toks, Token{Kind: TokOp, Text: two, Pos: start})
				i += 2
			case strings.ContainsRune("!<>()[].,*", rune(c)):
				toks = append(toks, Token{Kind: TokOp, Text: string(c), Pos: start})
				i++
			default:
				return nil, &Error{Pos: start, Msg: fmt.Sprintf("unexpected character %q", c)}
			}
		}
	}
	toks = append(toks, Token{Kind: TokEOF, Pos: len(src)})
	return toks, nil
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isHex(c byte) bool        { return (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }
func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentChar(c byte) bool  { return isIdentStart(c) || isDigit(c) || c == '-' }

func parseNumber(s string) (float64, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n, err := strconv.ParseInt(s[2:], 16, 64)
		return float64(n), err
	}
	if strings.HasPrefix(s, "-0x") || strings.HasPrefix(s, "-0X") {
		n, err := strconv.ParseInt(s[3:], 16, 64)
		return -float64(n), err
	}
	return strconv.ParseFloat(s, 64)
}

// --- AST ---------------------------------------------------------------------

// Node is an AST node. Pos is the byte offset of its first token.
type Node interface{ Pos() int }

// LitKind is a literal's type.
type LitKind int

const (
	LitNull LitKind = iota
	LitBool
	LitNumber
	LitString
)

type Literal struct {
	Kind LitKind
	Str  string
	Num  float64
	Bool bool
	P    int
}

type Ident struct {
	Name string
	P    int
}

// Member is x.name.
type Member struct {
	X    Node
	Name string
	P    int
}

// Index is x[expr].
type Index struct {
	X   Node
	Idx Node
	P   int
}

// Wildcard is x.*.
type Wildcard struct {
	X Node
	P int
}

type Call struct {
	Name string
	Args []Node
	P    int
}

type Unary struct {
	Op string
	X  Node
	P  int
}

type Binary struct {
	Op   string
	X, Y Node
	P    int
}

func (n *Literal) Pos() int  { return n.P }
func (n *Ident) Pos() int    { return n.P }
func (n *Member) Pos() int   { return n.P }
func (n *Index) Pos() int    { return n.P }
func (n *Wildcard) Pos() int { return n.P }
func (n *Call) Pos() int     { return n.P }
func (n *Unary) Pos() int    { return n.P }
func (n *Binary) Pos() int   { return n.P }

// --- parser --------------------------------------------------------------------

type parser struct {
	toks []Token
	i    int
}

// Parse parses an expression.
func Parse(src string) (Node, error) {
	toks, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.Kind != TokEOF {
		return nil, &Error{Pos: t.Pos, Msg: fmt.Sprintf("unexpected %q", t.Text)}
	}
	return n, nil
}

func (p *parser) peek() Token { return p.toks[p.i] }
func (p *parser) next() Token {
	t := p.toks[p.i]
	if t.Kind != TokEOF {
		p.i++
	}
	return t
}

func (p *parser) isOp(text string) bool {
	t := p.peek()
	return t.Kind == TokOp && t.Text == text
}

// precedence levels, lowest first.
var precedence = map[string]int{"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4}

func (p *parser) binary(minPrec int) (Node, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		prec, ok := precedence[t.Text]
		if t.Kind != TokOp || !ok || prec <= minPrec {
			return left, nil
		}
		p.next()
		right, err := p.binary(prec)
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: t.Text, X: left, Y: right, P: left.Pos()}
	}
}

func (p *parser) unary() (Node, error) {
	if p.isOp("!") {
		t := p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "!", X: x, P: t.Pos}, nil
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
		case p.isOp("."):
			dot := p.next()
			t := p.next()
			switch {
			case t.Kind == TokOp && t.Text == "*":
				n = &Wildcard{X: n, P: n.Pos()}
			case t.Kind == TokIdent || t.Kind == TokNumber:
				n = &Member{X: n, Name: t.Text, P: n.Pos()}
			default:
				return nil, &Error{Pos: dot.Pos, Msg: "expected property name after '.'"}
			}
		case p.isOp("["):
			p.next()
			idx, err := p.binary(0)
			if err != nil {
				return nil, err
			}
			if !p.isOp("]") {
				return nil, &Error{Pos: p.peek().Pos, Msg: "expected ']'"}
			}
			p.next()
			n = &Index{X: n, Idx: idx, P: n.Pos()}
		default:
			return n, nil
		}
	}
}

func (p *parser) primary() (Node, error) {
	t := p.next()
	switch t.Kind {
	case TokString:
		return &Literal{Kind: LitString, Str: t.Text, P: t.Pos}, nil
	case TokNumber:
		v, _ := parseNumber(t.Text)
		return &Literal{Kind: LitNumber, Num: v, P: t.Pos}, nil
	case TokIdent:
		switch strings.ToLower(t.Text) {
		case "true":
			return &Literal{Kind: LitBool, Bool: true, P: t.Pos}, nil
		case "false":
			return &Literal{Kind: LitBool, Bool: false, P: t.Pos}, nil
		case "null":
			return &Literal{Kind: LitNull, P: t.Pos}, nil
		}
		if p.isOp("(") {
			p.next()
			var args []Node
			if !p.isOp(")") {
				for {
					a, err := p.binary(0)
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.isOp(",") {
						p.next()
						continue
					}
					break
				}
			}
			if !p.isOp(")") {
				return nil, &Error{Pos: p.peek().Pos, Msg: "expected ')' after arguments"}
			}
			p.next()
			return &Call{Name: t.Text, Args: args, P: t.Pos}, nil
		}
		return &Ident{Name: t.Text, P: t.Pos}, nil
	case TokOp:
		if t.Text == "(" {
			n, err := p.binary(0)
			if err != nil {
				return nil, err
			}
			if !p.isOp(")") {
				return nil, &Error{Pos: p.peek().Pos, Msg: "expected ')'"}
			}
			p.next()
			return n, nil
		}
		return nil, &Error{Pos: t.Pos, Msg: fmt.Sprintf("unexpected %q", t.Text)}
	case TokEOF:
		return nil, &Error{Pos: t.Pos, Msg: "unexpected end of expression"}
	}
	return nil, &Error{Pos: t.Pos, Msg: "unexpected token"}
}

// --- chains ----------------------------------------------------------------------

// Seg is one access step of a chain.
type Seg struct {
	// Name is the field for dot access or a string-literal index; Index is
	// the integer for a number-literal index (Name == "", IsIndex true).
	Name    string
	Index   int
	IsIndex bool
	// Dynamic: a bracket access with a non-literal index; the chain stops
	// being checkable here.
	Dynamic  bool
	Wildcard bool
}

// Chain is a rooted context access: root.seg.seg... Call wraps the chain
// when it is the direct argument of fromJSON/toJSON (the name).
type Chain struct {
	Root string
	Segs []Seg
	Pos  int
	// Wrapped is the enclosing function when the chain is the first
	// argument of a call, e.g. "fromJSON".
	Wrapped string
	// Inner, for fromJSON(x) chains, is the chain of x.
	Inner *Chain
}

// String renders the chain in dot form.
func (c Chain) String() string {
	var b strings.Builder
	if c.Wrapped != "" {
		b.WriteString(c.Wrapped + "(")
	}
	b.WriteString(c.Root)
	if c.Inner != nil {
		b.WriteString("(" + c.Inner.String() + ")")
	}
	for _, s := range c.Segs {
		switch {
		case s.Wildcard:
			b.WriteString(".*")
		case s.Dynamic:
			b.WriteString("[…]")
		case s.IsIndex:
			fmt.Fprintf(&b, "[%d]", s.Index)
		default:
			b.WriteString("." + s.Name)
		}
	}
	if c.Wrapped != "" {
		b.WriteString(")")
	}
	return b.String()
}

// Chains collects every context access chain in the AST, including chains
// that continue after a fromJSON(...) call (represented as a Chain whose
// Root is "fromJSON" and whose Inner is the argument chain).
func Chains(n Node) []Chain {
	var out []Chain
	var visit func(n Node)
	visit = func(n Node) {
		switch x := n.(type) {
		case *Member, *Index, *Wildcard, *Ident:
			if c, ok := chainOf(n); ok {
				out = append(out, c)
				// Dynamic indexes may themselves contain chains.
				collectIndexChains(n, &out)
				return
			}
			visitChildren(n, visit)
		case *Call:
			if c, ok := chainOf(n); ok {
				out = append(out, c)
				for _, a := range x.Args[1:] {
					visit(a)
				}
				if len(x.Args) > 0 {
					if _, isChain := x.Args[0].(*Call); isChain {
						visit(x.Args[0])
					}
				}
				return
			}
			for _, a := range x.Args {
				visit(a)
			}
		case *Unary:
			visit(x.X)
		case *Binary:
			visit(x.X)
			visit(x.Y)
		}
	}
	visit(n)
	return out
}

func visitChildren(n Node, visit func(Node)) {
	switch x := n.(type) {
	case *Member:
		visit(x.X)
	case *Index:
		visit(x.X)
		visit(x.Idx)
	case *Wildcard:
		visit(x.X)
	}
}

func collectIndexChains(n Node, out *[]Chain) {
	switch x := n.(type) {
	case *Member:
		collectIndexChains(x.X, out)
	case *Wildcard:
		collectIndexChains(x.X, out)
	case *Index:
		collectIndexChains(x.X, out)
		if _, lit := x.Idx.(*Literal); !lit {
			*out = append(*out, Chains(x.Idx)...)
		}
	}
}

// chainOf flattens a postfix chain rooted at an identifier or at
// fromJSON(...)/toJSON(...).
func chainOf(n Node) (Chain, bool) {
	var segs []Seg
	cur := n
	for {
		switch x := cur.(type) {
		case *Member:
			segs = append([]Seg{{Name: x.Name}}, segs...)
			cur = x.X
		case *Wildcard:
			segs = append([]Seg{{Wildcard: true}}, segs...)
			cur = x.X
		case *Index:
			switch idx := x.Idx.(type) {
			case *Literal:
				if idx.Kind == LitString {
					segs = append([]Seg{{Name: idx.Str}}, segs...)
				} else if idx.Kind == LitNumber {
					segs = append([]Seg{{IsIndex: true, Index: int(idx.Num)}}, segs...)
				} else {
					segs = append([]Seg{{Dynamic: true}}, segs...)
				}
			default:
				segs = append([]Seg{{Dynamic: true}}, segs...)
			}
			cur = x.X
		case *Ident:
			return Chain{Root: x.Name, Segs: segs, Pos: x.P}, true
		case *Call:
			name := strings.ToLower(x.Name)
			if (name == "fromjson" || name == "tojson") && len(x.Args) == 1 {
				c := Chain{Root: x.Name, Segs: segs, Pos: x.P, Wrapped: ""}
				if inner, ok := chainOf(x.Args[0]); ok {
					c.Inner = &inner
				} else if lit, ok := x.Args[0].(*Literal); ok && lit.Kind == LitString {
					c.Inner = &Chain{Root: "'" + lit.Str + "'", Pos: lit.P}
				}
				return c, true
			}
			return Chain{}, false
		default:
			return Chain{}, false
		}
	}
}
