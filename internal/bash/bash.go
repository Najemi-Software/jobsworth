// Package bash analyses `run:` scripts (medium §0.2, §0.3, §2): which keys
// a script writes to $GITHUB_OUTPUT, $GITHUB_ENV and $GITHUB_PATH, whether
// those targets are open (keys we cannot name), the literal line stream to
// each target, deprecated workflow commands, reads of $GITHUB_OUTPUT, and
// same-step $GITHUB_ENV use. It parses with mvdan.cc/sh and never executes.
package bash

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Sentinel replaces every ${{ ... }} before parsing so the shell parser sees
// valid syntax (medium §0.2). Anything containing it is dynamic.
const Sentinel = "__JOBSWORTH_EXPR__"

// Target is one of the runner's command files.
type Target int

const (
	Output Target = iota
	Env
	Path
)

func (t Target) String() string {
	switch t {
	case Output:
		return "$GITHUB_OUTPUT"
	case Env:
		return "$GITHUB_ENV"
	case Path:
		return "$GITHUB_PATH"
	}
	return "?"
}

var targetVars = map[string]Target{"GITHUB_OUTPUT": Output, "GITHUB_ENV": Env, "GITHUB_PATH": Path}

// Write is one key written to a target.
type Write struct {
	// Source describes where the value comes from (medium §6.2.8); nil when
	// the write form carries no single value (heredoc bodies, printf with
	// several arguments).
	Source *ValueSource
	Key    string
	// Value is the literal value text ("" when dynamic or multi-line).
	Value string
	Lines []string
	Line  int
	// Definite: written on every path through the script.
	Definite bool
	// ValueDynamic: the value contains an expansion or the sentinel.
	ValueDynamic bool
	// Multiline: written with the KEY<<DELIM form; Lines holds the body.
	Multiline bool
}

// ValueKind classifies a value source.
type ValueKind int

const (
	// ValueLiteral: literal text, in Text.
	ValueLiteral ValueKind = iota
	// ValueVar: a single $VAR, name in Text.
	ValueVar
	// ValueCommand: a single $( ... ) substitution, in Cmd.
	ValueCommand
	// ValueMixed: several parts; the value is a string of unknown content.
	ValueMixed
	// ValueExpr: the sentinel (a ${{ }} expression); the expression text is
	// not available here.
	ValueExpr
)

// ValueSource is where a written (or assigned) value comes from.
type ValueSource struct {
	Kind ValueKind
	Text string
	Cmd  *CommandInfo
	// Vars names every parameter the value expands (ValueVar: the one;
	// ValueMixed: all of them), for taint tracking (advanced §1.2).
	Vars []string
}

// CommandInfo is a command inside $( ... ): its name, its literal args,
// what is piped into it, and any `< file` input.
type CommandInfo struct {
	Name string
	Args []CommandArg
	// Input is the command piped into this one (left of |), if any.
	Input *CommandInfo
	// InFile is a literal `< file` redirect, "" when absent or dynamic.
	InFile string
	// Heredoc is a literal heredoc body fed on stdin, if any.
	Heredoc string
	// Line is the script line.
	Line int
}

// CommandArg is one argument: its rendered text and whether it is literal.
type CommandArg struct {
	Text    string
	Literal bool
}

// LiteralArgs returns the literal argument texts, with "" for dynamic ones.
func (c *CommandInfo) LiteralArgs() []string {
	out := make([]string, len(c.Args))
	for i, a := range c.Args {
		if a.Literal {
			out[i] = a.Text
		}
	}
	return out
}

// JQCall is a jq invocation anywhere in the script with a literal filter.
type JQCall struct {
	Filter string
	Line   int
	// Cmd is the full command, for shape application.
	Cmd *CommandInfo
}

// LineWrite is one literal line in a target's stream, in order (§2.4).
type LineWrite struct {
	Text string
	Line int
	// Source is the value source after "k=" when the line came from a
	// single-argument echo/printf; nil otherwise.
	Source *ValueSource
	// Dynamic: contains an expansion or the sentinel; skipped by the
	// line-shape rules.
	Dynamic bool
	// Opaque: part of the text comes from a command substitution of an
	// opaque command (git, jq, ...).
	Opaque bool
	// InHeredoc: this line is inside a KEY<<DELIM value block.
	InHeredoc bool
	// EscapedNewline: an echo without -e whose text held a literal \n.
	EscapedNewline bool
}

// Heredoc is one KEY<<DELIM opener seen in a target's stream (§2.5).
type Heredoc struct {
	Key   string
	Delim string
	Line  int
	// OpaqueLines counts value lines that came from an opaque command.
	OpaqueLines int
	Terminated  bool
	EmptyDelim  bool
	Indented    bool
	// EscapedNewline: the opener came from an echo "KEY<<EOF\n..." without -e.
	EscapedNewline bool
}

// TargetState is everything known about one target after analysis.
type TargetState struct {
	Writes map[string]*Write
	Open   bool
	// OpenReason says what made the target open.
	OpenReason string
	Lines      []LineWrite
	Heredocs   []Heredoc
}

