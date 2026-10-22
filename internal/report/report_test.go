package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/finding"
)

func sample() []check.Result {
	return []check.Result{
		{
			Entry: "/repo/.github/workflows/release.yml",
			Files: []check.FileResult{
				{Name: "/repo/.github/workflows/release.yml", Findings: []finding.Finding{
					{Severity: finding.Error, Rule: "needs-output-undeclared", File: "/repo/.github/workflows/release.yml", Line: 42, Column: 9, Message: "job 'deploy' references needs.build.outputs.version, but job 'build' declares no output 'version'"},
					{Severity: finding.Note, Rule: "uses-dynamic", File: "/repo/.github/workflows/release.yml", Message: "dynamic"},
				}},
				{Name: "acme/shared/.github/workflows/lint.yml@v3", Findings: []finding.Finding{
					{Severity: finding.Warning, Rule: "input-unused", File: "acme/shared/.github/workflows/lint.yml@v3", Line: 7, Column: 7, Message: "input 'verbose' is declared but never referenced"},
				}},
			},
		},
		{
			Entry: "/repo/.github/workflows/ci.yml",
			Files: []check.FileResult{{Name: "/repo/.github/workflows/ci.yml"}},
		},
	}
}

func TestText(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	for _, r := range sample() {
		Text(&b, r, finding.Error, "/repo", false)
	}
	TextSummary(&b, Summarize(sample(), finding.Error))
	want := `FAIL .github/workflows/release.yml
  .github/workflows/release.yml:42:9  error  needs-output-undeclared
    job 'deploy' references needs.build.outputs.version, but job 'build' declares no output 'version'
  .github/workflows/release.yml  note  uses-dynamic
    dynamic
  acme/shared/.github/workflows/lint.yml@v3
    7:7  warning  input-unused  input 'verbose' is declared but never referenced

OK   .github/workflows/ci.yml

2 entries, 1 failed, 1 error, 1 warning, 1 note
`
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestTextSummaryOnlyForMultipleEntries(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	TextSummary(&b, Summary{Entries: 1, Failed: 1, Errors: 3})
	if b.Len() != 0 {
		t.Fatalf("single entry printed a summary: %q", b.String())
	}
	TextSummary(&b, Summary{Entries: 2})
	if b.String() != "2 entries, 0 failed, 0 errors, 0 warnings\n" {
		t.Fatalf("got %q", b.String())
	}
}

func TestFailOnChangesStatus(t *testing.T) {
	t.Parallel()
	r := check.Result{Entry: "/x.yml", Files: []check.FileResult{{Name: "/x.yml", Findings: []finding.Finding{{Severity: finding.Warning, Rule: "input-unused"}}}}}
	var b bytes.Buffer
	Text(&b, r, finding.Error, "", false)
	if !strings.HasPrefix(b.String(), "OK   ") {
		t.Fatalf("warning should not fail at --fail-on error: %q", b.String())
	}
	b.Reset()
	Text(&b, r, finding.Warning, "", false)
	if !strings.HasPrefix(b.String(), "FAIL ") {
		t.Fatalf("warning should fail at --fail-on warning: %q", b.String())
	}
	s := Summarize([]check.Result{r}, finding.Warning)
	if s.Failed != 1 || s.Warnings != 1 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestDisplayPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, cwd, want string }{
		{"/repo/.github/workflows/ci.yml", "/repo", ".github/workflows/ci.yml"},
		{"/other/ci.yml", "/repo", "/other/ci.yml"},
		{"owner/repo/action.yml@v1", "/repo", "owner/repo/action.yml@v1"},
		{"/repo/ci.yml", "", "/repo/ci.yml"},
	} {
		if got := DisplayPath(tc.name, tc.cwd); got != tc.want {
			t.Errorf("DisplayPath(%q, %q) = %q want %q", tc.name, tc.cwd, got, tc.want)
		}
	}
}

