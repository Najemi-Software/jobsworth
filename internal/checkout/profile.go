package checkout

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Kind is how a wrapper supplies one checkout input (checkout-inference §2).
type Kind int

const (
	// WireAbsent: the key is not passed; checkout's own default applies.
	WireAbsent Kind = iota
	// WireLiteral: a fixed value, whatever the caller passes.
	WireLiteral
	// WirePassthrough: exactly `${{ inputs.X }}`; the caller's with.X, else the
	// wrapper's declared default.
	WirePassthrough
	// WireFallback: `${{ inputs.X || <literal or own-repo> }}`.
	WireFallback
	// WireDynamic: anything else; unknowable statically.
	WireDynamic
)

// String names the kind for explain output.
func (k Kind) String() string {
	return [...]string{"absent", "literal", "passthrough", "fallback", "dynamic"}[k]
}

// Wire is the supply of one checkout input.
type Wire struct {
	Kind Kind
	// Value: WireLiteral's value; WireFallback's default ("" means own repo / absent).
	Value string
	// Input: the wrapper input the caller supplies (WirePassthrough, WireFallback).
	Input string
	// Default is the wrapper's declared default for Input (WirePassthrough):
	// nil or WireAbsent when none, WireLiteral, or WireDynamic.
	Default *Wire
}

// Entry is one checkout a wrapper performs, as a function of its inputs.
type Entry struct {
	Wiring map[string]Wire
	Reason string
	// Via is the uses: chain from the wrapper's own step down to
	// actions/checkout; ViaStep is the 1-based step of the innermost one.
	Via     []string
	ViaStep int
	// Conditional: the checkout step (or a wrapper step on the way) has an
	// if: (§3.3).
	Conditional bool
	// Unknown: the nesting cap or a cycle was hit (§3.4); Reason says which.
	Unknown bool
	// TrailingClean: every step after the checkout inside the wrapper (and,
	// through the chain, after each wrapper step) is filesystem-clean, so
	// the caller's later steps are the first readers of the tree
	// (checkout-inference review 1 D2).
	TrailingClean bool
}

// Inputs are the checkout inputs the rules consume (§3.2).
var Inputs = []string{"path", "repository", "ref", "sparse-checkout", "sparse-checkout-cone-mode", "persist-credentials", "token"}

// MaxDepth caps nested wrappers (§3.4).
const MaxDepth = 3

// Resolver loads the callee of a uses: inside a wrapper: the parsed file
// and its identity, ok false when it cannot be loaded.
type Resolver func(raw string) (f *workflow.File, id string, ok bool)

// Cleaner reports whether a wrapper step is filesystem-clean (the
// performance rules' test); callee is the step's loaded action when it
// has one. nil means "a later step may read the tree".
type Cleaner func(st workflow.Step, callee *workflow.File) bool

// Profile computes a composite action's checkouts (§2 "Profile"). id is the
// action's own identity (for cycle detection); resolve loads nested
// wrappers. Nil for anything that is not a composite action or performs no
// checkout.
func Profile(f *workflow.File, id string, resolve Resolver) []Entry {
	return ProfileWith(f, id, resolve, nil)
}

// ProfileWith is Profile with the cleanliness test for TrailingClean.
func ProfileWith(f *workflow.File, id string, resolve Resolver, clean Cleaner) []Entry {
	return profile(f, resolve, clean, 0, map[string]bool{id: true})
}

// trailingClean reports whether every step of f after index i is clean.
func trailingClean(f *workflow.File, i int, resolve Resolver, clean Cleaner) bool {
	for _, st := range f.Steps {
		if st.Index <= i {
			continue
		}
		if clean == nil {
			return false
		}
		var callee *workflow.File
		if st.Uses != "" && resolve != nil {
			callee, _, _ = resolve(st.Uses)
		}
		if !clean(st, callee) {
			return false
		}
	}
	return true
}

