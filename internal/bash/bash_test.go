package bash

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func keys(a *Analysis, t Target) (definite, conditional []string) {
	for k, w := range a.Target(t).Writes {
		if w.Definite {
			definite = append(definite, k)
		} else {
			conditional = append(conditional, k)
		}
	}
	sort.Strings(definite)
	sort.Strings(conditional)
	return
}

func TestWriteForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		{"echo quoted", `echo "k=v" >> $GITHUB_OUTPUT`, []string{"k"}},
		{"echo bare", `echo k=v >> "$GITHUB_OUTPUT"`, []string{"k"}},
		{"echo braces", `echo k=v >> ${GITHUB_OUTPUT}`, []string{"k"}},
		{"echo single redirect", `echo k=v > $GITHUB_OUTPUT`, []string{"k"}},
		{"echo -e two lines", `echo -e "a=1\nb=2" >> $GITHUB_OUTPUT`, []string{"a", "b"}},
		{"printf kv", `printf 'k=%s\n' v >> $GITHUB_OUTPUT`, []string{"k"}},
		{"printf cycling", `printf '%s=%s\n' a 1 b 2 >> $GITHUB_OUTPUT`, []string{"a", "b"}},
		{"cat heredoc", "cat <<EOF >> $GITHUB_OUTPUT\na=1\nb=2\nEOF\n", []string{"a", "b"}},
		{"multiline value", "echo 'k<<DELIM' >> $GITHUB_OUTPUT\necho body >> $GITHUB_OUTPUT\necho DELIM >> $GITHUB_OUTPUT\n", []string{"k"}},
		{"group", `{ echo a=1; echo b=2; } >> $GITHUB_OUTPUT`, []string{"a", "b"}},
		{"subshell", `( echo a=1; printf 'b=%s\n' 2 ) >> $GITHUB_OUTPUT`, []string{"a", "b"}},
		{"tee", `echo a=1 | tee -a $GITHUB_OUTPUT`, []string{"a"}},
		{"tee no append", `printf 'a=%s\n' 1 | tee "$GITHUB_OUTPUT"`, []string{"a"}},
		{"dynamic value is still a known key", `echo "k=$(git rev-parse HEAD)" >> $GITHUB_OUTPUT`, []string{"k"}},
		{"expression value", `echo "k=` + Sentinel + `" >> $GITHUB_OUTPUT`, []string{"k"}},
		{"function called", "emit() { echo a=1 >> $GITHUB_OUTPUT; }\nemit\n", []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := Analyse(tc.script, false, nil)
			if a.ParseErr != nil {
				t.Fatal(a.ParseErr)
			}
			def, cond := keys(a, Output)
			if !reflect.DeepEqual(def, tc.want) || len(cond) != 0 {
				t.Fatalf("definite %v conditional %v want %v", def, cond, tc.want)
			}
			if a.Target(Output).Open {
				t.Fatalf("target unexpectedly open: %s", a.Target(Output).OpenReason)
			}
		})
	}
}