// Keys returns the written keys sorted.
func (t *TargetState) Keys() []string {
	out := make([]string, 0, len(t.Writes))
	for k := range t.Writes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParseError is a bash syntax error with position.
type ParseError struct {
	Line, Col int
	Msg       string
}

func (e *ParseError) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

// Deprecation is a ::set-output style command emitted to stdout (§2.1).
type Deprecation struct {
	Command     string // e.g. "::set-output"
	Replacement string // e.g. "$GITHUB_OUTPUT"
	Line        int
}

// KV is an echo "K=V" that is not redirected anywhere (§2.2).
type KV struct {
	Key  string
	Line int
}

// Read is a read of $GITHUB_OUTPUT (§2.3).
type Read struct {
	Line int
	How  string
}

// EnvUse is a $K reference after K was written to $GITHUB_ENV (§2.6).
type EnvUse struct {
	Key       string
	WriteLine int
	RefLine   int
}

// Statement is one top-level or nested statement as the analyser classified
// it (spec-explain §3.1): what it was, what it wrote where.
type Statement struct {
	Line int
	Text string
	// Class: echo, printf, heredoc, group, subshell, tee, assignment,
	// call <cmd>, if, case, for, while, function, opaque <reason>, exec,
	// exit, pipe.
	Class string
	// Redirect names the target the statement's stdout went to, "".
	Redirect string
	// Keys are the output keys the statement wrote (k=v lines).
	Keys []string
	// Definite: the statement is on every path.
	Definite bool
	// Open says the statement opened a target (dynamic key, script, ...).
	Open string
}

// Analysis is the result for one script.
type Analysis struct {
	ParseErr *ParseError
	// Statements lists every statement the analyser classified, in order.
	Statements   []Statement
	Targets      map[Target]*TargetState
	Deprecated   []Deprecation
	Unredirected []KV
	Reads        []Read
	EnvSameStep  []EnvUse
	// Vars holds the last assignment to each shell variable (§6.2.8).
	Vars map[string]*ValueSource
	// JQ lists every jq invocation with a literal filter (§6.2.10).
	JQ []JQCall
}

// Target returns the state for t (never nil after Analyse).
func (a *Analysis) Target(t Target) *TargetState {
	return a.Targets[t]
}

// DefaultOpaque are commands whose output is treated as opaque for
// heredoc-delimiter collision (§2.5).
var DefaultOpaque = []string{"git", "jq", "python", "python3", "node", "curl", "kubectl", "docker", "awk", "sed", "perl", "make", "yq", "cat", "find", "ls", "gh", "aws", "terraform", "helm", "npm", "yarn", "pnpm", "go", "cargo"}

var deprecatedCommands = map[string]string{
	"::set-output": "$GITHUB_OUTPUT",
	"::save-state": "$GITHUB_STATE",
	"::set-env":    "$GITHUB_ENV",
	"::add-path":   "$GITHUB_PATH",
}

// readers are commands whose pure file argument counts as a read (§2.3).
var readers = map[string]bool{
	"cat": true, "grep": true, "egrep": true, "fgrep": true, "rg": true, "head": true, "tail": true, "awk": true, "sed": true,
	"wc": true, "stat": true, "file": true, "less": true, "more": true, "[": true, "test": true, "source": true, ".": true,
}

var interpreters = map[string]bool{"python": true, "python3": true, "node": true, "ruby": true, "perl": true, "php": true, "deno": true, "bun": true}

// DefaultRunners are package runners that execute a script or task named
// by their arguments (medium review 2 D8): the script may write anything.
// An entry is a command name, or a command plus its first word.
var DefaultRunners = []string{
	"uv run", "uvx", "npx", "pnpm exec", "pnpm dlx", "yarn", "bun run", "deno run",
	"poetry run", "pipx run", "pipenv run", "go run", "cargo run", "bundle exec",
	"make", "tox", "nox", "just", "task", "./gradlew", "gradlew", "mvn",
}

// scriptExts are argument suffixes that name a script to execute.
var scriptExts = []string{".py", ".js", ".ts", ".mjs", ".rb", ".pl", ".php", ".sh"}

// fileTools take script names as data, not as something to run.
var fileTools = map[string]bool{
	"git": true, "cp": true, "mv": true, "rm": true, "chmod": true, "chown": true, "touch": true, "mkdir": true,
	"diff": true, "echo": true, "printf": true, "tee": true, "sort": true, "find": true, "ls": true, "cat": true,
	"grep": true, "head": true, "tail": true, "wc": true, "sed": true, "awk": true, "stat": true, "file": true,
	"test": true, "[": true, "shellcheck": true, "ruff": true, "black": true, "flake8": true, "mypy": true,
	"eslint": true, "prettier": true, "rubocop": true, "pylint": true, "isort": true,
}

// Options tunes an analysis.
type Options struct {
	// Opaque extends DefaultOpaque (heredoc value opacity).
	Opaque []string
	// Runners extends DefaultRunners.
	Runners []string
	// Seeds are variables known before the script runs: $GITHUB_ENV
	// writes of earlier steps and literal env: entries (advanced §7.1
	// cross-step variables).
	Seeds map[string]string
}

// KeyCandidateCap bounds dynamic-key expansion (advanced §7.1).
const KeyCandidateCap = 64

// ReplaceExpressions substitutes every ${{ ... }} with the sentinel.
func ReplaceExpressions(script string) string {
	var b strings.Builder
	for {
		i := strings.Index(script, "${{")
		if i < 0 {
			b.WriteString(script)
			return b.String()
		}
		b.WriteString(script[:i])
		rest := script[i+3:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			b.WriteString(Sentinel)
			return b.String()
		}
		b.WriteString(Sentinel)
		script = rest[j+2:]
	}
}

// Analyse parses and analyses a script. posix selects sh semantics. opaque
// extends DefaultOpaque.
func Analyse(script string, posix bool, opaque []string) *Analysis {
	return AnalyseWith(script, posix, Options{Opaque: opaque})
}

// AnalyseWith is Analyse with runner and opaque extensions.
func AnalyseWith(script string, posix bool, o Options) *Analysis {
	opaque := o.Opaque
	a := &Analysis{Targets: map[Target]*TargetState{}, Vars: map[string]*ValueSource{}}
	for _, t := range []Target{Output, Env, Path} {
		a.Targets[t] = &TargetState{Writes: map[string]*Write{}}
	}
	variant := syntax.LangBash
	if posix {
		variant = syntax.LangPOSIX
	}
	parser := syntax.NewParser(syntax.Variant(variant), syntax.KeepComments(false))
	prog, err := parser.Parse(strings.NewReader(ReplaceExpressions(script)), "run")
	if err != nil {
		pe := &ParseError{Msg: err.Error()}
		if se, ok := err.(syntax.ParseError); ok {
			pe.Line, pe.Col, pe.Msg = int(se.Pos.Line()), int(se.Pos.Col()), se.Text
		}
		a.ParseErr = pe
		return a
	}
	w := &walker{a: a, funcs: map[string]*syntax.FuncDecl{}, opaque: map[string]bool{}, envWrites: map[string]int{}, assigned: map[string]bool{}, runners: map[string]bool{}, loopVars: map[string][]string{}, mutated: map[string]bool{}, seeded: map[string]bool{}}
	for _, r := range DefaultRunners {
		w.runners[r] = true
	}
	for _, r := range o.Runners {
		w.runners[strings.TrimSpace(r)] = true
	}
	for _, c := range DefaultOpaque {
		w.opaque[c] = true
	}
	for _, c := range opaque {
		w.opaque[c] = true
	}
	for name, v := range o.Seeds {
		w.a.Vars[name] = &ValueSource{Kind: ValueLiteral, Text: v}
		w.seeded[name] = true
	}
	st := w.stmts(prog.Stmts, pathCtx{definite: true})
	w.commit(st)
	for _, t := range []Target{Output, Env, Path} {
		w.finishHeredocs(a.Targets[t])
	}
	return a
}

// --- walker ----------------------------------------------------------------

// pathCtx describes the path a statement is on.
type pathCtx struct {
	definite bool
	// redirect is the target an enclosing group/exec redirects stdout to.
	redirect *Target
	// inFunc guards recursion when inlining functions.
	inFunc map[string]bool
	// posArgs are the literal positional arguments of the function being
	// inlined ("" for a non-literal one).
	posArgs []string
}

// pathState is the accumulated outcome of a statement list on one path.
type pathState struct {
	writes map[Target]map[string]*Write
	lines  map[Target][]LineWrite
	open   map[Target]string
	exited bool
}

func newState() pathState {
	return pathState{writes: map[Target]map[string]*Write{}, lines: map[Target][]LineWrite{}, open: map[Target]string{}}
}

type walker struct {
	a       *Analysis
	funcs   map[string]*syntax.FuncDecl
	opaque  map[string]bool
	runners map[string]bool
	// loopVars binds for-loop variables to their literal items while the
	// body is walked; mutated marks variables reassigned dynamically;
	// seeded marks variables known from earlier steps.
	loopVars  map[string][]string
	mutated   map[string]bool
	seeded    map[string]bool
	envWrites map[string]int // key -> line written to $GITHUB_ENV
	// assigned holds shell variables the script itself assigns; reading
	// one of those after an identically named $GITHUB_ENV write is fine.
	assigned map[string]bool
}

// commit folds a top-level state into the analysis.
func (w *walker) commit(st pathState) {
	for t, keys := range st.writes {
		ts := w.a.Targets[t]
		for k, wr := range keys {
			ts.Writes[k] = wr
		}
	}
	for t, ls := range st.lines {
		w.a.Targets[t].Lines = append(w.a.Targets[t].Lines, ls...)
	}
	for t, reason := range st.open {
		w.a.Targets[t].Open = true
		w.a.Targets[t].OpenReason = reason
	}
}

// merge combines branch states: a key is definite only when definite in
// every branch; lines concatenate; open propagates; exited only if all
// branches exit.
func merge(branches []pathState) pathState {
	out := newState()
	if len(branches) == 0 {
		return out
	}
	counts := map[Target]map[string]int{}
	for _, b := range branches {
		for t, keys := range b.writes {
			if counts[t] == nil {
				counts[t] = map[string]int{}
			}
			if out.writes[t] == nil {
				out.writes[t] = map[string]*Write{}
			}
			for k, wr := range keys {
				if wr.Definite {
					counts[t][k]++
				}
				if prev, ok := out.writes[t][k]; !ok || (!prev.Definite && wr.Definite) {
					cp := *wr
					out.writes[t][k] = &cp
				}
			}
		}
		for t, ls := range b.lines {
			out.lines[t] = append(out.lines[t], ls...)
		}
		for t, r := range b.open {
			out.open[t] = r
		}
	}
	for t, keys := range out.writes {
		for k, wr := range keys {
			wr.Definite = counts[t][k] == len(branches)
		}
	}
	exited := true
	for _, b := range branches {
		exited = exited && b.exited
	}
	out.exited = exited
	return out
}

// seq appends b after a on one path: writes after an exit are dropped.
func seq(a, b pathState) pathState {
	if a.exited {
		return a
	}
	for t, keys := range b.writes {
		if a.writes[t] == nil {
			a.writes[t] = map[string]*Write{}
		}
		for k, wr := range keys {
			if prev, ok := a.writes[t][k]; ok && prev.Definite && !wr.Definite {
				continue
			}
			a.writes[t][k] = wr
		}
	}
	for t, ls := range b.lines {
		a.lines[t] = append(a.lines[t], ls...)
	}
	for t, r := range b.open {
		a.open[t] = r
	}
	a.exited = b.exited
	return a
}

func conditional(st pathState) pathState {
	for _, keys := range st.writes {
		for _, wr := range keys {
			wr.Definite = false
		}
	}
	st.exited = false
	return st
}

func (w *walker) stmts(list []*syntax.Stmt, ctx pathCtx) pathState {
	st := newState()
	for _, s := range list {
		st = seq(st, w.stmt(s, ctx))
		if st.exited {
			break
		}
	}
	return st
}

func (w *walker) stmt(s *syntax.Stmt, ctx pathCtx) pathState {
	if s.Cmd == nil {
		return newState()
	}
	// Redirects on the statement itself.
	target, hasTarget := stdoutTarget(s.Redirs)
	w.checkReads(s)
	if s.Background {
		ctx.definite = false
	}
	switch c := s.Cmd.(type) {
	case *syntax.CallExpr:
		st := w.call(c, s, target, hasTarget, ctx)
		w.record(s, w.classOf(c, st), target, hasTarget, ctx, st)
		return st
	case *syntax.IfClause:
		w.record(s, "if", target, hasTarget, ctx, newState())
		return w.ifClause(c, ctx)
	case *syntax.CaseClause:
		w.record(s, "case", target, hasTarget, ctx, newState())
		return w.caseClause(c, ctx)
	case *syntax.WhileClause:
		w.record(s, "while", target, hasTarget, ctx, newState())
		return conditional(w.stmts(c.Do, ctx))
	case *syntax.ForClause:
		w.record(s, "for", target, hasTarget, ctx, newState())
		if it, ok := c.Loop.(*syntax.WordIter); ok && it.Name != nil {
			items, literal := literalItems(it.Items)
			name := it.Name.Value
			if literal && len(items) > 0 {
				prev, had := w.loopVars[name]
				w.loopVars[name] = items
				w.assigned[name] = true
				out := conditional(w.stmts(c.Do, ctx))
				if had {
					w.loopVars[name] = prev
				} else {
					delete(w.loopVars, name)
				}
				return out
			}
			w.assigned[name] = true
			w.mutated[name] = true
		}
		return conditional(w.stmts(c.Do, ctx))
	case *syntax.Block:
		inner := ctx
		if hasTarget {
			inner.redirect = &target
		}
		w.record(s, "group", target, hasTarget, ctx, newState())
		return w.stmts(c.Stmts, inner)
	case *syntax.Subshell:
		inner := ctx
		if hasTarget {
			inner.redirect = &target
		}
		w.record(s, "subshell", target, hasTarget, ctx, newState())
		return w.stmts(c.Stmts, inner)
	case *syntax.BinaryCmd:
		return w.binary(c, ctx)
	case *syntax.FuncDecl:
		w.funcs[c.Name.Value] = c
		w.record(s, "function "+c.Name.Value, target, hasTarget, ctx, newState())
		return newState()
	case *syntax.DeclClause:
		for _, as := range c.Args {
			if as.Name != nil {
				w.assigned[as.Name.Value] = true
			}
		}
		return newState()
	case *syntax.LetClause, *syntax.ArithmCmd, *syntax.TestClause, *syntax.TimeClause, *syntax.CoprocClause:
		return newState()
	}
	return newState()
}

// record appends a Statement describing s and what it wrote on this path.
func (w *walker) record(s *syntax.Stmt, class string, target Target, hasTarget bool, ctx pathCtx, st pathState) {
	stmt := Statement{Line: int(s.Pos().Line()), Text: w.stmtText(s), Class: class, Definite: ctx.definite}
	if hasTarget {
		stmt.Redirect = strings.TrimPrefix(target.String(), "$")
	} else if ctx.redirect != nil {
		stmt.Redirect = strings.TrimPrefix(ctx.redirect.String(), "$")
	}
	for t, keys := range st.writes {
		for k := range keys {
			stmt.Keys = append(stmt.Keys, k)
		}
		if stmt.Redirect == "" {
			stmt.Redirect = strings.TrimPrefix(t.String(), "$")
		}
	}
	sort.Strings(stmt.Keys)
	for _, reason := range st.open {
		stmt.Open = reason
		break
	}
	w.a.Statements = append(w.a.Statements, stmt)
}

// classOf names a simple command for the statement list.
func (w *walker) classOf(c *syntax.CallExpr, st pathState) string {
	name := cmdName(c)
	if len(c.Args) == 0 {
		return "assignment"
	}
	for _, reason := range st.open {
		return "opaque: " + reason
	}
	switch name {
	case "echo", "printf", "cat", "exec", "exit", "eval", "source", ".":
		return name
	}
	if _, ok := w.funcs[name]; ok {
		return "call " + name + " (function)"
	}
	return "call " + name
}

// stmtText renders a statement's source on one line.
func (w *walker) stmtText(s *syntax.Stmt) string {
	var b strings.Builder
	printer := syntax.NewPrinter()
	_ = printer.Print(&b, s)
	text := strings.TrimSpace(b.String())
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i] + " …"
	}
	return text
}

