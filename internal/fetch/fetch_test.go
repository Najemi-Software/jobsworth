package fetch_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/testkit"
)

const remote = "../../testdata/remote"

func newClient(t *testing.T) (*fetch.Client, *testkit.Server) {
	t.Helper()
	srv := testkit.FakeGitHub(t, remote)
	c := fetch.New(srv.URL, "test-token", cache.New(t.TempDir()))
	c.Sleep = func(time.Duration) {}
	return c, srv
}

func count(srv *testkit.Server) int { return len(srv.Requests()) }

func TestResolveRefClassification(t *testing.T) {
	t.Parallel()
	full := "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name     string
		ref      string
		wantKind cache.RefKind
		wantSHA  string
		calls    int
		// warm is the total after a second, fresh client resolves the same
		// ref: tags/branches hit the disk cache, abbreviated SHAs are never
		// cached under refs/ (§6.2) so they cost one call again.
		warm int
	}{
		{"full sha", full, cache.Commit, full, 0, 0},
		{"abbreviated sha", full[:8], cache.Commit, full, 1, 2},
		{"tag", "v4", cache.Tag, testkit.SHAFor("actions", "checkout", "v4"), 2, 2},
		{"branch", "main", cache.Branch, testkit.SHAFor("actions", "checkout", "main"), 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, srv := newClient(t)
			got, err := fetch.ResolveRef(c, "actions", "checkout", tc.ref)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tc.wantKind || got.SHA != tc.wantSHA {
				t.Fatalf("got %+v want %v %s", got, tc.wantKind, tc.wantSHA)
			}
			if n := count(srv); n != tc.calls {
				t.Fatalf("cold: %d calls, want %d: %+v", n, tc.calls, srv.Requests())
			}
			// Warm: tags and branches come from disk, SHAs never call.
			c2 := fetch.New(srv.URL, "t", c.Cache)
			if _, err := fetch.ResolveRef(c2, "Actions", "Checkout", tc.ref); err != nil {
				t.Fatal(err)
			}
			if n := count(srv); n != tc.warm {
				t.Fatalf("warm: %d calls, want %d", n, tc.warm)
			}
		})
	}
}

func TestResolveRefMemo(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	c.Cache.Disabled = true
	for i := 0; i < 3; i++ {
		if _, err := fetch.ResolveRef(c, "actions", "checkout", "v4"); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(srv); n != 2 {
		t.Fatalf("in-memory dedupe: %d calls", n)
	}
}

func TestContent(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	r, err := fetch.ResolveRef(c, "actions", "checkout", "v4")
	if err != nil {
		t.Fatal(err)
	}
	srv.ResetRequests()
	data, err := fetch.Content(c, "actions", "checkout", r.SHA, "action.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: Checkout") {
		t.Fatalf("content = %q", data)
	}
	if n := count(srv); n != 1 {
		t.Fatalf("%d calls", n)
	}
	// Warm from disk, new client.
	c2 := fetch.New(srv.URL, "t", c.Cache)
	if _, err := fetch.Content(c2, "actions", "checkout", r.SHA, "action.yml"); err != nil {
		t.Fatal(err)
	}
	if n := count(srv); n != 1 {
		t.Fatalf("warm content: %d calls", n)
	}
	// 404.
	_, err = fetch.Content(c, "actions", "checkout", r.SHA, "nope.yml")
	if k, ok := fetch.KindOf(err); !ok || k != fetch.NotFound {
		t.Fatalf("missing file err = %v", err)
	}
}

func TestFailureModes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		repo  string
		want  fetch.ErrorKind
		calls int
	}{
		{"unknown repo", "nope", fetch.NotFound, 1},
		{"forbidden", "forbidden", fetch.Forbidden, 1},
		{"rate limited", "limited", fetch.RateLimited, 1},
		{"flaky recovers on retry", "flaky", 0, 3}, // 500 + retry + tag check
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, srv := newClient(t)
			_, err := fetch.ResolveRef(c, "acme", tc.repo, "v1")
			if tc.name == "flaky recovers on retry" {
				if err != nil {
					t.Fatal(err)
				}
			} else if k, ok := fetch.KindOf(err); !ok || k != tc.want {
				t.Fatalf("err = %v want %v", err, tc.want)
			}
			if n := count(srv); n != tc.calls {
				t.Fatalf("%d calls want %d: %+v", n, tc.calls, srv.Requests())
			}
		})
	}
}