func TestPathSensitivity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		script   string
		def      []string
		cond     []string
		notWrite []string
	}{
		{"if without else", "if true; then echo a=1 >> $GITHUB_OUTPUT; fi\n", nil, []string{"a"}, nil},
		{"if else both", "if true; then echo a=1 >> $GITHUB_OUTPUT; else echo a=2 >> $GITHUB_OUTPUT; fi\n", []string{"a"}, nil, nil},
		{"if else different", "if true; then echo a=1 >> $GITHUB_OUTPUT; else echo b=2 >> $GITHUB_OUTPUT; fi\n", nil, []string{"a", "b"}, nil},
		{"elif chain", "if x; then echo a=1 >> $GITHUB_OUTPUT; elif y; then echo a=1 >> $GITHUB_OUTPUT; else echo a=1 >> $GITHUB_OUTPUT; fi\n", []string{"a"}, nil, nil},
		{"case with default", "case $x in a) echo k=1 >> $GITHUB_OUTPUT;; *) echo k=2 >> $GITHUB_OUTPUT;; esac\n", []string{"k"}, nil, nil},
		{"case no default", "case $x in a) echo k=1 >> $GITHUB_OUTPUT;; esac\n", nil, []string{"k"}, nil},
		{"and conditional", "test -f x && echo a=1 >> $GITHUB_OUTPUT\n", nil, []string{"a"}, nil},
		{"or conditional", "test -f x || echo a=1 >> $GITHUB_OUTPUT\n", nil, []string{"a"}, nil},
		{"loop conditional", "for f in *; do echo a=1 >> $GITHUB_OUTPUT; done\n", nil, []string{"a"}, nil},
		{"after exit dropped", "echo a=1 >> $GITHUB_OUTPUT\nexit 0\necho b=2 >> $GITHUB_OUTPUT\n", []string{"a"}, nil, []string{"b"}},
		{"after conditional exit kept", "echo a=1 >> $GITHUB_OUTPUT\nif x; then exit 1; fi\necho b=2 >> $GITHUB_OUTPUT\n", []string{"a", "b"}, nil, nil},
		{"definite then conditional stays definite", "echo a=1 >> $GITHUB_OUTPUT\nif x; then echo a=2 >> $GITHUB_OUTPUT; fi\n", []string{"a"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := Analyse(tc.script, false, nil)
			if a.ParseErr != nil {
				t.Fatal(a.ParseErr)
			}
			def, cond := keys(a, Output)
			if !reflect.DeepEqual(def, tc.def) || !reflect.DeepEqual(cond, tc.cond) {
				t.Fatalf("definite %v conditional %v; want %v / %v", def, cond, tc.def, tc.cond)
			}
			for _, k := range tc.notWrite {
				if _, ok := a.Target(Output).Writes[k]; ok {
					t.Fatalf("%s should not be written", k)
				}
			}
		})
	}
}

