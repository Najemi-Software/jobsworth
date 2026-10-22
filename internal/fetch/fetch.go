// Package fetch talks to the GitHub REST API for ref resolution and raw file
// content (spec §5), in front of the on-disk cache (§6) and an in-process
// memo so each ref and each file is requested at most once per run.
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
)

// DefaultBaseURL is the public GitHub API.
const DefaultBaseURL = "https://api.github.com"

// DefaultConcurrency bounds in-flight requests (§5.7).
const DefaultConcurrency = 8

// Client holds configuration and per-run state. Construct with New.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	Cache   *cache.Cache
	Offline bool
	// Sleep is used for retry backoff; nil means time.Sleep.
	Sleep func(time.Duration)
	// Progress, if set, receives "fetching owner/repo@ref" style lines.
	Progress func(string)
	// LazyToken, if set, resolves the token on the first request instead
	// of at construction, so runs with no external references never shell
	// out to gh. It returns the token and whether one was found.
	LazyToken func() (string, bool)
	// OnNoToken is called once when LazyToken finds nothing.
	OnNoToken func()
	tokenOnce sync.Once
	// OnCacheError is called once when a cache write fails (an unwritable
	// root, spec-cache §6); the run continues without caching.
	OnCacheError func(err error)
	cacheErrOnce sync.Once

	// Stats counts this run's API activity (spec-cache §3.2).
	Stats Stats
	// log records every request made (spec-explain §3.1 fetch context).
	logMu sync.Mutex
	log   []LogEntry

	sem   chan struct{}
	memo  memo
	limit struct {
		sync.Mutex
		hit   bool
		reset time.Time
	}
}

// SetConcurrency bounds in-flight requests; n < 1 keeps the default.
func SetConcurrency(c *Client, n int) {
	if n < 1 {
		n = DefaultConcurrency
	}
	c.sem = make(chan struct{}, n)
}

// New returns a client with defaults filled in.
func New(baseURL, token string, c *cache.Cache) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Cache:   c,
		sem:     make(chan struct{}, DefaultConcurrency),
		memo:    memo{entries: map[string]*entry{}},
	}
}

// LogEntry is one API request as explain reports it.
type LogEntry struct {
	// What is the owner/repo@ref or file the request served.
	What   string
	Path   string
	Status int
	// RateLimited is set when the response carried an exhausted limit.
	RateLimited bool
	Err         string
}

// Log returns the requests made so far, in order.
func Log(c *Client) []LogEntry {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	return append([]LogEntry(nil), c.log...)
}

func (c *Client) logReq(e LogEntry) {
	c.logMu.Lock()
	c.log = append(c.log, e)
	c.logMu.Unlock()
}

// Stats are per-run counters, safe for concurrent use.
type Stats struct {
	APICalls     atomic.Int64
	FetchedBytes atomic.Int64
	// Successes counts references that resolved or fetched; AuthFailures
	// counts 401/403/rate-limit failures, for warm's exit code.
	Successes    atomic.Int64
	AuthFailures atomic.Int64
}

// ErrorKind classifies a failed fetch per §5.6.
type ErrorKind int

const (
	// NotFound: 404 — the reference is wrong. Error at the caller.
	NotFound ErrorKind = iota
	// Forbidden: 401/403 — no access. Warning at the caller.
	Forbidden
	// RateLimited: 403/429 with the limit exhausted. Warning, and no further
	// fetches this run.
	RateLimited
	// Network: transport error, timeout or 5xx after one retry. Warning.
	Network
	// Offline: --offline and not cached. Warning.
	Offline
)

func (k ErrorKind) String() string {
	switch k {
	case NotFound:
		return "not found"
	case Forbidden:
		return "forbidden"
	case RateLimited:
		return "rate limited"
	case Network:
		return "network error"
	case Offline:
		return "offline"
	}
	return "error"
}