func (w *walker) ifClause(c *syntax.IfClause, ctx pathCtx) pathState {
	var branches []pathState
	cur := c
	hasElse := false
	for cur != nil {
		// Conditions execute on the main path; a write inside `if cmd >> $GITHUB_OUTPUT; then` is rare and ignored.
		w.stmts(cur.Cond, ctx)
		branches = append(branches, w.stmts(cur.Then, ctx))
		if cur.Else == nil {
			break
		}
		if len(cur.Else.Cond) == 0 {
			// plain else
			branches = append(branches, w.stmts(cur.Else.Then, ctx))
			hasElse = true
			break
		}
		cur = cur.Else
	}
	if !hasElse {
		branches = append(branches, newState())
	}
	return merge(branches)
}

func (w *walker) caseClause(c *syntax.CaseClause, ctx pathCtx) pathState {
	var branches []pathState
	hasDefault := false
	for _, item := range c.Items {
		for _, p := range item.Patterns {
			if lit := literal(p); lit == "*" {
				hasDefault = true
			}
		}
		branches = append(branches, w.stmts(item.Stmts, ctx))
	}
	if !hasDefault {
		branches = append(branches, newState())
	}
	return merge(branches)
}

func (w *walker) binary(c *syntax.BinaryCmd, ctx pathCtx) pathState {
	switch c.Op {
	case syntax.AndStmt, syntax.OrStmt:
		left := w.stmt(c.X, ctx)
		inner := ctx
		inner.definite = false
		right := conditional(w.stmt(c.Y, inner))
		return seq(left, right)
	case syntax.Pipe, syntax.PipeAll:
		// `producer | tee [-a] $GITHUB_OUTPUT`: the producer's stdout lines
		// go to the target.
		if call, ok := c.Y.Cmd.(*syntax.CallExpr); ok {
			if name := cmdName(call); name == "tee" {
				for _, arg := range call.Args[1:] {
					if t, ok := targetWord(arg); ok {
						st := w.producerTo(c.X, t, ctx)
						w.record(c.X, "tee", t, true, ctx, st)
						return st
					}
				}
			}
		}
		left := w.stmt(c.X, ctx)
		right := w.stmt(c.Y, ctx)
		return seq(left, right)
	}
	return newState()
}

