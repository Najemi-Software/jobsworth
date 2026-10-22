package rules_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

const testdata = "../../testdata"

func TestRegistryInvariants(t *testing.T) {
	t.Parallel()
	seen := map[string]string{}
	families := map[string]bool{}
	for _, f := range rules.Families {
		families[f] = true
	}
	for _, r := range rules.Rules {
		if !rules.IDPattern.MatchString(r.ID) {
			t.Errorf("%s: id does not match %s", r.ID, rules.IDPattern)
		}
		// spec-rules §3.2 says two to four segments; spec-check-medium §3.2
		// itself names local-action-after-subdir-only, so five is allowed.
		if n := strings.Count(r.ID, "-") + 1; n < 2 || n > 5 {
			t.Errorf("%s: ids have two to five segments", r.ID)
		}
		if prev, dup := seen[r.ID]; dup {
			t.Errorf("%s: duplicate id (also %s)", r.ID, prev)
		}
		seen[r.ID] = r.ID
		for _, a := range r.Aliases {
			if prev, dup := seen[a]; dup {
				t.Errorf("%s: alias %s collides with %s", r.ID, a, prev)
			}
			seen[a] = r.ID
		}
		if !families[r.Family] {
			t.Errorf("%s: unknown family %q", r.ID, r.Family)
		}
		if r.Summary == "" || len(r.Summary) >= 80 || strings.HasSuffix(r.Summary, ".") {
			t.Errorf("%s: summary must be one sentence under 80 chars without a trailing period: %q", r.ID, r.Summary)
		}
		if r.Since == "" {
			t.Errorf("%s: missing since", r.ID)
		}
		switch r.Status {
		case rules.Stable, rules.Experimental, rules.Deprecated:
		default:
			t.Errorf("%s: bad status %q", r.ID, r.Status)
		}
		if r.Status == rules.Deprecated && !rules.IsKnown(r.Replacement) {
			t.Errorf("%s: deprecated without a registered replacement", r.ID)
		}
		if r.Fixable {
			t.Errorf("%s: fixable is reserved and must be false", r.ID)
		}
		for _, rel := range r.Related {
			if _, canon, ok := rules.Lookup(rel); !ok || canon != rel {
				t.Errorf("%s: related %q is not a canonical registered id", r.ID, rel)
			}
			if rel == r.ID {
				t.Errorf("%s: related to itself", r.ID)
			}
		}
		if _, err := rules.LoadDoc(r.ID); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
	}
	// Every doc file corresponds to a registered rule.
	for _, id := range rules.DocIDs() {
		if _, _, ok := rules.Lookup(id); !ok {
			t.Errorf("doc/%s.md has no registered rule", id)
		}
	}
	// Registry order is by family in Families order.
	last := -1
	for _, r := range rules.Rules {
		idx := indexOf(rules.Families, r.Family)
		if idx < last {
			t.Errorf("%s: family %s out of order", r.ID, r.Family)
		}
		last = idx
	}
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func TestLookupAndNearest(t *testing.T) {
	t.Parallel()
	if _, canon, ok := rules.Lookup("needs-unknown"); !ok || canon != "needs-unknown" {
		t.Fatal("lookup failed")
	}
	if _, _, ok := rules.Lookup("needs-unkown"); ok {
		t.Fatal("typo should not resolve")
	}
	near := rules.Nearest("needs-unkown", 3)
	if len(near) != 3 || near[0] != "needs-unknown" {
		t.Fatalf("Nearest = %v", near)
	}
}

func TestEffective(t *testing.T) {
	t.Parallel()
	r, _, _ := rules.Lookup("input-unused")
	sev, off, changed := rules.Effective(r, config.Config{})
	if sev != finding.Warning || off || changed {
		t.Fatalf("default: %v %v %v", sev, off, changed)
	}
	cfg := config.Config{Rules: map[string]config.Override{"input-unused": {Severity: finding.Error}}}
	sev, off, changed = rules.Effective(r, cfg)
	if sev != finding.Error || off || !changed {
		t.Fatalf("override: %v %v %v", sev, off, changed)
	}
	cfg = config.Config{Rules: map[string]config.Override{"input-unused": {Off: true}}}
	if _, off, changed := rules.Effective(r, cfg); !off || !changed {
		t.Fatalf("off: %v %v", off, changed)
	}
	exp := rules.Rule{ID: "x-y", Severity: finding.Note, Status: rules.Experimental}
	if _, off, _ := rules.Effective(exp, config.Config{}); !off {
		t.Fatal("experimental should be off by default")
	}
	cfg = config.Config{Rules: map[string]config.Override{"x-y": {Severity: finding.Note}}}
	if _, off, _ := rules.Effective(exp, cfg); off {
		t.Fatal("experimental named in config should run")
	}
	shipsOff := rules.Rule{ID: "x-z", Severity: finding.Note, Off: true}
	if _, off, _ := rules.Effective(shipsOff, config.Config{}); !off {
		t.Fatal("Off rule should be off")
	}
	fs := rules.ApplyEffective(config.Config{Rules: map[string]config.Override{"input-unused": {Off: true}}},
		[]finding.Finding{{Rule: "input-unused"}, {Rule: "needs-unknown"}, {Rule: "unregistered"}})
	if len(fs) != 2 || fs[0].Rule != "needs-unknown" || fs[1].Rule != "unregistered" {
		t.Fatalf("ApplyEffective = %+v", fs)
	}
}

// TestFamilyMembership pins the family of every basic-spec and §5.4 rule
// (the table from spec-rules §3.3, kept here so the test outlives the spec
// file).
func TestFamilyMembership(t *testing.T) {
	t.Parallel()
	want := map[string][]string{
		"inputs":   {"input-undeclared", "input-unused", "input-default-type", "choice-default-not-option", "choice-no-options"},
		"checkout": {"local-action-no-checkout", "local-action-wrong-repo", "local-action-sparse-miss", "local-action-after-subdir-only", "checkout-ref-unresolvable"},
		"secrets":  {"call-secret-unknown", "call-secret-missing", "call-secret-literal", "call-secret-inherit-partial", "secret-undeclared", "secret-unused", "secret-in-action", "secret-github-token-declared", "secret-to-output", "secret-in-name", "secret-unknown-in-scope"},
		"values":   {"value-set-mismatch", "if-always-true-string", "if-double-wrapped", "if-status-with-other"},
		"shapes":   {"shape-mismatch", "fromjson-invalid-literal", "fromjson-non-string", "jq-syntax", "shape-file-missing", "matrix-empty", "matrix-exclude-no-match", "matrix-strategy-type"},
		"calls":    {"call-input-unknown", "call-input-missing", "call-input-type", "step-input-unknown", "step-input-missing", "step-input-type", "step-input-choice", "step-with-on-run", "step-run-and-uses"},
		"jobs":     {"needs-unknown", "needs-self", "needs-cycle", "needs-output-job-unknown", "needs-output-not-needed", "needs-output-undeclared", "call-output-job-unknown", "call-output-undeclared", "output-unused"},
		"steps": {"step-id-unknown", "step-id-duplicate", "step-id-invalid", "step-forward-ref", "step-output-unwritten", "step-output-conditional",
			"step-output-action-undeclared", "step-output-not-run", "step-output-unused", "step-id-unreferenced", "action-output-unlifted", "action-output-unused", "composite-shell-missing"},
		"bash": {"bash-syntax", "bash-deprecated-command", "bash-output-unredirected", "bash-output-read", "bash-path-kv-line", "bash-env-bare-line", "bash-env-bad-key",
			"bash-env-reserved-key", "bash-env-same-step", "heredoc-unterminated", "heredoc-empty-delim", "heredoc-escaped-newline", "heredoc-delim-collision", "heredoc-indented-form"},
		"fetch": {"fetch-failed", "fetch-not-found", "fetch-offline", "uses-no-ref", "uses-dynamic", "callee-unparseable", "uses-invalid", "uses-not-found"},
		"structure": {"file-invalid", "runs-on-unknown", "runs-on-retired", "runs-on-matrix", "runs-on-missing", "runs-on-group-and-labels", "job-empty-steps", "job-no-steps",
			"step-null", "step-no-action", "job-orphan-outputs", "job-unreachable", "job-timeout-missing", "step-continue-on-error-outputs",
			"workflow-name-missing", "workflow-name-duplicate", "action-name-missing", "action-description-missing", "input-description-missing", "name-expression",
			"environment-unknown", "runs-on-group-unknown"},
		"suppress": {"suppress-wildcard", "suppress-unclosed", "suppress-unknown-rule", "suppress-unused"},
		"expressions": {"expr-unknown-function", "expr-arity", "expr-format-placeholders", "expr-contains-types", "expr-hashfiles-absolute",
			"expr-string-escape", "expr-comparison-type", "expr-env-in-if-job", "expr-secrets-in-if", "expr-context-unavailable"},
		"triggers": {"on-missing", "on-unknown-event", "on-unknown-key", "on-unknown-type", "on-filter-conflict", "on-filter-shape", "on-filter-expression",
			"on-filter-glob", "on-filter-empty", "on-workflow-run-target", "on-workflow-run-unknown", "on-push-tags-only",
			"cron-syntax", "cron-too-frequent", "cron-impossible-date", "cron-quoted", "dispatch-input-type", "call-input-type-decl",
			"dispatch-input-limit", "input-no-trigger", "dispatch-input-event-spelling", "concurrency-group-static", "concurrency-cancel-on-push-main", "on-filter-no-branch"},
		"security": {"injection-run", "injection-script", "injection-via-input", "prt-untrusted-checkout", "prt-untrusted-checkout-and-run", "prt-no-permissions",
			"uses-unpinned", "uses-unpinned-no-comment", "uses-docker-unpinned", "permissions-write-all", "permissions-missing", "permissions-unknown-scope",
			"permissions-unknown-level", "permissions-id-token-unused", "checkout-persist-credentials", "secret-to-artifact", "secret-echo", "self-hosted-pr-trigger", "cache-poisoning-path",
			"environment-no-protection", "uses-archived", "uses-deprecated-tag"},
		"conditions": {"if-not-boolean", "if-string-compare-bool", "if-always-false", "if-literal-false", "if-status-contradiction", "if-redundant-success",
			"if-dead-for-all-callers", "if-dead-for-caller", "expr-or-default-boolean", "expr-and-or-ternary-falsy"},
		"reachability": {"step-unreachable", "job-needs-skipped", "output-behind-condition"},
		"performance":  {"checkout-unused", "checkout-trailing", "checkout-redundant", "jobs-mergeable", "setup-no-cache", "artifact-whole-workspace"},
		"scripts":      {"shellcheck-codes", "python-syntax", "pwsh-syntax", "node-syntax", "ruby-syntax"},
	}
	for family, ids := range want {
		for _, id := range ids {
			r, _, ok := rules.Lookup(id)
			if !ok {
				t.Errorf("%s is not registered", id)
				continue
			}
			if r.Family != family {
				t.Errorf("%s is in family %s, want %s", id, r.Family, family)
			}
		}
	}
	for _, r := range rules.Rules {
		if indexOf(want[r.Family], r.ID) < 0 {
			t.Errorf("%s (family %s) is registered but not in this table; add it", r.ID, r.Family)
		}
	}
}

func TestDeprecatedRuleAddsNote(t *testing.T) {
	t.Parallel()
	old := rules.Rule{ID: "old-thing", Family: "jobs", Severity: finding.Warning, Status: rules.Deprecated, Replacement: "needs-unknown"}
	lookup := func(id string) (rules.Rule, bool) {
		if id == old.ID {
			return old, true
		}
		r, _, ok := rules.Lookup(id)
		return r, ok
	}
	got := rules.ApplyEffectiveWith(config.Config{}, []finding.Finding{{Rule: "old-thing", Line: 1}, {Rule: "old-thing", Line: 2}, {Rule: "needs-unknown"}}, lookup)
	if len(got) != 4 || got[3].Severity != finding.Note || !strings.Contains(got[3].Message, "use needs-unknown instead") {
		t.Fatalf("got %+v", got)
	}
	off := config.Config{Rules: map[string]config.Override{"old-thing": {Off: true}}}
	if got := rules.ApplyEffectiveWith(off, []finding.Finding{{Rule: "old-thing"}}, lookup); len(got) != 0 {
		t.Fatalf("off deprecated rule should emit nothing: %+v", got)
	}
}

// TestDocExamples runs every rule doc's flagged and passing examples through
// the pipeline (spec-rules §3.4): flagged must produce the rule (and at most
// its related ids), passes must not produce it.
func TestDocExamples(t *testing.T) {
	kit := testkit.New(t, testdata)
	for _, r := range rules.Rules {
		r := r
		t.Run(r.ID, func(t *testing.T) {
			t.Parallel()
			doc, err := rules.LoadDoc(r.ID)
			if err != nil {
				t.Fatal(err)
			}
			offline := false
			enrichOn := false
			self := ""
			cfg := config.Defaults()
			caseCfg := ""
			if r.ID == "permissions-missing" {
				caseCfg = "permissions-missing"
			}
			testkit.HarnessConfig(&cfg, caseCfg)
			for _, h := range doc.TestHints {
				switch {
				case h == "offline":
					offline = true
				case h == "enrich":
					enrichOn = true
				case strings.HasPrefix(h, "self="):
					self = strings.TrimPrefix(h, "self=")
				case h == "enable":
					// A default-off rule: the doc test turns it on at its
					// registered severity, as a config would.
					cfg.Rules[r.ID] = config.Override{Severity: r.Severity}
				}
			}
			runExample := func(name, body string) []string {
				dir := t.TempDir()
				path := filepath.Join(dir, name+".yml")
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				res := check.RunEntry(path, check.Options{
					Fetcher:      testkit.Fetcher(kit, offline),
					Config:       cfg,
					RootOverride: filepath.Join(kit.Testdata, "callees"),
					SelfSlug:     self,
					Enrich:       enrichOn,
					Store:        cache.New(kit.CacheDir),
				})
				var ids []string
				for _, f := range check.AllFindings(res) {
					id := f.Rule
					if rules.IsDerived(id) {
						id = "shellcheck-codes" // a derived id stands for its umbrella
					}
					ids = append(ids, id)
				}
				sort.Strings(ids)
				return ids
			}
			allowed := map[string]bool{r.ID: true}
			for _, rel := range r.Related {
				allowed[rel] = true
			}
			flagged := runExample("flagged", doc.Flagged)
			if indexOf(flagged, r.ID) < 0 {
				t.Errorf("flagged example does not trigger %s: got %v", r.ID, flagged)
			}
			for _, id := range flagged {
				if !allowed[id] {
					t.Errorf("flagged example triggers unrelated %s (got %v)", id, flagged)
				}
			}
			passes := runExample("passes", doc.Passes)
			if indexOf(passes, r.ID) >= 0 {
				t.Errorf("passes example triggers %s: got %v", r.ID, passes)
			}
		})
	}
}