func TestOpacity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		script string
		open   []Target
		known  []string
	}{
		{"dynamic key", `echo "$k=v" >> $GITHUB_OUTPUT`, []Target{Output}, nil},
		{"expression key", `echo "` + Sentinel + `=v" >> $GITHUB_OUTPUT`, []Target{Output}, nil},
		{"dynamic key keeps known", "echo a=1 >> $GITHUB_OUTPUT\necho \"$k=v\" >> $GITHUB_OUTPUT\n", []Target{Output}, []string{"a"}},
		{"external script", "echo a=1 >> $GITHUB_OUTPUT\n./scripts/foo.sh\n", []Target{Output, Env, Path}, []string{"a"}},
		{"bash script", "bash foo.sh\n", []Target{Output, Env, Path}, nil},
		{"source", "source x.sh\n", []Target{Output, Env, Path}, nil},
		{"python -c", `python -c "print(1)"`, []Target{Output, Env, Path}, nil},
		{"python heredoc", "python <<EOF\nprint(1)\nEOF\n", []Target{Output, Env, Path}, nil},
		{"node -e", `node -e "1"`, []Target{Output, Env, Path}, nil},
		{"eval", `eval "$cmd"`, []Target{Output, Env, Path}, nil},
		{"uncalled function contributes nothing", "f() { echo a=1 >> $GITHUB_OUTPUT; }\n", nil, nil},
		{"function via variable", "f() { echo a=1 >> $GITHUB_OUTPUT; }\n$fn\n", []Target{Output, Env, Path}, nil},
		{"other command into target", `date >> $GITHUB_OUTPUT`, []Target{Output}, nil},
		{"python running a script opens everything", `python script.py`, []Target{Output, Env, Path}, nil},
		{"python module", `python -m tool.run --flag`, []Target{Output, Env, Path}, nil},
		{"python with only flags is closed", `python --version`, nil, nil},
		{"uv run with a script", `uv run --no-project --with pyyaml python .github/scripts/ci/affected.py --github-output`, []Target{Output, Env, Path}, nil},
		{"uv run script directly", `uv run tests/tavern-e2e/ci/report.py`, []Target{Output, Env, Path}, nil},
		{"npx tool", `npx tsx scripts/emit.ts`, []Target{Output, Env, Path}, nil},
		{"make target", `make outputs`, []Target{Output, Env, Path}, nil},
		{"unknown runner with a script argument", `mytool emit.py --flag`, []Target{Output, Env, Path}, nil},
		{"unknown runner with an interpreter argument", `hyperfine python bench.rs`, []Target{Output, Env, Path}, nil},
		{"file tool with a script argument stays closed", "git add emit.py\ncat run.sh\n", nil, nil},
		{"uv without run is closed", `uv pip install -r requirements.txt`, nil, nil},
		{"loop variable keys resolve (conditional)", "for k in a b c; do echo \"run-$k=1\" >> $GITHUB_OUTPUT; done\n", nil, nil},
		{"brace expansion keys resolve (conditional)", "for i in {1..3}; do echo \"n$i=x\" >> $GITHUB_OUTPUT; done\n", nil, nil},
		{"static assignment keys resolve", "K=version\necho \"$K=1\" >> $GITHUB_OUTPUT\n", nil, []string{"version"}},
		{"reassigned variable opens", "K=version\nK=$(date)\necho \"$K=1\" >> $GITHUB_OUTPUT\n", []Target{Output}, nil},
		{"read here-string binds", "read -r K V <<< \"name value\"\necho \"$K=$V\" >> $GITHUB_OUTPUT\n", nil, []string{"name"}},
		{"function positional args bind", "emit() { echo \"$1=1\" >> $GITHUB_OUTPUT; }\nemit alpha\n", nil, []string{"alpha"}},
		{"ANSI-C newline builds a heredoc opener", "echo \"k<<EOF\"$'\\n'\"${V}\"$'\\n'EOF >> \"$GITHUB_OUTPUT\"\n", nil, []string{"k"}},
		{"command through a variable", "echo a=1 >> $GITHUB_OUTPUT\n$CMD --flag\n", []Target{Output, Env, Path}, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := Analyse(tc.script, false, nil)
			if a.ParseErr != nil {
				t.Fatal(a.ParseErr)
			}
			for _, tg := range []Target{Output, Env, Path} {
				wantOpen := false
				for _, o := range tc.open {
					wantOpen = wantOpen || o == tg
				}
				if a.Target(tg).Open != wantOpen {
					t.Errorf("%s open=%v want %v (%s)", tg, a.Target(tg).Open, wantOpen, a.Target(tg).OpenReason)
				}
			}
			def, _ := keys(a, Output)
			if !reflect.DeepEqual(def, tc.known) {
				t.Errorf("known keys %v want %v", def, tc.known)
			}
		})
	}
}

func TestLineStreams(t *testing.T) {
	t.Parallel()
	a := Analyse("echo /usr/local/bin >> $GITHUB_PATH\necho FOO=bar >> $GITHUB_PATH\necho BAR=1 >> $GITHUB_ENV\necho bare >> $GITHUB_ENV\necho \"X=$v\" >> $GITHUB_ENV\n", false, nil)
	path := a.Target(Path).Lines
	if len(path) != 2 || path[0].Text != "/usr/local/bin" || path[1].Text != "FOO=bar" {
		t.Fatalf("path lines = %+v", path)
	}
	env := a.Target(Env).Lines
	if len(env) != 3 || env[1].Text != "bare" || !env[2].Dynamic {
		t.Fatalf("env lines = %+v", env)
	}
	if _, ok := a.Target(Path).Writes["FOO"]; ok {
		t.Fatal("PATH lines are never key writes")
	}
}