func profile(f *workflow.File, resolve Resolver, clean Cleaner, depth int, seen map[string]bool) []Entry {
	if f == nil || f.Kind != workflow.KindCompositeAction {
		return nil
	}
	var out []Entry
	for _, st := range f.Steps {
		if st.Uses == "" {
			continue
		}
		cond := st.If.Value != "" && !alwaysTrue(st.If.Value)
		if IsCheckoutAction(st.Uses) {
			out = append(out, Entry{Wiring: wiringOf(st, f), Conditional: cond, Via: []string{st.Uses}, ViaStep: st.Index + 1, TrailingClean: trailingClean(f, st.Index, resolve, clean)})
			continue
		}
		ref, err := uses.Parse(st.Uses)
		if err != nil || ref.Kind == uses.Docker || ref.Kind == uses.Dynamic || ref.IsWorkflow || resolve == nil {
			continue
		}
		inner, innerID, ok := resolve(st.Uses)
		if !ok || inner == nil || inner.Kind != workflow.KindCompositeAction {
			continue
		}
		if seen[innerID] {
			out = append(out, Entry{Unknown: true, Reason: "wrapper cycle through " + st.Uses, Via: []string{st.Uses}, ViaStep: st.Index + 1, Conditional: cond})
			continue
		}
		if depth+1 >= MaxDepth {
			out = append(out, Entry{Unknown: true, Reason: "wrappers nested deeper than " + itoa(MaxDepth), Via: []string{st.Uses}, ViaStep: st.Index + 1, Conditional: cond})
			continue
		}
		next := map[string]bool{innerID: true}
		for k := range seen {
			next[k] = true
		}
		after := trailingClean(f, st.Index, resolve, clean)
		for _, e := range profile(inner, resolve, clean, depth+1, next) {
			c := compose(st, f, e)
			c.Via = append([]string{st.Uses}, e.Via...)
			c.Conditional = cond || e.Conditional
			c.TrailingClean = e.TrailingClean && after
			out = append(out, c)
		}
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

func alwaysTrue(v string) bool {
	t := strings.TrimSpace(v)
	t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "${{"), "}}"))
	return t == "" || t == "true" || t == "always()" || t == "success()"
}

var (
	reInputOnly   = regexp.MustCompile(`^\$\{\{\s*inputs\.([A-Za-z0-9_-]+)\s*\}\}$`)
	reInputOr     = regexp.MustCompile(`^\$\{\{\s*inputs\.([A-Za-z0-9_-]+)\s*\|\|\s*(?:'([^']*)'|"([^"]*)"|(github\.repository))\s*\}\}$`)
	reOwnRepo     = regexp.MustCompile(`^\$\{\{\s*github\.repository\s*\}\}$`)
	reOwnRepoExpr = reOwnRepo
)

// wiringOf reads one checkout step's with: inside a wrapper.
func wiringOf(st workflow.Step, f *workflow.File) map[string]Wire {
	with := map[string]workflow.Scalar{}
	for _, w := range st.With {
		with[w.Name] = w.Value
	}
	out := map[string]Wire{}
	for _, k := range Inputs {
		v, ok := with[k]
		if !ok {
			continue
		}
		out[k] = wireOf(v.Value, f)
	}
	return out
}

// wireOf classifies one with: value against the wrapper's inputs.
func wireOf(value string, f *workflow.File) Wire {
	t := strings.TrimSpace(value)
	if !strings.Contains(t, "${{") {
		return Wire{Kind: WireLiteral, Value: t}
	}
	if reOwnRepoExpr.MatchString(t) {
		return Wire{Kind: WireAbsent}
	}
	if m := reInputOnly.FindStringSubmatch(t); m != nil {
		d := defaultOf(f, m[1])
		return Wire{Kind: WirePassthrough, Input: m[1], Default: &d}
	}
	if m := reInputOr.FindStringSubmatch(t); m != nil {
		return Wire{Kind: WireFallback, Input: m[1], Value: m[2] + m[3]} // own repo → ""
	}
	return Wire{Kind: WireDynamic}
}

// defaultOf is the wrapper's declared default for an input.
func defaultOf(f *workflow.File, name string) Wire {
	for _, in := range f.Inputs {
		if !strings.EqualFold(in.Name, name) {
			continue
		}
		if !in.HasDefault {
			return Wire{Kind: WireAbsent}
		}
		if in.Default.IsExpr() {
			if reOwnRepo.MatchString(strings.TrimSpace(in.Default.Value)) {
				return Wire{Kind: WireAbsent}
			}
			return Wire{Kind: WireDynamic}
		}
		return Wire{Kind: WireLiteral, Value: strings.TrimSpace(in.Default.Value)}
	}
	return Wire{Kind: WireAbsent}
}

