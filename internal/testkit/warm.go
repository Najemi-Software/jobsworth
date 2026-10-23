package testkit

import (
	"testing"

	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/uses"
)

// Warm pre-populates the kit's cache for the given external references
// ("owner/repo@ref" or "owner/repo/path@ref") from the fake server, so a
// test can start from a warm cache (test spec §3.2).
func Warm(t testing.TB, k *Kit, refs ...string) {
	t.Helper()
	c := Fetcher(k, false)
	for _, raw := range refs {
		ref, err := uses.Parse(raw)
		if err != nil {
			t.Fatalf("warm %s: %v", raw, err)
		}
		r, err := fetch.ResolveRef(c, ref.Owner, ref.Repo, ref.Ref)
		if err != nil {
			t.Fatalf("warm %s: %v", raw, err)
		}
		path := ref.Path
		if !ref.IsWorkflow {
			if path != "" {
				path += "/"
			}
			path += "action.yml"
		}
		if _, err := fetch.Content(c, ref.Owner, ref.Repo, r.SHA, path); err != nil {
			t.Fatalf("warm %s: %v", raw, err)
		}
	}
}