func TestJSONShape(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := JSON(&b, sample(), finding.Error); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Entries []struct {
			Path     string `json:"path"`
			OK       bool   `json:"ok"`
			Findings []struct {
				Severity string `json:"severity"`
				Rule     string `json:"rule"`
				File     string `json:"file"`
				Line     int    `json:"line"`
				Column   int    `json:"column"`
				Message  string `json:"message"`
			} `json:"findings"`
		} `json:"entries"`
		Summary Summary `json:"summary"`
	}
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 2 || doc.Entries[0].OK || !doc.Entries[1].OK {
		t.Fatalf("entries = %+v", doc.Entries)
	}
	f := doc.Entries[0].Findings
	if len(f) != 3 || f[0].Severity != "error" || f[0].Line != 42 || f[0].Column != 9 || f[2].File != "acme/shared/.github/workflows/lint.yml@v3" {
		t.Fatalf("findings = %+v", f)
	}
	if doc.Summary != (Summary{Entries: 2, Failed: 1, Errors: 1, Warnings: 1, Notes: 1}) {
		t.Fatalf("summary = %+v", doc.Summary)
	}
	// Absent line/column are omitted, not zero.
	if strings.Contains(b.String(), `"line": 0`) {
		t.Fatal("zero line should be omitted")
	}
	// Empty findings render as [] not null; suppressed is always present.
	if !strings.Contains(b.String(), `"findings": []`) || !strings.Contains(b.String(), `"suppressed": []`) {
		t.Fatalf("empty lists should be []:\n%s", b.String())
	}
}