func TestHeredocMachine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		script string
		check  func(t *testing.T, h []Heredoc, ts *TargetState)
	}{
		{"terminated", "echo 'k<<EOF' >> $GITHUB_OUTPUT\necho line >> $GITHUB_OUTPUT\necho EOF >> $GITHUB_OUTPUT\n", func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || !h[0].Terminated || h[0].Key != "k" || h[0].Delim != "EOF" {
				t.Fatalf("%+v", h)
			}
			if w := ts.Writes["k"]; w == nil || !w.Multiline || !reflect.DeepEqual(w.Lines, []string{"line"}) {
				t.Fatalf("write = %+v", w)
			}
			// Body and closer are part of the value block; the opener is not.
			if ts.Lines[0].InHeredoc || !ts.Lines[1].InHeredoc || !ts.Lines[2].InHeredoc {
				t.Fatalf("lines = %+v", ts.Lines)
			}
		}},
		{"unterminated", "echo 'k<<EOF' >> $GITHUB_OUTPUT\necho line >> $GITHUB_OUTPUT\n", func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || h[0].Terminated {
				t.Fatalf("%+v", h)
			}
		}},
		{"empty delim", "echo 'k<<' >> $GITHUB_OUTPUT\n", func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || !h[0].EmptyDelim {
				t.Fatalf("%+v", h)
			}
		}},
		{"indented form", "echo 'k<<-EOF' >> $GITHUB_OUTPUT\necho EOF >> $GITHUB_OUTPUT\n", func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || !h[0].Indented || !h[0].Terminated {
				t.Fatalf("%+v", h)
			}
		}},
		{"escaped newline without -e", `echo "k<<EOF\nv\nEOF" >> $GITHUB_OUTPUT`, func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || !h[0].EscapedNewline || h[0].Terminated {
				t.Fatalf("%+v", h)
			}
		}},
		{"escaped newline with -e", `echo -e "k<<EOF\nv\nEOF" >> $GITHUB_OUTPUT`, func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || h[0].EscapedNewline || !h[0].Terminated {
				t.Fatalf("%+v", h)
			}
		}},
		{"opaque collision", "echo 'k<<EOF' >> $GITHUB_OUTPUT\necho \"$(git log)\" >> $GITHUB_OUTPUT\necho EOF >> $GITHUB_OUTPUT\n", func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || h[0].OpaqueLines != 1 {
				t.Fatalf("%+v", h)
			}
		}},
		{"cat heredoc with multiline value", "cat <<EOF >> $GITHUB_ENV\nk<<DELIM\nline\nDELIM\nEOF\n", func(t *testing.T, h []Heredoc, ts *TargetState) {
			if len(h) != 1 || !h[0].Terminated || h[0].Delim != "DELIM" {
				t.Fatalf("%+v", h)
			}
			if !ts.Lines[1].InHeredoc {
				t.Fatalf("lines = %+v", ts.Lines)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := Analyse(tc.script, false, nil)
			if a.ParseErr != nil {
				t.Fatal(a.ParseErr)
			}
			target := Output
			if strings.Contains(tc.script, "GITHUB_ENV") {
				target = Env
			}
			tc.check(t, a.Target(target).Heredocs, a.Target(target))
		})
	}
}

func TestReads(t *testing.T) {
	t.Parallel()
	for script, want := range map[string]int{
		`cat $GITHUB_OUTPUT`:                        1,
		`grep foo "$GITHUB_OUTPUT"`:                 1,
		`head -1 ${GITHUB_OUTPUT}`:                  1,
		`while read l; do :; done < $GITHUB_OUTPUT`: 1,
		`cat <<< $GITHUB_OUTPUT`:                    1,
		`source $GITHUB_OUTPUT`:                     1,
		`[ -s $GITHUB_OUTPUT ] && echo yes`:         1,
		`[[ -s $GITHUB_OUTPUT ]] && echo yes`:       1,
		`echo "see $GITHUB_OUTPUT"`:                 0,
		`echo "k=v" >> $GITHUB_OUTPUT`:              0,
		`ls -l $GITHUB_OUTPUT`:                      0,
	} {
		a := Analyse(script, false, nil)
		if a.ParseErr != nil {
			t.Fatalf("%s: %v", script, a.ParseErr)
		}
		if got := len(a.Reads); got != want {
			t.Errorf("%s: %d reads want %d (%+v)", script, got, want, a.Reads)
		}
	}
}