// producerTo analyses a statement whose stdout is piped to target.
func (w *walker) producerTo(s *syntax.Stmt, t Target, ctx pathCtx) pathState {
	if call, ok := s.Cmd.(*syntax.CallExpr); ok {
		return w.call(call, s, t, true, ctx)
	}
	inner := ctx
	inner.redirect = &t
	return w.stmt(s, inner)
}

// call analyses one simple command.
func (w *walker) call(c *syntax.CallExpr, s *syntax.Stmt, target Target, hasTarget bool, ctx pathCtx) pathState {
	st := newState()
	line := int(c.Pos().Line())
	for _, as := range c.Assigns {
		if as.Name != nil {
			w.assigned[as.Name.Value] = true
			if as.Value != nil {
				src := w.sourceOf(as.Value, 0)
				w.a.Vars[as.Name.Value] = src
				if src == nil || src.Kind != ValueLiteral || !ctx.definite {
					w.mutated[as.Name.Value] = true
				} else {
					delete(w.mutated, as.Name.Value)
				}
			}
		}
	}
	if len(c.Args) == 0 {
		// Bare assignment: VAR=value. Nothing to do for outputs.
		return st
	}
	name := cmdName(c)
	if name == "jq" {
		if info := w.commandInfo(c, s); info != nil {
			if f, ok := jqFilter(info); ok {
				w.a.JQ = append(w.a.JQ, JQCall{Filter: f, Line: line, Cmd: info})
			}
		}
	}
	if !hasTarget && ctx.redirect != nil {
		target, hasTarget = *ctx.redirect, true
	}
	// Any arg that references $GITHUB_ENV-written keys after the write.
	w.checkEnvUse(c, line)

	switch name {
	case "echo", "printf":
		lines, meta := w.render(c, name)
		if hasTarget {
			w.emit(&st, target, lines, meta, line, ctx)
			return st
		}
		// Not redirected: deprecated commands and stray K=V lines.
		for _, l := range lines {
			text := strings.TrimSpace(l.Text)
			for cmd, repl := range deprecatedCommands {
				if strings.HasPrefix(text, cmd) {
					w.a.Deprecated = append(w.a.Deprecated, Deprecation{Command: cmd, Replacement: repl, Line: line})
				}
			}
			if k, _, ok := splitKV(text); ok && !keyDynamic(k) && validKey(k) {
				w.a.Unredirected = append(w.a.Unredirected, KV{Key: k, Line: line})
			}
		}
		return st
	case "cat":
		// cat <<EOF ... EOF >> $GITHUB_OUTPUT (heredoc body), or cat file.
		if hasTarget {
			for _, r := range s.Redirs {
				if r.Hdoc != nil {
					var lines []LineWrite
					for _, l := range strings.Split(strings.TrimRight(w.wordText(r.Hdoc), "\n"), "\n") {
						lines = append(lines, LineWrite{Text: l, Line: int(r.Hdoc.Pos().Line()), Dynamic: w.dynamic(r.Hdoc), Opaque: w.opaqueWord(r.Hdoc)})
					}
					w.emit(&st, target, lines, lineMeta{}, line, ctx)
				}
			}
			if len(c.Args) > 1 {
				w.openAll(&st, "cat of a file into "+target.String())
			}
			return st
		}
		return st
	case "exec":
		if hasTarget {
			// exec >> $GITHUB_OUTPUT: subsequent bare echoes go there. We
			// model this by opening nothing and relying on ctx; a full
			// model would rewrite the remaining statements. Treat as open
			// so later references are accepted.
			w.openOne(&st, target, "exec redirect")
		}
		return st
	case "eval":
		w.openAll(&st, "eval")
		return st
	case "source", ".":
		w.openAll(&st, "sourced script")
		return st
	case "exit":
		st.exited = ctx.definite
		return st
	case "return":
		st.exited = false
		return st
	}
	if fn, ok := w.funcs[name]; ok {
		if ctx.inFunc[name] {
			return st
		}
		inner := ctx
		inner.inFunc = map[string]bool{name: true}
		for k := range ctx.inFunc {
			inner.inFunc[k] = true
		}
		inner.posArgs = nil
		for _, arg := range c.Args[1:] {
			inner.posArgs = append(inner.posArgs, literal(arg))
		}
		body := w.stmt(fn.Body, inner)
		body.exited = false
		return body
	}
	if name == "read" {
		var names []string
		for _, arg := range c.Args[1:] {
			if lit := literal(arg); lit != "" && !strings.HasPrefix(lit, "-") {
				names = append(names, lit)
			}
		}
		var hereString string
		literalInput := false
		for _, r := range s.Redirs {
			if r.Op == syntax.WordHdoc && r.Word != nil {
				hereString = literal(r.Word)
				literalInput = hereString != "" && !strings.Contains(hereString, Sentinel)
			}
		}
		words := strings.Fields(hereString)
		for i, n := range names {
			w.assigned[n] = true
			if literalInput && i < len(words) {
				v := words[i]
				if i == len(names)-1 {
					v = strings.Join(words[i:], " ")
				}
				w.a.Vars[n] = &ValueSource{Kind: ValueLiteral, Text: v}
			} else {
				w.mutated[n] = true
			}
		}
		return st
	}
	if name == "" {
		// A command invoked through a variable ($CMD ...): anything may run.
		w.openAll(&st, "command invoked through a variable")
		return st
	}
	if interpreters[name] {
		// Inline code, a heredoc, a module (-m) or a script file: the
		// interpreter may write anything (medium review D1).
		for _, arg := range c.Args[1:] {
			lit := literal(arg)
			switch {
			case lit == "-c" || lit == "-e" || lit == "-m":
				w.openAll(&st, name+" inline code or module")
				return st
			case lit == "" || !strings.HasPrefix(lit, "-"):
				w.openAll(&st, name+" running a script")
				return st
			}
		}
		for _, r := range s.Redirs {
			if r.Hdoc != nil || r.Op == syntax.RdrIn {
				w.openAll(&st, name+" heredoc or redirected script")
				return st
			}
		}
		if hasTarget {
			w.openOne(&st, target, name)
		}
		return st
	}
	if strings.HasPrefix(name, "./") || strings.HasSuffix(name, ".sh") || name == "bash" || name == "sh" {
		w.openAll(&st, "external script "+name)
		return st
	}
	// A package runner (uv run, npx, make, ...) executes the script or
	// task its arguments name (medium review 2 D8).
	firstWord := ""
	if len(c.Args) > 1 {
		firstWord = literal(c.Args[1])
	}
	if w.runners[name] || (firstWord != "" && w.runners[name+" "+firstWord]) {
		w.openAll(&st, "runner "+name)
		return st
	}
	// Any command handed a script (x.py, x.js, ...) or an interpreter
	// name runs it, whatever the command is, unless it is a tool that
	// only reads or moves files.
	if !fileTools[name] {
		for _, arg := range c.Args[1:] {
			lit := literal(arg)
			if lit == "" {
				continue
			}
			if interpreters[lit] {
				w.openAll(&st, name+" running "+lit)
				return st
			}
			for _, ext := range scriptExts {
				if strings.HasSuffix(lit, ext) {
					w.openAll(&st, name+" running a script")
					return st
				}
			}
		}
	}
	if hasTarget {
		// Some other command's stdout into the target: unknown keys.
		w.openOne(&st, target, name+" output")
	}
	return st
}

