package finding

import (
	"reflect"
	"testing"
)

func TestParseSeverity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want Severity
		ok   bool
	}{
		{"error", Error, true},
		{"warning", Warning, true},
		{"note", Note, true},
		{"fatal", 0, false},
		{"", 0, false},
		{"Error", 0, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseSeverity(tc.in)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("ParseSeverity(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
			}
			if ok && got.String() != tc.in {
				t.Fatalf("round trip %q -> %q", tc.in, got.String())
			}
		})
	}
}

func TestAtOrAbove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sev, threshold Severity
		want           bool
	}{
		{Error, Error, true},
		{Warning, Error, false},
		{Note, Error, false},
		{Error, Warning, true},
		{Warning, Warning, true},
		{Note, Warning, false},
		{Note, Note, true},
	} {
		if got := AtOrAbove(Finding{Severity: tc.sev}, tc.threshold); got != tc.want {
			t.Errorf("AtOrAbove(%v, %v) = %v", tc.sev, tc.threshold, got)
		}
	}
}

func TestSortOrdersBySeverityThenPosition(t *testing.T) {
	t.Parallel()
	fs := []Finding{
		{Severity: Note, Line: 1, Rule: "z"},
		{Severity: Error, Line: 9, Rule: "b"},
		{Severity: Warning, Line: 3},
		{Severity: Error, Line: 2, Column: 5, Rule: "a"},
		{Severity: Error, Line: 2, Column: 1, Rule: "a"},
		{Severity: Error, Line: 9, Rule: "a"},
	}
	Sort(fs)
	want := []Finding{
		{Severity: Error, Line: 2, Column: 1, Rule: "a"},
		{Severity: Error, Line: 2, Column: 5, Rule: "a"},
		{Severity: Error, Line: 9, Rule: "a"},
		{Severity: Error, Line: 9, Rule: "b"},
		{Severity: Warning, Line: 3},
		{Severity: Note, Line: 1, Rule: "z"},
	}
	if !reflect.DeepEqual(fs, want) {
		t.Fatalf("got %+v\nwant %+v", fs, want)
	}
}

func TestTally(t *testing.T) {
	t.Parallel()
	got := Tally([]Finding{{Severity: Error}, {Severity: Note}, {Severity: Error}, {Severity: Warning}})
	if got != (Count{Errors: 2, Warnings: 1, Notes: 1}) {
		t.Fatalf("Tally = %+v", got)
	}
}
