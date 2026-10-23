// Package testkit holds helpers imported only by _test.go files: the fake
// GitHub server (test spec §3.1), temp cache/home setup (§3.2), and the
// fixture-case loader (§2). It is never compiled into the binary.
package testkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Request is one recorded API call.
type Request struct {
	Method string
	Path   string
	Query  string
}

// Server is the fake GitHub API over a testdata/remote tree.
type Server struct {
	*httptest.Server
	root string
	// TimeoutSleep is how long a `.timeout` ref sleeps; tests set the client
	// timeout below this.
	TimeoutSleep time.Duration

	mu       sync.Mutex
	requests []Request
	flaked   map[string]bool
}

// FakeGitHub serves root (a testdata/remote tree, see test spec §1.2) and
// records every request.
func FakeGitHub(t testing.TB, root string) *Server {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{root: abs, TimeoutSleep: 2 * time.Second, flaked: map[string]bool{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// Requests returns the ordered list of calls made so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// ResetRequests clears the recorded calls.
func (s *Server) ResetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

// SHAFor is the deterministic SHA the server returns for owner/repo/ref: the
// ref itself when it is 40 hex, else the first 40 hex of sha256(o/r/ref).
func SHAFor(owner, repo, ref string) string {
	if isSHA(ref) {
		return ref
	}
	sum := sha256.Sum256([]byte(strings.ToLower(owner) + "/" + strings.ToLower(repo) + "/" + ref))
	return hex.EncodeToString(sum[:])[:40]
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isSHA(s string) bool { return shaRe.MatchString(s) }

var (
	reRepo     = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)$`)
	reCommits  = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/commits/(.+)$`)
	reTagRef   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/ref/tags/(.+)$`)
	reContents = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/contents/(.+)$`)
)

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery})
	s.mu.Unlock()

	switch {
	case reMeta.MatchString(r.URL.Path):
		m := reMeta.FindStringSubmatch(r.URL.Path)
		s.meta(w, m[1], m[2], m[3])
	case reOrgRunners.MatchString(r.URL.Path):
		m := reOrgRunners.FindStringSubmatch(r.URL.Path)
		s.orgMeta(w, m[1], m[2])
	case reRepo.MatchString(r.URL.Path):
		m := reRepo.FindStringSubmatch(r.URL.Path)
		rd, ok := s.repoDir(m[1], m[2])
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		// The default branch is the ref dir named in `.default`, else main.
		branch := "main"
		if data, err := os.ReadFile(filepath.Join(rd, ".default")); err == nil {
			branch = strings.TrimSpace(string(data))
		}
		w.Header().Set("Content-Type", "application/json")
		info := map[string]any{"default_branch": branch}
		if data, err := os.ReadFile(filepath.Join(rd, ".meta", "repo.json")); err == nil {
			_ = json.Unmarshal(data, &info)
			info["default_branch"] = branch
		}
		_ = json.NewEncoder(w).Encode(info)
	case reCommits.MatchString(r.URL.Path):
		m := reCommits.FindStringSubmatch(r.URL.Path)
		s.commits(w, m[1], m[2], m[3])
	case reTagRef.MatchString(r.URL.Path):
		m := reTagRef.FindStringSubmatch(r.URL.Path)
		s.tagRef(w, m[1], m[2], m[3])
	case reContents.MatchString(r.URL.Path):
		m := reContents.FindStringSubmatch(r.URL.Path)
		s.contents(w, m[1], m[2], m[3], r.URL.Query().Get("ref"))
	default:
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}
}

// repoDir finds remote/<owner>/<repo>, case-insensitively.
func (s *Server) repoDir(owner, repo string) (string, bool) {
	ownerDir, ok := findDir(s.root, owner)
	if !ok {
		return "", false
	}
	return findDir(ownerDir, repo)
}

func findDir(parent, name string) (string, bool) {
	p := filepath.Join(parent, name)
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p, true
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), name) {
			return filepath.Join(parent, e.Name()), true
		}
	}
	return "", false
}