type lineMeta struct {
	escapedNewline bool
}

// emit records rendered lines into a target: key writes, line stream, and
// openness for dynamic keys.
func (w *walker) emit(st *pathState, t Target, lines []LineWrite, meta lineMeta, line int, ctx pathCtx) {
	if st.writes[t] == nil {
		st.writes[t] = map[string]*Write{}
	}
	for _, l := range lines {
		l.EscapedNewline = meta.escapedNewline
		st.lines[t] = append(st.lines[t], l)
		if t == Path {
			continue
		}
		text := l.Text
		k, v, ok := splitKV(text)
		if !ok {
			// A KEY<<DELIM opener or a stray line; the heredoc machine
			// handles openers at the end.
			if key, _, _, isOpener := heredocOpener(text); isOpener {
				if keyDynamic(key) {
					st.open[t] = "dynamic key"
					continue
				}
				st.writes[t][key] = &Write{Key: key, Definite: ctx.definite, Line: l.Line, Multiline: true}
			}
			continue
		}
		keys := []string{k}
		src := l.Source
		// A resolved dynamic key leaves a literal value literal.
		valueDynamic := l.Dynamic && (strings.ContainsAny(v, "$`") || strings.Contains(v, Sentinel))
		if keyDynamic(k) {
			resolved, ok := w.resolveKey(k, ctx)
			if !ok {
				st.open[t] = "dynamic key"
				continue
			}
			keys = resolved
			// The line's source describes the key's expansion, not the
			// value; rebuild it from the value text.
			src = nil
			if !valueDynamic {
				src = &ValueSource{Kind: ValueLiteral, Text: v}
			}
		}
		for _, key := range keys {
			definite := ctx.definite && len(keys) == 1
			wr := &Write{Key: key, Definite: definite, Line: l.Line, Value: v, ValueDynamic: valueDynamic, Source: src}
			if t == Env {
				if _, seen := w.envWrites[key]; !seen {
					w.envWrites[key] = l.Line
				}
			}
			st.writes[t][key] = wr
		}
	}
}

// resolveKey expands $NAME / ${NAME} in a key through loop variables with
// literal items, positional arguments bound at an inlined call, and
// static literal assignments (advanced §7.1). ok is false when any link
// is unresolved, a resolved value contains "=", or the cap is exceeded.
func (w *walker) resolveKey(k string, ctx pathCtx) ([]string, bool) {
	if strings.Contains(k, Sentinel) {
		return nil, false
	}
	cands := []string{""}
	i := 0
	for i < len(k) {
		if k[i] != '$' {
			for j := range cands {
				cands[j] += string(k[i])
			}
			i++
			continue
		}
		// $NAME, ${NAME}, $1
		j := i + 1
		braced := j < len(k) && k[j] == '{'
		if braced {
			j++
		}
		start := j
		for j < len(k) && (k[j] == '_' || (k[j] >= 'a' && k[j] <= 'z') || (k[j] >= 'A' && k[j] <= 'Z') || (k[j] >= '0' && k[j] <= '9')) {
			j++
		}
		name := k[start:j]
		if braced {
			if j >= len(k) || k[j] != '}' {
				return nil, false
			}
			j++
		}
		if name == "" {
			return nil, false
		}
		values, ok := w.varValues(name, ctx)
		if !ok {
			return nil, false
		}
		var next []string
		for _, c := range cands {
			for _, v := range values {
				if strings.Contains(v, "=") {
					return nil, false
				}
				next = append(next, c+v)
				if len(next) > KeyCandidateCap {
					return nil, false
				}
			}
		}
		cands = next
		i = j
	}
	for _, c := range cands {
		if !validKey(c) {
			return nil, false
		}
	}
	return cands, true
}