func TestDeprecatedAndUnredirected(t *testing.T) {
	t.Parallel()
	a := Analyse("echo \"::set-output name=x::1\"\nprintf '::add-path::/x\\n'\necho \"::set-output name=k::v\" >> $GITHUB_OUTPUT\necho \"k=v\"\necho \"$dyn=v\"\n", false, nil)
	if a.ParseErr != nil {
		t.Fatal(a.ParseErr)
	}
	if len(a.Deprecated) != 2 || a.Deprecated[0].Command != "::set-output" || a.Deprecated[0].Replacement != "$GITHUB_OUTPUT" || a.Deprecated[1].Command != "::add-path" {
		t.Fatalf("deprecated = %+v", a.Deprecated)
	}
	if len(a.Unredirected) != 1 || a.Unredirected[0].Key != "k" {
		t.Fatalf("unredirected = %+v", a.Unredirected)
	}
}

func TestEnvSameStep(t *testing.T) {
	t.Parallel()
	a := Analyse("echo \"FOO=1\" >> $GITHUB_ENV\necho \"$FOO\"\n", false, nil)
	if len(a.EnvSameStep) != 1 || a.EnvSameStep[0].Key != "FOO" || a.EnvSameStep[0].RefLine != 2 {
		t.Fatalf("%+v", a.EnvSameStep)
	}
	a = Analyse("echo \"$FOO\"\necho \"FOO=1\" >> $GITHUB_ENV\n", false, nil)
	if len(a.EnvSameStep) != 0 {
		t.Fatalf("reference before write is fine: %+v", a.EnvSameStep)
	}
}

func TestParseErrorAndPosix(t *testing.T) {
	t.Parallel()
	a := Analyse("if true; then\necho a=1 >> $GITHUB_OUTPUT\n", false, nil)
	if a.ParseErr == nil || a.ParseErr.Line == 0 {
		t.Fatalf("expected a positioned parse error, got %+v", a.ParseErr)
	}
	if len(a.Target(Output).Writes) != 0 {
		t.Fatal("no writes after a parse error")
	}
	if a := Analyse("[ -f x ] && echo a=1 >> $GITHUB_OUTPUT\n", true, nil); a.ParseErr != nil {
		t.Fatal(a.ParseErr)
	}
}

func TestReplaceExpressions(t *testing.T) {
	t.Parallel()
	got := ReplaceExpressions("echo ${{ inputs.a }} and ${{ b }} and ${{ unterminated")
	if strings.Count(got, Sentinel) != 3 || strings.Contains(got, "${{") {
		t.Fatalf("got %q", got)
	}
}

func TestValueCapture(t *testing.T) {
	t.Parallel()
	a := Analyse("echo 'k={\"a\":1}' >> $GITHUB_OUTPUT\necho \"d=$(date)\" >> $GITHUB_OUTPUT\n", false, nil)
	if w := a.Target(Output).Writes["k"]; w == nil || w.Value != `{"a":1}` || w.ValueDynamic {
		t.Fatalf("k = %+v", w)
	}
	if w := a.Target(Output).Writes["d"]; w == nil || !w.ValueDynamic || w.Value != "$(date)" {
		t.Fatalf("d = %+v", w)
	}
}