// Error is a classified fetch failure.
type Error struct {
	Kind   ErrorKind
	Status int
	Reset  time.Time
	What   string
	Err    error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.What)
	b.WriteString(": ")
	switch e.Kind {
	case Offline:
		b.WriteString("offline, not cached")
	case RateLimited:
		fmt.Fprintf(&b, "GitHub API rate limit exceeded (resets %s)", e.Reset.Local().Format(time.Kitchen))
	case NotFound:
		b.WriteString("not found on GitHub")
	case Forbidden:
		fmt.Fprintf(&b, "could not fetch (HTTP %d)", e.Status)
	default:
		b.WriteString("could not fetch")
		if e.Err != nil {
			b.WriteString(": ")
			b.WriteString(e.Err.Error())
		}
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf extracts the ErrorKind from err; ok is false for non-fetch errors.
func KindOf(err error) (ErrorKind, bool) {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Kind, true
	}
	return 0, false
}

// RateLimitReset reports whether the run hit the rate limit and when it
// resets, for the one process-wide warning (§5.6).
func RateLimitReset(c *Client) (time.Time, bool) {
	c.limit.Lock()
	defer c.limit.Unlock()
	return c.limit.reset, c.limit.hit
}

// --- memo -------------------------------------------------------------------

type entry struct {
	done chan struct{}
	val  any
	err  error
}

type memo struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// once runs fn for key exactly once per process; concurrent and later
// callers wait for and share the first result.
func (m *memo) once(key string, fn func() (any, error)) (any, error) {
	m.mu.Lock()
	e, ok := m.entries[key]
	if !ok {
		e = &entry{done: make(chan struct{})}
		m.entries[key] = e
		m.mu.Unlock()
		e.val, e.err = fn()
		close(e.done)
		return e.val, e.err
	}
	m.mu.Unlock()
	<-e.done
	return e.val, e.err
}

// --- ref resolution (§5.2) --------------------------------------------------

// Resolved is a ref turned into an immutable SHA.
type Resolved struct {
	SHA  string
	Kind cache.RefKind
	// Cache is how the ref cache answered: hit, miss, stale (expired and
	// re-resolved), bypassed (cache disabled), or "" for a literal SHA.
	Cache string
}

// ResolveRef turns owner/repo@ref into a commit SHA and classifies the ref.
func ResolveRef(c *Client, owner, repo, ref string) (Resolved, error) {
	if cache.IsSHA(ref) {
		return Resolved{SHA: ref, Kind: cache.Commit}, nil
	}
	if sha, kind, ok := cache.GetRef(c.Cache, owner, repo, ref); ok {
		return Resolved{SHA: sha, Kind: kind, Cache: "hit"}, nil
	}
	cacheStatus := "miss"
	if _, _, hadPrev := cache.PeekRef(c.Cache, owner, repo, ref); hadPrev {
		cacheStatus = "stale"
	}
	if !cache.Active(c.Cache) {
		cacheStatus = "bypassed"
	}
	key := "ref:" + strings.ToLower(owner+"/"+repo) + "@" + ref
	what := owner + "/" + repo + "@" + ref
	v, err := c.memo.once(key, func() (any, error) {
		if c.Progress != nil {
			c.Progress("resolving " + what)
		}
		body, status, hdr, err := get(c, fmt.Sprintf("/repos/%s/%s/commits/%s", url.PathEscape(owner), url.PathEscape(repo), escapeRef(ref)), "application/vnd.github+json", what)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, classify(status, hdr, what, nil)
		}
		var resp struct {
			SHA string `json:"sha"`
		}
		if err := json.Unmarshal(body, &resp); err != nil || !cache.IsSHA(resp.SHA) {
			return nil, &Error{Kind: Network, What: what, Err: fmt.Errorf("unexpected commit response")}
		}
		r := Resolved{SHA: resp.SHA, Kind: cache.Branch, Cache: cacheStatus}
		prevSHA, prevKind, hadPrev := cache.PeekRef(c.Cache, owner, repo, ref)
		switch {
		case strings.HasPrefix(resp.SHA, strings.ToLower(ref)):
			r.Kind = cache.Commit
		case hadPrev && prevSHA == resp.SHA:
			// Unchanged since last stored: the kind cannot have changed
			// without the SHA moving, so skip the tag lookup.
			r.Kind = prevKind
		default:
			_, st, hdr, err := get(c, fmt.Sprintf("/repos/%s/%s/git/ref/tags/%s", url.PathEscape(owner), url.PathEscape(repo), escapeRef(ref)), "application/vnd.github+json", what)
			if err != nil {
				return nil, err
			}
			if st == http.StatusOK {
				r.Kind = cache.Tag
			} else if st != http.StatusNotFound {
				return nil, classify(st, hdr, what, nil)
			}
		}
		if err := cache.PutRef(c.Cache, owner, repo, r.Kind, ref, r.SHA); err != nil {
			c.cacheFailed(err) // cache write failures never fail the run
		}
		return r, nil
	})
	if err != nil {
		countFailure(c, err)
		return Resolved{}, err
	}
	c.Stats.Successes.Add(1)
	return v.(Resolved), nil
}