// varValues lists the literal values a variable can hold at this point.
func (w *walker) varValues(name string, ctx pathCtx) ([]string, bool) {
	if vals, ok := w.loopVars[name]; ok {
		return vals, true
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 1 && n <= len(ctx.posArgs) && ctx.posArgs[n-1] != "" {
		return []string{ctx.posArgs[n-1]}, true
	}
	if w.mutated[name] {
		return nil, false
	}
	if v, ok := w.a.Vars[name]; ok && v != nil && v.Kind == ValueLiteral && !strings.Contains(v.Text, Sentinel) {
		return []string{v.Text}, true
	}
	return nil, false
}

// literalItems renders a word list to literals, expanding braces.
func literalItems(words []*syntax.Word) ([]string, bool) {
	var out []string
	for _, wd := range words {
		syntax.SplitBraces(wd)
		for _, ex := range expand.Braces(wd) {
			lit := literal(ex)
			if lit == "" || strings.Contains(lit, Sentinel) {
				return nil, false
			}
			out = append(out, lit)
			if len(out) > KeyCandidateCap {
				return nil, false
			}
		}
	}
	return out, true
}

func (w *walker) openAll(st *pathState, reason string) {
	for _, t := range []Target{Output, Env, Path} {
		st.open[t] = reason
	}
}

func (w *walker) openOne(st *pathState, t Target, reason string) {
	st.open[t] = reason
}

// render turns an echo/printf call into the lines it prints.
func (w *walker) render(c *syntax.CallExpr, name string) ([]LineWrite, lineMeta) {
	line := int(c.Pos().Line())
	args := c.Args[1:]
	interpret := name == "printf"
	for len(args) > 0 {
		lit := literal(args[0])
		if name == "echo" && (lit == "-e" || lit == "-n" || lit == "-ne" || lit == "-en" || lit == "-E") {
			if strings.Contains(lit, "e") && !strings.Contains(lit, "E") {
				interpret = true
			}
			args = args[1:]
			continue
		}
		break
	}
	if len(args) == 0 {
		return nil, lineMeta{}
	}
	dynamic, opaque := false, false
	for _, a := range args {
		dynamic = dynamic || w.dynamic(a)
		opaque = opaque || w.opaqueWord(a)
	}
	var text string
	if name == "printf" {
		text = w.printf(args)
	} else {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = w.wordText(a)
		}
		text = strings.Join(parts, " ")
	}
	meta := lineMeta{}
	if interpret {
		text = strings.ReplaceAll(text, `\n`, "\n")
		text = strings.ReplaceAll(text, `\t`, "\t")
	} else if strings.Contains(text, `\n`) {
		meta.escapedNewline = true
	}
	var src *ValueSource
	switch {
	case name == "echo" && len(args) == 1:
		src = w.sourceOf(args[0], strings.IndexByte(w.wordText(args[0]), '=')+1)
	case name == "printf" && len(args) == 2 && strings.Count(w.wordText(args[0]), "%") == 1:
		src = w.sourceOf(args[1], 0)
	}
	var out []LineWrite
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		out = append(out, LineWrite{Text: l, Line: line, Dynamic: dynamic, Opaque: opaque, Source: src})
	}
	if len(out) != 1 {
		for i := range out {
			out[i].Source = nil
		}
	}
	return out, meta
}

var fmtSpec = regexp.MustCompile(`%[-+ #0]*\d*(?:\.\d+)?[sdifgxXobcq%]`)

// printf expands a format over its args, cycling as printf does.
func (w *walker) printf(args []*syntax.Word) string {
	format := w.wordText(args[0])
	format = strings.ReplaceAll(format, `\n`, "\n")
	rest := make([]string, 0, len(args)-1)
	for _, a := range args[1:] {
		rest = append(rest, w.wordText(a))
	}
	specs := fmtSpec.FindAllStringIndex(format, -1)
	nSpecs := 0
	for _, sp := range specs {
		if format[sp[0]:sp[1]] != "%%" {
			nSpecs++
		}
	}
	if nSpecs == 0 || len(rest) == 0 {
		return strings.ReplaceAll(format, "%%", "%")
	}
	var b strings.Builder
	i := 0
	for i < len(rest) || b.Len() == 0 {
		last := 0
		for _, sp := range specs {
			b.WriteString(format[last:sp[0]])
			spec := format[sp[0]:sp[1]]
			if spec == "%%" {
				b.WriteString("%")
			} else if i < len(rest) {
				b.WriteString(rest[i])
				i++
			}
			last = sp[1]
		}
		b.WriteString(format[last:])
		if i >= len(rest) {
			break
		}
	}
	return b.String()
}

// sglValue is a single-quoted word's text: verbatim for 'x', with the
// ANSI-C escapes expanded for $'x' (medium review 3 D10), where $'\n'
// is the usual way to put a real newline into one echo argument.
func sglValue(x *syntax.SglQuoted) string {
	if !x.Dollar {
		return x.Value
	}
	return ansiC(x.Value)
}