func TestNoRetryOn404(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	_, _ = fetch.ResolveRef(c, "acme", "nope", "v1")
	if n := count(srv); n != 1 {
		t.Fatalf("404 must not retry, got %d calls", n)
	}
}

func TestRateLimitStopsFurtherFetches(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	_, err := fetch.ResolveRef(c, "acme", "limited", "v1")
	if k, _ := fetch.KindOf(err); k != fetch.RateLimited {
		t.Fatal(err)
	}
	reset, hit := fetch.RateLimitReset(c)
	if !hit || time.Until(reset) < 30*time.Second {
		t.Fatalf("reset = %v hit = %v", reset, hit)
	}
	srv.ResetRequests()
	_, err = fetch.ResolveRef(c, "actions", "checkout", "v4")
	if k, _ := fetch.KindOf(err); k != fetch.RateLimited {
		t.Fatalf("subsequent fetch should be rate limited without a call: %v", err)
	}
	if n := count(srv); n != 0 {
		t.Fatalf("made %d calls after rate limit", n)
	}
}

func TestTimeoutIsNetworkError(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	srv.TimeoutSleep = 300 * time.Millisecond
	c.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	_, err := fetch.ResolveRef(c, "acme", "slow", "v1")
	if k, ok := fetch.KindOf(err); !ok || k != fetch.Network {
		t.Fatalf("err = %v", err)
	}
	if n := count(srv); n != 2 {
		t.Fatalf("timeout should retry once, got %d calls", n)
	}
}

func TestOffline(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	r, err := fetch.ResolveRef(c, "actions", "checkout", "v4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetch.Content(c, "actions", "checkout", r.SHA, "action.yml"); err != nil {
		t.Fatal(err)
	}
	srv.ResetRequests()

	off := fetch.New(srv.URL, "", c.Cache)
	off.Offline = true
	if _, err := fetch.ResolveRef(off, "actions", "checkout", "v4"); err != nil {
		t.Fatalf("cached ref offline: %v", err)
	}
	if _, err := fetch.Content(off, "actions", "checkout", r.SHA, "action.yml"); err != nil {
		t.Fatalf("cached content offline: %v", err)
	}
	_, err = fetch.ResolveRef(off, "actions", "checkout", "main")
	if k, ok := fetch.KindOf(err); !ok || k != fetch.Offline {
		t.Fatalf("uncached offline err = %v", err)
	}
	if !strings.Contains(err.Error(), "offline, not cached") {
		t.Fatalf("message = %q", err.Error())
	}
	if n := count(srv); n != 0 {
		t.Fatalf("offline made %d calls", n)
	}
}

func TestAuthHeaderSent(t *testing.T) {
	t.Parallel()
	var got string
	srv := testkit.FakeGitHub(t, remote)
	proxy := http.NewServeMux()
	_ = proxy
	c := fetch.New(srv.URL, "", cache.New(t.TempDir()))
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Get("Authorization")
		return http.DefaultTransport.RoundTrip(r)
	})}
	_, _ = fetch.ResolveRef(c, "actions", "checkout", "main")
	if got != "" {
		t.Fatalf("unauthenticated client sent %q", got)
	}
	c.Token = "abc"
	_, _ = fetch.ResolveRef(c, "actions", "checkout", "v4")
	if got != "Bearer abc" {
		t.Fatalf("Authorization = %q", got)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResolveToken(t *testing.T) {
	t.Parallel()
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	gh := func(v string, ok bool) func() (string, bool) {
		return func() (string, bool) { return v, ok }
	}
	cases := []struct {
		name   string
		env    map[string]string
		gh     func() (string, bool)
		want   string
		source fetch.TokenSource
	}{
		{"jobsworth first", map[string]string{"JOBSWORTH_TOKEN": "j", "GITHUB_TOKEN": "g", "GH_TOKEN": "h"}, gh("c", true), "j", fetch.SourceJobsworth},
		{"github second", map[string]string{"GITHUB_TOKEN": "g", "GH_TOKEN": "h"}, gh("c", true), "g", fetch.SourceGitHub},
		{"gh_token third", map[string]string{"GH_TOKEN": " h "}, gh("c", true), "h", fetch.SourceGH},
		{"gh cli fourth", nil, gh("c\n", true), "c", fetch.SourceGHCLI},
		{"gh cli unauthenticated", nil, gh("", false), "", fetch.SourceNone},
		{"no gh", nil, nil, "", fetch.SourceNone},
		{"blank env ignored", map[string]string{"JOBSWORTH_TOKEN": "  "}, nil, "", fetch.SourceNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, src := fetch.ResolveToken(env(tc.env), tc.gh)
			if got != tc.want || src != tc.source {
				t.Fatalf("got %q %q want %q %q", got, src, tc.want, tc.source)
			}
		})
	}
}