// refDirs lists the ref directories of a repo.
func refDirs(repoDir string) []string {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// resolve maps a ref (name, full SHA, or abbreviated SHA) to its ref dir and
// SHA.
func (s *Server) resolve(owner, repo, ref string) (dir, sha string, ok bool) {
	rd, ok := s.repoDir(owner, repo)
	if !ok {
		return "", "", false
	}
	// Refs may contain '/', so try the literal path first.
	if st, err := os.Stat(filepath.Join(rd, filepath.FromSlash(ref))); err == nil && st.IsDir() {
		return filepath.Join(rd, filepath.FromSlash(ref)), SHAFor(owner, repo, ref), true
	}
	for _, name := range refDirs(rd) {
		sha := SHAFor(owner, repo, name)
		if sha == ref || (len(ref) >= 7 && strings.HasPrefix(sha, ref)) {
			return filepath.Join(rd, name), sha, true
		}
	}
	return "", "", false
}

// marker checks for a failure-mode marker in the ref dir and writes the
// response for it. Returns true when the request has been handled.
func (s *Server) marker(w http.ResponseWriter, dir string) bool {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists(".forbidden"):
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
		return true
	case exists(".ratelimit"):
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10))
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
		return true
	case exists(".secondary"):
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("Retry-After", "60")
		http.Error(w, `{"message":"You have exceeded a secondary rate limit"}`, http.StatusForbidden)
		return true
	case exists(".timeout"):
		time.Sleep(s.TimeoutSleep)
		http.Error(w, `{"message":"too slow"}`, http.StatusGatewayTimeout)
		return true
	case exists(".flaky"):
		s.mu.Lock()
		first := !s.flaked[dir]
		s.flaked[dir] = true
		s.mu.Unlock()
		if first {
			http.Error(w, `{"message":"flaky"}`, http.StatusInternalServerError)
			return true
		}
	}
	return false
}

func (s *Server) commits(w http.ResponseWriter, owner, repo, ref string) {
	dir, sha, ok := s.resolve(owner, repo, ref)
	if !ok {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	if s.marker(w, dir) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"sha": sha})
}

func (s *Server) tagRef(w http.ResponseWriter, owner, repo, ref string) {
	dir, sha, ok := s.resolve(owner, repo, ref)
	if !ok {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	if s.marker(w, dir) {
		return
	}
	if _, err := os.Stat(filepath.Join(dir, ".tag")); err != nil {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/tags/" + ref, "object": map[string]string{"sha": sha}})
}

func (s *Server) contents(w http.ResponseWriter, owner, repo, path, ref string) {
	if ref == "" {
		ref = "main"
	}
	dir, _, ok := s.resolve(owner, repo, ref)
	if !ok {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	if s.marker(w, dir) {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.github.raw")
	_, _ = w.Write(data)
}

// Enrichment endpoints (advanced §11) are served from
// remote/<owner>/<repo>/.meta/<name>.json and remote/<owner>/.meta/runners.json;
// a missing file is a 404. The repo endpoint itself falls through to the
// default-branch handler unless .meta/repo.json exists.
var (
	reMeta       = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/(actions/permissions/workflow|environments|actions/secrets|branches|releases/latest|actions/workflows|environments/[^/]+/secrets)$`)
	reOrgRunners = regexp.MustCompile(`^/orgs/([^/]+)/actions/(runners|runner-groups|secrets)$`)
)

var metaFiles = map[string]string{
	"actions/permissions/workflow": "permissions.json",
	"environments":                 "environments.json",
	"actions/secrets":              "secrets.json",
	"branches":                     "branches.json",
	"releases/latest":              "release.json",
	"actions/workflows":            "workflows.json",
}

func (s *Server) meta(w http.ResponseWriter, owner, repo, what string) {
	rd, ok := s.repoDir(owner, repo)
	if !ok {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	name := metaFiles[what]
	if m := reEnvSecrets.FindStringSubmatch(what); m != nil {
		name = "env-" + m[1] + "-secrets.json"
	}
	serveMeta(w, filepath.Join(rd, ".meta", name))
}

var reEnvSecrets = regexp.MustCompile(`^environments/([^/]+)/secrets$`)

// serveMeta serves one meta JSON file; <name>.forbidden beside it means
// the token cannot read that endpoint (403), a missing file is a 404.
func serveMeta(w http.ResponseWriter, path string) {
	if _, err := os.Stat(path + ".forbidden"); err == nil {
		http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) orgMeta(w http.ResponseWriter, owner, what string) {
	od, ok := findDir(s.root, owner)
	if !ok {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		return
	}
	serveMeta(w, filepath.Join(od, ".meta", what+".json"))
}