func TestValueSources(t *testing.T) {
	t.Parallel()
	a := Analyse(`CFG=$(cat config.json)
RAW='{"a":1}'
echo "lit={\"x\":1}" >> $GITHUB_OUTPUT
echo "var=$CFG" >> $GITHUB_OUTPUT
echo "cmd=$(jq -r '.version' config.json)" >> $GITHUB_OUTPUT
echo "pipe=$(cat config.json | jq '.items')" >> $GITHUB_OUTPUT
echo "mix=a-$CFG" >> $GITHUB_OUTPUT
printf 'pf=%s\n' "$RAW" >> $GITHUB_OUTPUT
echo "ex=`+Sentinel+`" >> $GITHUB_OUTPUT
jq -n '{a: 1' > out.json
`, false, nil)
	if a.ParseErr != nil {
		t.Fatal(a.ParseErr)
	}
	w := a.Target(Output).Writes
	if s := w["lit"].Source; s == nil || s.Kind != ValueLiteral || s.Text != `{"x":1}` {
		t.Errorf("lit = %+v", s)
	}
	if s := w["var"].Source; s == nil || s.Kind != ValueVar || s.Text != "CFG" {
		t.Errorf("var = %+v", s)
	}
	if s := w["cmd"].Source; s == nil || s.Kind != ValueCommand || s.Cmd.Name != "jq" || s.Cmd.Args[0].Text != "-r" || s.Cmd.Args[1].Text != ".version" || !s.Cmd.Args[1].Literal || s.Cmd.Args[2].Text != "config.json" {
		t.Errorf("cmd = %+v", s)
	}
	if s := w["pipe"].Source; s == nil || s.Kind != ValueCommand || s.Cmd.Name != "jq" || s.Cmd.Input == nil || s.Cmd.Input.Name != "cat" || s.Cmd.Input.Args[0].Text != "config.json" {
		t.Errorf("pipe = %+v", s)
	}
	if s := w["mix"].Source; s == nil || s.Kind != ValueMixed {
		t.Errorf("mix = %+v", s)
	}
	if s := w["pf"].Source; s == nil || s.Kind != ValueVar || s.Text != "RAW" {
		t.Errorf("pf = %+v", s)
	}
	if s := w["ex"].Source; s == nil || s.Kind != ValueExpr {
		t.Errorf("ex = %+v", s)
	}
	if v := a.Vars["CFG"]; v == nil || v.Kind != ValueCommand || v.Cmd.Name != "cat" {
		t.Errorf("CFG var = %+v", v)
	}
	if v := a.Vars["RAW"]; v == nil || v.Kind != ValueLiteral || v.Text != `{"a":1}` {
		t.Errorf("RAW var = %+v", v)
	}
	var filters []string
	for _, c := range a.JQ {
		filters = append(filters, c.Filter)
	}
	if !reflect.DeepEqual(filters, []string{"{a: 1"}) {
		t.Errorf("jq calls = %v (only top-level invocations are listed; substitutions are reached through sources)", filters)
	}
}

func TestAnsiC(t *testing.T) {
	for in, want := range map[string]string{
		`a\nb`:     "a\nb",
		`t\tx`:     "t\tx",
		`\x41\x42`: "AB",
		`\\`:       `\`,
		`\'q`:      "'q",
		`\u00e9`:   "é",
		`\101`:     "A",
		`\q`:       `\q`,
		`plain`:    "plain",
	} {
		if got := ansiC(in); got != want {
			t.Errorf("ansiC(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSeedsResolveKeys is advanced §7.1 cross-step variables: a seeded
// variable (an earlier step's $GITHUB_ENV write) resolves a key.
func TestSeedsResolveKeys(t *testing.T) {
	a := AnalyseWith("echo \"$NAME=1\" >> $GITHUB_OUTPUT\n", false, Options{Seeds: map[string]string{"NAME": "mode"}})
	ts := a.Target(Output)
	if ts.Open || ts.Writes["mode"] == nil {
		t.Fatalf("open=%v writes=%v", ts.Open, ts.Keys())
	}
}

// TestLoopKeysConditional is advanced §7.1: keys resolved through a loop
// are written on every iteration path but a loop may run zero times, so
// they are conditional, with literal values kept literal.
func TestLoopKeysConditional(t *testing.T) {
	for script, want := range map[string][]string{
		"for k in a b c; do echo \"run-$k=1\" >> $GITHUB_OUTPUT; done\n": {"run-a", "run-b", "run-c"},
		"for i in {1..3}; do echo \"n$i=x\" >> $GITHUB_OUTPUT; done\n":   {"n1", "n2", "n3"},
		"for i in {a,b}; do echo \"$i=x\" >> $GITHUB_OUTPUT; done\n":     {"a", "b"},
	} {
		a := Analyse(script, false, nil)
		ts := a.Target(Output)
		if ts.Open {
			t.Errorf("%q: open (%s)", script, ts.OpenReason)
			continue
		}
		_, cond := keys(a, Output)
		if !reflect.DeepEqual(cond, want) {
			t.Errorf("%q: conditional keys %v want %v", script, cond, want)
		}
		for _, k := range want {
			if w := ts.Writes[k]; w == nil || w.ValueDynamic {
				t.Errorf("%q: %s value should stay literal: %+v", script, k, w)
			}
		}
	}
}
