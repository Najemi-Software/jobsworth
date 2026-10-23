package rules_test

import (
	"testing"

	"github.com/najemi-software/jobsworth/internal/contextschema"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// TestEventTablesAgree pins advanced §2.1: every event with a payload
// schema is a known trigger.
func TestEventTablesAgree(t *testing.T) {
	for name := range contextschema.Payloads {
		if _, ok := rules.Events[name]; !ok {
			t.Errorf("payload schema for %q but no trigger table entry", name)
		}
	}
}

func TestCronParsing(t *testing.T) {
	for expr, wantErr := range map[string]bool{
		"0 * * * *":       false,
		"*/15 * * * *":    false,
		"0 0 1 JAN MON":   false,
		"0 0 * * 7":       false,
		"0 0 * *":         true,
		"60 * * * *":      true,
		"0 0 * FOO *":     true,
		"0 0 * * MON-FRI": false,
		"1,2,3 * * * *":   false,
		"0 0 1-5/2 * *":   false,
		"*/0 * * * *":     true,
		"5-1 * * * *":     true,
	} {
		_, err := rules.ParseCron(expr)
		if (err != "") != wantErr {
			t.Errorf("%q: err %q, want error %v", expr, err, wantErr)
		}
	}
}