// ansiC expands the escapes bash honours in $'...'.
func ansiC(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case 'e', 'E':
			b.WriteByte(0x1b)
		case '\\', '\'', '"', '?':
			b.WriteByte(s[i])
		case 'x':
			n, w := hexDigits(s[i+1:], 2)
			if w == 0 {
				b.WriteString("\\x")
				continue
			}
			b.WriteByte(byte(n))
			i += w
		case 'u', 'U':
			max := 4
			if s[i] == 'U' {
				max = 8
			}
			n, w := hexDigits(s[i+1:], max)
			if w == 0 {
				b.WriteByte('\\')
				b.WriteByte(s[i])
				continue
			}
			b.WriteRune(rune(n))
			i += w
		case '0', '1', '2', '3', '4', '5', '6', '7':
			n, w := 0, 0
			for w < 3 && i+w < len(s) && s[i+w] >= '0' && s[i+w] <= '7' {
				n = n*8 + int(s[i+w]-'0')
				w++
			}
			b.WriteByte(byte(n))
			i += w - 1
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// hexDigits parses up to max hex digits; w is how many were consumed.
func hexDigits(s string, max int) (n, w int) {
	for w < max && w < len(s) {
		c := s[w]
		switch {
		case c >= '0' && c <= '9':
			n = n*16 + int(c-'0')
		case c >= 'a' && c <= 'f':
			n = n*16 + int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			n = n*16 + int(c-'A') + 10
		default:
			return n, w
		}
		w++
	}
	return n, w
}

// unescapeDQ resolves the escapes bash honours inside double quotes.
func unescapeDQ(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && strings.ContainsRune("\"\\$`\n", rune(s[i+1])) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// sourceOf classifies the part of a word after byte offset skip (the "k="
// prefix) as a literal, a single variable, a single command substitution,
// or a mix.
func (w *walker) sourceOf(wd *syntax.Word, skip int) *ValueSource {
	// Flatten double quotes, resolving their escapes.
	var parts []syntax.WordPart
	for _, p := range wd.Parts {
		if dq, ok := p.(*syntax.DblQuoted); ok {
			for _, q := range dq.Parts {
				if lit, ok := q.(*syntax.Lit); ok {
					parts = append(parts, &syntax.Lit{Value: unescapeDQ(lit.Value)})
					continue
				}
				parts = append(parts, q)
			}
		} else {
			parts = append(parts, p)
		}
	}
	// Drop the "k=" prefix from the leading literal.
	consumed := 0
	var rest []syntax.WordPart
	for _, p := range parts {
		if consumed >= skip {
			rest = append(rest, p)
			continue
		}
		lit := ""
		switch x := p.(type) {
		case *syntax.Lit:
			lit = x.Value
		case *syntax.SglQuoted:
			lit = sglValue(x)
		}
		if lit == "" {
			rest = append(rest, p)
			continue
		}
		need := skip - consumed
		if len(lit) > need {
			rest = append(rest, &syntax.Lit{Value: lit[need:]})
			consumed = skip
		} else {
			consumed += len(lit)
		}
	}
	var literal strings.Builder
	var expansions []syntax.WordPart
	for _, p := range rest {
		switch x := p.(type) {
		case *syntax.Lit:
			literal.WriteString(x.Value)
		case *syntax.SglQuoted:
			literal.WriteString(sglValue(x))
		default:
			expansions = append(expansions, p)
		}
	}
	text := literal.String()
	if strings.Contains(text, Sentinel) {
		return &ValueSource{Kind: ValueExpr, Text: text}
	}
	switch {
	case len(expansions) == 0:
		return &ValueSource{Kind: ValueLiteral, Text: text}
	case len(expansions) == 1 && strings.TrimSpace(text) == "":
		switch x := expansions[0].(type) {
		case *syntax.ParamExp:
			if x.Param != nil && x.Exp == nil && x.Index == nil && x.Slice == nil && x.Repl == nil && !x.Length {
				return &ValueSource{Kind: ValueVar, Text: x.Param.Value, Vars: []string{x.Param.Value}}
			}
			return &ValueSource{Kind: ValueMixed, Vars: paramNames(expansions)}
		case *syntax.CmdSubst:
			if info := w.substInfo(x); info != nil {
				return &ValueSource{Kind: ValueCommand, Cmd: info, Vars: paramNames(expansions)}
			}
		}
	}
	return &ValueSource{Kind: ValueMixed, Vars: paramNames(expansions)}
}

// paramNames lists the parameters expanded anywhere inside word parts.
func paramNames(parts []syntax.WordPart) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range parts {
		syntax.Walk(p, func(n syntax.Node) bool {
			if pe, ok := n.(*syntax.ParamExp); ok && pe.Param != nil && !seen[pe.Param.Value] {
				seen[pe.Param.Value] = true
				out = append(out, pe.Param.Value)
			}
			return true
		})
	}
	return out
}

// substInfo describes the last command of a $( ... ) substitution with its
// pipeline input.
func (w *walker) substInfo(cs *syntax.CmdSubst) *CommandInfo {
	if len(cs.Stmts) != 1 {
		return nil
	}
	return w.stmtInfo(cs.Stmts[0])
}

func (w *walker) stmtInfo(s *syntax.Stmt) *CommandInfo {
	switch c := s.Cmd.(type) {
	case *syntax.CallExpr:
		return w.commandInfo(c, s)
	case *syntax.BinaryCmd:
		if c.Op == syntax.Pipe || c.Op == syntax.PipeAll {
			right := w.stmtInfo(c.Y)
			if right != nil {
				right.Input = w.stmtInfo(c.X)
			}
			return right
		}
	}
	return nil
}

func (w *walker) commandInfo(c *syntax.CallExpr, s *syntax.Stmt) *CommandInfo {
	if len(c.Args) == 0 {
		return nil
	}
	info := &CommandInfo{Name: cmdName(c), Line: int(c.Pos().Line())}
	for _, a := range c.Args[1:] {
		lit := literal(a)
		info.Args = append(info.Args, CommandArg{Text: w.wordText(a), Literal: lit != "" || w.wordText(a) == ""})
		if lit != "" {
			info.Args[len(info.Args)-1].Text = lit
		}
	}
	for _, r := range s.Redirs {
		if r.Op == syntax.RdrIn {
			info.InFile = literal(r.Word)
		}
		if r.Hdoc != nil && !w.dynamic(r.Hdoc) {
			info.Heredoc = w.wordText(r.Hdoc)
		}
	}
	return info
}

// jqFilter finds the literal filter argument of a jq command: the first
// positional after flags that take no value.
func jqFilter(info *CommandInfo) (string, bool) {
	valueFlags := map[string]bool{"--arg": true, "--argjson": true, "--slurpfile": true, "--rawfile": true, "--args": true, "--jsonargs": true, "-f": true, "--from-file": true, "--indent": true, "-L": true}
	skip := 0
	for _, a := range info.Args {
		if skip > 0 {
			skip--
			continue
		}
		if strings.HasPrefix(a.Text, "-") && a.Literal {
			if valueFlags[a.Text] {
				skip = 2
				if a.Text == "-f" || a.Text == "--from-file" || a.Text == "--indent" || a.Text == "-L" {
					skip = 1
				}
			}
			continue
		}
		if !a.Literal {
			return "", false
		}
		return a.Text, true
	}
	return "", false
}

// --- word helpers ------------------------------------------------------------

// wordText renders a word as text: literals verbatim, quotes removed,
// expansions as $NAME / $(...) so dynamic keys stay detectable.
func (w *walker) wordText(wd *syntax.Word) string {
	var b strings.Builder
	for _, p := range wd.Parts {
		w.partText(&b, p)
	}
	return b.String()
}

func (w *walker) partText(b *strings.Builder, p syntax.WordPart) {
	switch x := p.(type) {
	case *syntax.Lit:
		b.WriteString(x.Value)
	case *syntax.SglQuoted:
		b.WriteString(sglValue(x))
	case *syntax.DblQuoted:
		for _, q := range x.Parts {
			if lit, ok := q.(*syntax.Lit); ok {
				b.WriteString(unescapeDQ(lit.Value))
				continue
			}
			w.partText(b, q)
		}
	case *syntax.ParamExp:
		b.WriteString("$" + x.Param.Value)
	case *syntax.CmdSubst:
		b.WriteString("$(" + w.cmdSubstName(x) + ")")
	case *syntax.ArithmExp:
		b.WriteString("$((...))")
	case *syntax.ProcSubst:
		b.WriteString("<(...)")
	}
}

func (w *walker) cmdSubstName(x *syntax.CmdSubst) string {
	if len(x.Stmts) == 0 {
		return ""
	}
	if call, ok := x.Stmts[0].Cmd.(*syntax.CallExpr); ok {
		return cmdName(call)
	}
	return "..."
}

// dynamic reports whether a word contains any expansion or the sentinel.
func (w *walker) dynamic(wd *syntax.Word) bool {
	found := false
	syntax.Walk(wd, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
			found = true
		case *syntax.Lit:
			if strings.Contains(x.Value, Sentinel) {
				found = true
			}
		case *syntax.SglQuoted:
			if strings.Contains(sglValue(x), Sentinel) {
				found = true
			}
		}
		return !found
	})
	return found
}