// cacheFailed reports an unwritable cache once and keeps going.
func (c *Client) cacheFailed(err error) {
	c.cacheErrOnce.Do(func() {
		if c.OnCacheError != nil {
			c.OnCacheError(err)
		}
	})
}

func countFailure(c *Client, err error) {
	if k, ok := KindOf(err); ok && (k == Forbidden || k == RateLimited) {
		c.Stats.AuthFailures.Add(1)
	}
}

// escapeRef path-escapes a ref while keeping '/' (branches like feature/x
// are valid in the commits endpoint).
func escapeRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// DefaultBranch returns a repo's default branch name (medium §3.1), cached
// under refs/ as a branch named HEAD whose "SHA" is unused: the name is
// stored in the content cache instead. Falls back to an API call.
func DefaultBranch(c *Client, owner, repo string) (string, error) {
	key := "default-branch:" + strings.ToLower(owner+"/"+repo)
	what := owner + "/" + repo + " default branch"
	v, err := c.memo.once(key, func() (any, error) {
		if data, ok := cache.GetContent(c.Cache, owner, repo, headSHA, ".jobsworth-default-branch"); ok {
			return strings.TrimSpace(string(data)), nil
		}
		body, status, hdr, err := get(c, fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo)), "application/vnd.github+json", what)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, classify(status, hdr, what, nil)
		}
		var resp struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := json.Unmarshal(body, &resp); err != nil || resp.DefaultBranch == "" {
			return nil, &Error{Kind: Network, What: what, Err: fmt.Errorf("unexpected repository response")}
		}
		_ = cache.PutContent(c.Cache, owner, repo, headSHA, ".jobsworth-default-branch", []byte(resp.DefaultBranch+"\n"))
		return resp.DefaultBranch, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// headSHA is the pseudo-SHA the default-branch name is cached under.
const headSHA = "0000000000000000000000000000000000000000"

// --- content (§5.3) ---------------------------------------------------------

// Content fetches a file at a SHA. The result is cached forever.
func Content(c *Client, owner, repo, sha, path string) ([]byte, error) {
	data, _, err := ContentWithStatus(c, owner, repo, sha, path)
	return data, err
}

// ContentWithStatus is Content reporting how the content cache answered:
// hit, miss, or bypassed (cache disabled). A SHA-pinned reference skips
// ref resolution, not the cache, so this is its cache status
// (spec-graph review D2).
func ContentWithStatus(c *Client, owner, repo, sha, path string) ([]byte, string, error) {
	if data, ok := cache.GetContent(c.Cache, owner, repo, sha, path); ok {
		return data, "hit", nil
	}
	status := "miss"
	if !cache.Active(c.Cache) {
		status = "bypassed"
	}
	data, err := fetchContent(c, owner, repo, sha, path)
	return data, status, err
}

func fetchContent(c *Client, owner, repo, sha, path string) ([]byte, error) {
	key := "content:" + strings.ToLower(owner+"/"+repo) + "/" + sha + "/" + path
	what := owner + "/" + repo + "/" + path + "@" + sha[:7]
	v, err := c.memo.once(key, func() (any, error) {
		if c.Progress != nil {
			c.Progress("fetching " + what)
		}
		body, status, hdr, err := get(c, fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", url.PathEscape(owner), url.PathEscape(repo), escapeRef(path), sha), "application/vnd.github.raw", what)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, classify(status, hdr, what, nil)
		}
		c.Stats.FetchedBytes.Add(int64(len(body)))
		if err := cache.PutContent(c.Cache, owner, repo, sha, path, body); err != nil {
			c.cacheFailed(err)
		}
		return body, nil
	})
	if err != nil {
		countFailure(c, err)
		return nil, err
	}
	c.Stats.Successes.Add(1)
	return v.([]byte), nil
}