func TestTextShowSuppressed(t *testing.T) {
	t.Parallel()
	r := check.Result{Entry: "/r/x.yml", Files: []check.FileResult{
		{Name: "/r/x.yml", Suppressed: []finding.Finding{{Severity: finding.Error, Rule: "needs-unknown", Line: 3, Column: 5, Message: "job 'a' needs unknown job 'nope'", Reason: "generated"}}},
		{Name: "acme/x/action.yml@v1", Suppressed: []finding.Finding{{Severity: finding.Warning, Rule: "input-unused", Line: 2, Column: 3, Message: "input 'x' is declared but never referenced"}}},
	}}
	var b bytes.Buffer
	Text(&b, r, finding.Error, "/r", false)
	if b.String() != "OK   x.yml\n\n" {
		t.Fatalf("hidden by default, got %q", b.String())
	}
	b.Reset()
	Text(&b, r, finding.Error, "/r", true)
	want := `OK   x.yml
  suppressed:
    x.yml:3:5  error  needs-unknown  job 'a' needs unknown job 'nope'  -- generated
  acme/x/action.yml@v1
    suppressed:
      2:3  warning  input-unused  input 'x' is declared but never referenced

`
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// TestSortOrder pins finding.Sort: severity, then line, column, rule,
// message (test spec §4, report row).
func TestSortOrder(t *testing.T) {
	t.Parallel()
	fs := []finding.Finding{
		{Severity: finding.Note, Rule: "b", Line: 1, Column: 1, Message: "n"},
		{Severity: finding.Error, Rule: "z", Line: 9, Column: 1, Message: "late"},
		{Severity: finding.Error, Rule: "b", Line: 2, Column: 5, Message: "m"},
		{Severity: finding.Error, Rule: "a", Line: 2, Column: 5, Message: "m"},
		{Severity: finding.Error, Rule: "a", Line: 2, Column: 3, Message: "m"},
		{Severity: finding.Warning, Rule: "a", Line: 1, Column: 1, Message: "w"},
		{Severity: finding.Error, Rule: "a", Line: 2, Column: 5, Message: "l"},
	}
	finding.Sort(fs)
	var got []string
	for _, f := range fs {
		got = append(got, f.Severity.String()+":"+f.Rule+":"+itoa(f.Line)+":"+itoa(f.Column)+":"+f.Message)
	}
	want := "error:a:2:3:m error:a:2:5:l error:a:2:5:m error:b:2:5:m error:z:9:1:late warning:a:1:1:w note:b:1:1:n"
	if strings.Join(got, " ") != want {
		t.Fatalf("order\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// TestDedupeSplit pins Dedupe.Split: an entry's own findings always
// print and seed the memory, a callee's first sighting prints, a repeat
// is attributed to the first entry, and a nil Dedupe keeps everything.
func TestDedupeSplit(t *testing.T) {
	t.Parallel()
	callee := "acme/shared/.github/workflows/lint.yml@v3"
	f1 := finding.Finding{Severity: finding.Warning, Rule: "input-unused", File: callee, Line: 7, Column: 7, Message: "x"}
	f2 := finding.Finding{Severity: finding.Note, Rule: "uses-dynamic", File: callee, Line: 9, Column: 1, Message: "y"}
	own := finding.Finding{Severity: finding.Error, Rule: "needs-unknown", File: "/r/a.yml", Line: 1, Message: "own"}

	var nilD *Dedupe
	if fresh, rep := nilD.Split("/r/a.yml", check.FileResult{Name: callee, Findings: []finding.Finding{f1, f2}}); len(fresh) != 2 || rep != nil {
		t.Fatalf("nil dedupe: fresh %d repeated %v", len(fresh), rep)
	}

	d := NewDedupe()
	// Own findings: all printed, remembered under the entry.
	fresh, rep := d.Split("/r/a.yml", check.FileResult{Name: "/r/a.yml", Findings: []finding.Finding{own}})
	if len(fresh) != 1 || len(rep) != 0 {
		t.Fatalf("own: fresh %d repeated %v", len(fresh), rep)
	}
	// Callee first seen under entry a: printed.
	fresh, rep = d.Split("/r/a.yml", check.FileResult{Name: callee, Findings: []finding.Finding{f1, f2}})
	if len(fresh) != 2 || len(rep) != 0 {
		t.Fatalf("first sighting: fresh %d repeated %v", len(fresh), rep)
	}
	// Same callee under entry b: nothing fresh, both attributed to a.
	fresh, rep = d.Split("/r/b.yml", check.FileResult{Name: callee, Findings: []finding.Finding{f1, f2}})
	if len(fresh) != 0 || strings.Join(rep, ",") != "/r/a.yml,/r/a.yml" {
		t.Fatalf("repeat: fresh %d repeated %v", len(fresh), rep)
	}
	// A new finding on the same callee under b is fresh.
	f3 := finding.Finding{Severity: finding.Note, Rule: "uses-dynamic", File: callee, Line: 11, Message: "z"}
	fresh, rep = d.Split("/r/b.yml", check.FileResult{Name: callee, Findings: []finding.Finding{f1, f3}})
	if len(fresh) != 1 || fresh[0].Line != 11 || len(rep) != 1 {
		t.Fatalf("mixed: fresh %+v repeated %v", fresh, rep)
	}
	// A later entry reaching entry a's own file as a callee does not
	// repeat a's own findings.
	fresh, rep = d.Split("/r/c.yml", check.FileResult{Name: "/r/a.yml", Findings: []finding.Finding{own}})
	if len(fresh) != 0 || strings.Join(rep, ",") != "/r/a.yml" {
		t.Fatalf("own as callee: fresh %d repeated %v", len(fresh), rep)
	}
}

// TestDedupeSeed pins Seed (medium review 4 D11): a callee that is itself
// an entry sorted after its caller is reported under its own listing, so
// the caller's walk attributes the repeat to that later entry.
func TestDedupeSeed(t *testing.T) {
	t.Parallel()
	calleeEntry := "/r/.github/workflows/reusable.yml"
	f1 := finding.Finding{Severity: finding.Warning, Rule: "input-unused", File: calleeEntry, Line: 7, Column: 7, Message: "x"}
	results := []check.Result{
		{Entry: "/r/.github/workflows/caller.yml", Files: []check.FileResult{
			{Name: "/r/.github/workflows/caller.yml"},
			{Name: calleeEntry, Findings: []finding.Finding{f1}},
		}},
		{Entry: calleeEntry, Files: []check.FileResult{{Name: calleeEntry, Findings: []finding.Finding{f1}}}},
	}
	d := NewDedupe()
	Seed(d, results)
	// The caller, printed first, sees the callee's finding as already owned
	// by the callee entry.
	fresh, rep := d.Split(results[0].Entry, results[0].Files[1])
	if len(fresh) != 0 || strings.Join(rep, ",") != calleeEntry {
		t.Fatalf("caller: fresh %d repeated %v", len(fresh), rep)
	}
	// The callee entry prints its own finding.
	fresh, rep = d.Split(results[1].Entry, results[1].Files[0])
	if len(fresh) != 1 || len(rep) != 0 {
		t.Fatalf("callee entry: fresh %d repeated %v", len(fresh), rep)
	}
	// Seeding a nil dedupe is a no-op.
	Seed(nil, results)
	// Without seeding, order decides: the caller claims the finding.
	d2 := NewDedupe()
	if fresh, _ := d2.Split(results[0].Entry, results[0].Files[1]); len(fresh) != 1 {
		t.Fatalf("unseeded caller should print the callee finding, got %d", len(fresh))
	}
	if fresh, rep := d2.Split(results[1].Entry, results[1].Files[0]); len(fresh) != 1 || len(rep) != 0 {
		t.Fatalf("an entry's own findings always print: fresh %d repeated %v", len(fresh), rep)
	}
}