// compose applies an outer wrapper step's with: to an inner entry (§3.4).
func compose(outerStep workflow.Step, outer *workflow.File, e Entry) Entry {
	if e.Unknown {
		return e
	}
	with := map[string]workflow.Scalar{}
	for _, w := range outerStep.With {
		with[w.Name] = w.Value
	}
	supply := func(input string) Wire {
		v, ok := with[input]
		if !ok {
			return Wire{Kind: WireAbsent}
		}
		return wireOf(v.Value, outer)
	}
	out := Entry{Wiring: map[string]Wire{}, Conditional: e.Conditional, ViaStep: e.ViaStep, TrailingClean: e.TrailingClean}
	for k, iw := range e.Wiring {
		switch iw.Kind {
		case WireAbsent, WireLiteral, WireDynamic:
			out.Wiring[k] = iw
		case WirePassthrough:
			ow := supply(iw.Input)
			switch ow.Kind {
			case WireAbsent:
				if iw.Default != nil {
					out.Wiring[k] = *iw.Default
				}
			case WireLiteral:
				out.Wiring[k] = ow
			case WirePassthrough:
				d := Wire{Kind: WireAbsent}
				if ow.Default != nil && ow.Default.Kind != WireAbsent {
					d = *ow.Default
				}
				out.Wiring[k] = Wire{Kind: WirePassthrough, Input: ow.Input, Default: &d}
			case WireFallback:
				out.Wiring[k] = ow
			default:
				out.Wiring[k] = Wire{Kind: WireDynamic}
			}
		case WireFallback:
			ow := supply(iw.Input)
			switch ow.Kind {
			case WireAbsent:
				out.Wiring[k] = literalOrAbsent(iw.Value)
			case WireLiteral:
				if ow.Value != "" {
					out.Wiring[k] = ow
				} else {
					out.Wiring[k] = literalOrAbsent(iw.Value)
				}
			case WirePassthrough:
				d := iw.Value
				if ow.Default != nil && ow.Default.Kind == WireLiteral && ow.Default.Value != "" {
					d = ow.Default.Value
				}
				out.Wiring[k] = Wire{Kind: WireFallback, Input: ow.Input, Value: d}
			case WireFallback:
				d := ow.Value
				if d == "" {
					d = iw.Value
				}
				out.Wiring[k] = Wire{Kind: WireFallback, Input: ow.Input, Value: d}
			default:
				out.Wiring[k] = Wire{Kind: WireDynamic}
			}
		}
	}
	for k, w := range out.Wiring {
		if w.Kind == WireAbsent || (w.Kind == WireLiteral && w.Value == "") {
			delete(out.Wiring, k) // an empty literal is what absent means to checkout
		}
	}
	return out
}

func literalOrAbsent(v string) Wire {
	if v == "" {
		return Wire{Kind: WireAbsent}
	}
	return Wire{Kind: WireLiteral, Value: v}
}

// dynamicMarker stands in for a value that cannot be known statically; it
// classifies as an expression everywhere.
const dynamicMarker = "${{ <dynamic> }}"

// Effective applies a call site's with: to a profile entry (§3.2): the
// checkout.Step a direct actions/checkout with those values would give.
func Effective(e Entry, st workflow.Step) Step {
	if e.Unknown {
		return Step{Index: st.Index, Uses: st.Uses, Unknown: true, Reason: e.Reason, Via: e.Via, ViaStep: e.ViaStep, Conditional: e.Conditional, If: st.If.Value}
	}
	caller := map[string]workflow.Scalar{}
	for _, w := range st.With {
		caller[w.Name] = w.Value
	}
	with := map[string]workflow.Scalar{}
	for k, w := range e.Wiring {
		switch w.Kind {
		case WireLiteral:
			with[k] = workflow.Scalar{Value: w.Value}
		case WireDynamic:
			with[k] = workflow.Scalar{Value: dynamicMarker}
		case WirePassthrough:
			if v, ok := caller[w.Input]; ok {
				with[k] = v
				continue
			}
			if w.Default != nil {
				switch w.Default.Kind {
				case WireLiteral:
					with[k] = workflow.Scalar{Value: w.Default.Value}
				case WireDynamic:
					with[k] = workflow.Scalar{Value: dynamicMarker}
				}
			}
		case WireFallback:
			if v, ok := caller[w.Input]; ok && strings.TrimSpace(v.Value) != "" {
				with[k] = v
				continue
			}
			if w.Value != "" {
				with[k] = workflow.Scalar{Value: w.Value}
			}
		}
	}
	cs := classify(st.Index, with)
	cs.Uses = st.Uses
	cs.Via = e.Via
	cs.ViaStep = e.ViaStep
	cs.Conditional = e.Conditional || (st.If.Value != "" && !alwaysTrue(st.If.Value))
	cs.If = st.If.Value
	cs.Wiring = e.Wiring
	cs.TrailingClean = e.TrailingClean
	return cs
}

// EffectiveAll is Effective over every entry of a profile.
func EffectiveAll(entries []Entry, st workflow.Step) []Step {
	var out []Step
	for _, e := range entries {
		out = append(out, Effective(e, st))
	}
	return out
}