// --- transport --------------------------------------------------------------

// get performs one bounded, retried GET. It returns an *Error for offline,
// rate-limit, and transport failures; HTTP status failures are returned as
// (status, headers) for the caller to classify, because 404 means different
// things to different endpoints.
func get(c *Client, path, accept, what string) (body []byte, status int, hdr http.Header, err error) {
	if c.Offline {
		return nil, 0, nil, &Error{Kind: Offline, What: what}
	}
	if reset, hit := RateLimitReset(c); hit {
		return nil, 0, nil, &Error{Kind: RateLimited, What: what, Reset: reset}
	}
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			sleep := c.Sleep
			if sleep == nil {
				sleep = time.Sleep
			}
			sleep(500 * time.Millisecond)
		}
		body, status, hdr, err = doGet(c, path, accept)
		if err != nil {
			lastErr = err
			c.logReq(LogEntry{What: what, Path: path, Err: err.Error()})
			continue
		}
		c.logReq(LogEntry{What: what, Path: path, Status: status, RateLimited: status == http.StatusTooManyRequests || hdr.Get("X-RateLimit-Remaining") == "0"})
		if status >= 500 {
			lastErr = fmt.Errorf("HTTP %d", status)
			continue
		}
		if status == http.StatusForbidden || status == http.StatusTooManyRequests {
			if e := rateLimit(c, status, hdr, what); e != nil {
				return nil, status, hdr, e
			}
		}
		return body, status, hdr, nil
	}
	return nil, 0, nil, &Error{Kind: Network, What: what, Err: lastErr}
}

func doGet(c *Client, path, accept string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "jobsworth")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	c.tokenOnce.Do(func() {
		if c.LazyToken == nil {
			return
		}
		token, found := c.LazyToken()
		if found {
			c.Token = token
		} else if c.OnNoToken != nil {
			c.OnNoToken()
		}
	})
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	c.Stats.APICalls.Add(1)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, nil, err
	}
	return body, resp.StatusCode, resp.Header, nil
}

// rateLimit records a rate-limit response and returns its error, or nil when
// the 403 was an ordinary permission failure.
func rateLimit(c *Client, status int, hdr http.Header, what string) *Error {
	retryAfter, hasRetryAfter := strconv.Atoi(hdr.Get("Retry-After"))
	secondary := hasRetryAfter == nil && retryAfter > 0
	if status != http.StatusTooManyRequests && hdr.Get("X-RateLimit-Remaining") != "0" && !secondary {
		return nil
	}
	reset := time.Now().Add(time.Hour)
	if v, err := strconv.ParseInt(hdr.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		reset = time.Unix(v, 0)
	}
	if secondary {
		reset = time.Now().Add(time.Duration(retryAfter) * time.Second)
	}
	c.limit.Lock()
	if !c.limit.hit {
		c.limit.hit = true
		c.limit.reset = reset
	}
	c.limit.Unlock()
	return &Error{Kind: RateLimited, Status: status, Reset: reset, What: what}
}

// classify turns a non-200 status into an *Error.
func classify(status int, hdr http.Header, what string, err error) *Error {
	switch status {
	case http.StatusNotFound:
		return &Error{Kind: NotFound, Status: status, What: what, Err: err}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &Error{Kind: Forbidden, Status: status, What: what, Err: err}
	}
	return &Error{Kind: Network, Status: status, What: what, Err: fmt.Errorf("HTTP %d", status)}
}

// API performs one GET against the REST API path (e.g.
// /repos/o/r/environments) and returns the body and status; what names
// the request in progress and error messages (advanced §11).
func API(c *Client, path, what string) ([]byte, int, error) {
	body, status, hdr, err := get(c, path, "application/vnd.github+json", what)
	if err != nil {
		return nil, 0, err
	}
	if status != http.StatusOK {
		return body, status, classify(status, hdr, what, nil)
	}
	return body, status, nil
}