func TestSecondaryRateLimit(t *testing.T) {
	t.Parallel()
	c, srv := newClient(t)
	_, err := fetch.ResolveRef(c, "acme", "secondary", "v1")
	if k, _ := fetch.KindOf(err); k != fetch.RateLimited {
		t.Fatalf("403 with Retry-After should be rate limited: %v", err)
	}
	reset, hit := fetch.RateLimitReset(c)
	if !hit || time.Until(reset) < 30*time.Second || time.Until(reset) > 90*time.Second {
		t.Fatalf("reset = %v hit = %v", reset, hit)
	}
	srv.ResetRequests()
	_, _ = fetch.ResolveRef(c, "actions", "checkout", "v4")
	if n := count(srv); n != 0 {
		t.Fatalf("fetched after secondary limit: %d calls", n)
	}
}

// TestLazyTokenResolvedOnce pins spec-check-basic review 3: the token is
// resolved on the first outgoing request only, and never before one.
func TestLazyTokenResolvedOnce(t *testing.T) {
	t.Parallel()
	srv := testkit.FakeGitHub(t, remote)
	c := fetch.New(srv.URL, "", cache.New(t.TempDir()))
	c.Cache.Disabled = true
	calls := 0
	var headers []string
	c.LazyToken = func() (string, bool) {
		calls++
		return "lazy-token", true
	}
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		headers = append(headers, r.Header.Get("Authorization"))
		return http.DefaultTransport.RoundTrip(r)
	})}
	if calls != 0 {
		t.Fatal("token resolved before any request")
	}
	if _, err := fetch.ResolveRef(c, "actions", "checkout", "v4"); err != nil {
		t.Fatal(err)
	}
	if _, err := fetch.ResolveRef(c, "actions", "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("LazyToken called %d times, want 1", calls)
	}
	if len(headers) < 2 {
		t.Fatalf("expected at least two requests, got %d", len(headers))
	}
	for _, h := range headers {
		if h != "Bearer lazy-token" {
			t.Fatalf("Authorization = %q", h)
		}
	}
}

// TestLazyTokenMissingWarnsOnce: with no token found, OnNoToken fires once
// and requests go out unauthenticated.
func TestLazyTokenMissingWarnsOnce(t *testing.T) {
	t.Parallel()
	srv := testkit.FakeGitHub(t, remote)
	c := fetch.New(srv.URL, "", cache.New(t.TempDir()))
	c.Cache.Disabled = true
	warned := 0
	var headers []string
	c.LazyToken = func() (string, bool) { return "", false }
	c.OnNoToken = func() { warned++ }
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		headers = append(headers, r.Header.Get("Authorization"))
		return http.DefaultTransport.RoundTrip(r)
	})}
	_, _ = fetch.ResolveRef(c, "actions", "checkout", "v4")
	_, _ = fetch.ResolveRef(c, "actions", "checkout", "main")
	if warned != 1 {
		t.Fatalf("OnNoToken fired %d times, want 1", warned)
	}
	for _, h := range headers {
		if h != "" {
			t.Fatalf("unauthenticated client sent Authorization %q", h)
		}
	}
	// Offline: no request, so no resolution and no warning.
	off := fetch.New(srv.URL, "", cache.New(t.TempDir()))
	off.Offline = true
	offWarned := 0
	off.LazyToken = func() (string, bool) { offWarned++; return "", false }
	off.OnNoToken = func() { offWarned++ }
	_, _ = fetch.ResolveRef(off, "actions", "checkout", "v4")
	if offWarned != 0 {
		t.Fatal("offline run must not resolve the token or warn")
	}
}