// opaqueWord reports whether a word contains a command substitution of an
// opaque command.
func (w *walker) opaqueWord(wd *syntax.Word) bool {
	found := false
	syntax.Walk(wd, func(n syntax.Node) bool {
		if cs, ok := n.(*syntax.CmdSubst); ok {
			name := w.cmdSubstName(cs)
			if w.opaque[name] || strings.Contains(name, "/") {
				found = true
			}
		}
		return !found
	})
	return found
}

func keyDynamic(k string) bool {
	return strings.ContainsAny(k, "$") || strings.Contains(k, Sentinel)
}

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func validKey(k string) bool {
	return keyRe.MatchString(k)
}

// splitKV splits "k=v" at the first '=' not part of a "<<" opener.
func splitKV(text string) (k, v string, ok bool) {
	eq := strings.IndexByte(text, '=')
	if eq < 0 {
		return "", "", false
	}
	k = text[:eq]
	if strings.Contains(k, "<<") || strings.ContainsAny(k, " \t") {
		return "", "", false
	}
	return k, text[eq+1:], true
}

var openerRe = regexp.MustCompile(`^([^\s=<]+)<<(-?)(.*)$`)

// heredocOpener parses a KEY<<DELIM line.
func heredocOpener(text string) (key, delim string, indented, ok bool) {
	m := openerRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", "", false, false
	}
	return m[1], strings.TrimSpace(m[3]), m[2] == "-", true
}

// literal returns a word's text when it is purely literal, else "".
func literal(wd *syntax.Word) string {
	if wd == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range wd.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(sglValue(x))
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				l, ok := q.(*syntax.Lit)
				if !ok {
					return ""
				}
				b.WriteString(l.Value)
			}
		default:
			return ""
		}
	}
	return b.String()
}

func cmdName(c *syntax.CallExpr) string {
	if len(c.Args) == 0 {
		return ""
	}
	return literal(c.Args[0])
}

// targetWord reports whether a word is exactly $GITHUB_OUTPUT & co.
func targetWord(wd *syntax.Word) (Target, bool) {
	if wd == nil {
		return 0, false
	}
	parts := wd.Parts
	if len(parts) == 1 {
		if dq, ok := parts[0].(*syntax.DblQuoted); ok {
			parts = dq.Parts
		}
	}
	if len(parts) != 1 {
		return 0, false
	}
	pe, ok := parts[0].(*syntax.ParamExp)
	if !ok || pe.Param == nil {
		return 0, false
	}
	t, ok := targetVars[pe.Param.Value]
	return t, ok
}

// stdoutTarget finds a > or >> redirect of stdout to a target.
func stdoutTarget(redirs []*syntax.Redirect) (Target, bool) {
	for _, r := range redirs {
		if r.Op != syntax.AppOut && r.Op != syntax.RdrOut && r.Op != syntax.RdrAll && r.Op != syntax.AppAll {
			continue
		}
		if r.N != nil && r.N.Value != "1" {
			continue
		}
		if t, ok := targetWord(r.Word); ok {
			return t, true
		}
	}
	return 0, false
}

// checkReads records reads of $GITHUB_OUTPUT (§2.3).
func (w *walker) checkReads(s *syntax.Stmt) {
	line := int(s.Pos().Line())
	for _, r := range s.Redirs {
		if r.Op == syntax.RdrIn || r.Op == syntax.WordHdoc {
			if t, ok := targetWord(r.Word); ok && t == Output {
				w.a.Reads = append(w.a.Reads, Read{Line: line, How: "input redirect"})
			}
		}
	}
	switch c := s.Cmd.(type) {
	case *syntax.CallExpr:
		name := cmdName(c)
		if readers[name] {
			for _, arg := range c.Args[1:] {
				if t, ok := targetWord(arg); ok && t == Output {
					w.a.Reads = append(w.a.Reads, Read{Line: line, How: name})
				}
			}
		}
	case *syntax.TestClause:
		syntax.Walk(c, func(n syntax.Node) bool {
			if wd, ok := n.(*syntax.Word); ok {
				if t, ok := targetWord(wd); ok && t == Output {
					w.a.Reads = append(w.a.Reads, Read{Line: line, How: "[[ ]]"})
				}
			}
			return true
		})
	}
}

// checkEnvUse records $K references after K was written to $GITHUB_ENV.
func (w *walker) checkEnvUse(c *syntax.CallExpr, line int) {
	if len(w.envWrites) == 0 {
		return
	}
	syntax.Walk(c, func(n syntax.Node) bool {
		if pe, ok := n.(*syntax.ParamExp); ok && pe.Param != nil {
			if w.assigned[pe.Param.Value] {
				return true
			}
			if wl, ok := w.envWrites[pe.Param.Value]; ok && wl < line {
				w.a.EnvSameStep = append(w.a.EnvSameStep, EnvUse{Key: pe.Param.Value, WriteLine: wl, RefLine: line})
			}
		}
		return true
	})
}

// finishHeredocs runs the KEY<<DELIM state machine over a target's line
// stream (§2.5), marking value lines and recording openers.
func (w *walker) finishHeredocs(ts *TargetState) {
	var open *Heredoc
	for i := range ts.Lines {
		l := &ts.Lines[i]
		if open != nil {
			if strings.TrimSpace(l.Text) == open.Delim && open.Delim != "" {
				open.Terminated = true
				l.InHeredoc = true
				if wr, ok := ts.Writes[open.Key]; ok {
					wr.Multiline = true
				}
				open = nil
				continue
			}
			l.InHeredoc = true
			if l.Opaque {
				open.OpaqueLines++
			}
			if wr, ok := ts.Writes[open.Key]; ok {
				wr.Lines = append(wr.Lines, l.Text)
			}
			continue
		}
		if l.Dynamic && !strings.Contains(l.Text, "<<") {
			continue
		}
		if key, delim, indented, ok := heredocOpener(l.Text); ok && !keyDynamic(key) {
			h := Heredoc{Key: key, Delim: delim, Line: l.Line, Indented: indented, EmptyDelim: delim == "", EscapedNewline: l.EscapedNewline}
			ts.Heredocs = append(ts.Heredocs, h)
			if delim == "" {
				continue
			}
			open = &ts.Heredocs[len(ts.Heredocs)-1]
		}
	}
	if open != nil {
		open.Terminated = false
	}
}

// NewSynthetic builds an analysis for a non-bash step from modelled
// $GITHUB_OUTPUT writes (advanced §5.4): the given keys are definite
// writes, and open marks the target open with the reason.
func NewSynthetic(writes map[string]string, open bool, reason string) *Analysis {
	a := &Analysis{Targets: map[Target]*TargetState{}, Vars: map[string]*ValueSource{}}
	for _, t := range []Target{Output, Env, Path} {
		a.Targets[t] = &TargetState{Writes: map[string]*Write{}}
	}
	out := a.Targets[Output]
	for k, v := range writes {
		out.Writes[k] = &Write{Key: k, Definite: true, Line: 1, Value: v, Source: &ValueSource{Kind: ValueLiteral, Text: v}}
	}
	if open {
		out.Open, out.OpenReason = true, reason
	}
	return a
}
