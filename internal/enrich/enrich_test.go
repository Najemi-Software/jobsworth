package enrich_test

import (
	"strings"
	"testing"

	"github.com/najemi-software/jobsworth/internal/enrich"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

// TestSecretScopes is advanced review 1 D1: an unreadable organisation
// list makes the set unknown; a user account (404) does not; environment
// secrets are read per environment.
func TestSecretScopes(t *testing.T) {
	kit := testkit.New(t, "../../testdata")
	c := enrich.New(testkit.Fetcher(kit, false), nil, 0)
	if names, ok := c.SecretNames("example", "app"); !ok || strings.Join(names, ",") != "NPM_TOKEN,ORG_TOKEN" {
		t.Fatalf("example/app: %v ok=%v", names, ok)
	}
	if names, ok := c.SecretNames("forbidden-org", "app"); ok {
		t.Fatalf("forbidden org list should be unknown, got %v", names)
	}
	// someone/thing has repository secrets? none listed → 404 on the repo
	// list itself is unknown too.
	if _, ok := c.SecretNames("someone", "thing"); ok {
		t.Fatal("a 404 repository list is unknown")
	}
	if names, ok := c.EnvironmentSecrets("example", "app", "production"); !ok || strings.Join(names, ",") != "PROD_KEY" {
		t.Fatalf("production: %v ok=%v", names, ok)
	}
	if _, ok := c.EnvironmentSecrets("example", "app", "staging"); ok {
		t.Fatal("forbidden environment list should be unknown")
	}
}

// TestLookupsMemoisedPerRun is review 1 N1: with no store, repeated
// lookups of one repository still cost one API call each.
func TestLookupsMemoisedPerRun(t *testing.T) {
	kit := testkit.New(t, "../../testdata")
	c := enrich.New(testkit.Fetcher(kit, false), nil, 0)
	kit.Server.ResetRequests()
	for i := 0; i < 5; i++ {
		if _, ok := c.LatestTag("someone", "thing"); !ok {
			t.Fatal("release lookup failed")
		}
		c.Archived("someone", "old")
		c.SecretNames("forbidden-org", "app") // a failure is memoised too
	}
	var n int
	for _, r := range kit.Server.Requests() {
		if strings.Contains(r.Path, "releases/latest") || strings.Contains(r.Path, "/repos/someone/old") || strings.Contains(r.Path, "actions/secrets") {
			n++
		}
	}
	if n != 4 { // release, repo, repo secrets, org secrets: once each
		t.Fatalf("%d API calls for 5 rounds of lookups, want 4: %v", n, kit.Server.Requests())
	}
}
