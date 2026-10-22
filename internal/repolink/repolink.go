// Package repolink maps external owner/name repos to local checkouts (spec
// §4): explicit --repo links, config links, and implicit links detected from
// git remotes.
package repolink

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Source says how a link was registered; it drives precedence and the
// startup report (§4.4).
type Source int

const (
	// Explicit: --repo flag or config `repos:`. Always wins.
	Explicit Source = iota
	// CWD: detected from the current directory's git remote.
	CWD
	// Entry: detected from an entry file's git remote.
	Entry
)

func (s Source) String() string {
	switch s {
	case Explicit:
		return "explicit"
	case CWD:
		return "cwd"
	case Entry:
		return "entry"
	}
	return "unknown"
}

// Link is one registered redirect.
type Link struct {
	// Slug is the lower-cased owner/name.
	Slug string
	// Display is the owner/name spelling as first given.
	Display string
	Path    string
	Source  Source
}

// Links is the registry. Keys are lower-cased owner/name. Order records the
// registration sequence for the startup report.
type Links struct {
	byslug map[string]Link
	order  []string
}

// New returns an empty registry.
func New() *Links {
	return &Links{byslug: map[string]Link{}}
}

// Lookup finds a link for owner/name, case-insensitively.
func Lookup(l *Links, owner, name string) (Link, bool) {
	if l == nil {
		return Link{}, false
	}
	lk, ok := l.byslug[strings.ToLower(owner+"/"+name)]
	return lk, ok
}

// LookupSlug finds a link for an "owner/name" string, case-insensitively.
func LookupSlug(l *Links, slug string) (Link, bool) {
	if l == nil {
		return Link{}, false
	}
	lk, ok := l.byslug[strings.ToLower(slug)]
	return lk, ok
}

// All returns the links in registration order.
func All(l *Links) []Link {
	out := make([]Link, 0, len(l.order))
	for _, s := range l.order {
		out = append(out, l.byslug[s])
	}
	return out
}

// ParseExplicit parses the --repo syntax `owner/name=path` (§4.1). home
// expands a leading ~; relative paths resolve against cwd. The path must be
// an existing directory.
func ParseExplicit(arg, cwd, home string) (Link, error) {
	eq := strings.IndexByte(arg, '=')
	if eq < 0 {
		return Link{}, fmt.Errorf("--repo %q: expected owner/name=path", arg)
	}
	slug, p := strings.TrimSpace(arg[:eq]), strings.TrimSpace(arg[eq+1:])
	if err := validSlug(slug); err != nil {
		return Link{}, fmt.Errorf("--repo %q: %w", arg, err)
	}
	if p == "" {
		return Link{}, fmt.Errorf("--repo %q: empty path", arg)
	}
	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	st, err := os.Stat(p)
	if err != nil {
		return Link{}, fmt.Errorf("--repo %s: %w", slug, err)
	}
	if !st.IsDir() {
		return Link{}, fmt.Errorf("--repo %s: %s is not a directory", slug, p)
	}
	return Link{Slug: strings.ToLower(slug), Display: slug, Path: p, Source: Explicit}, nil
}

func validSlug(slug string) error {
	parts := strings.Split(slug, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("expected owner/name")
	}
	return nil
}

// AddExplicit registers an explicit link. The same slug with a different
// path is a conflict (§4.1); the same path is accepted silently. An explicit
// link replaces an implicit one for the same slug.
func AddExplicit(l *Links, lk Link) error {
	lk.Source = Explicit
	lk.Slug = strings.ToLower(lk.Slug)
	if cur, ok := l.byslug[lk.Slug]; ok {
		if cur.Source == Explicit && cur.Path != lk.Path {
			return fmt.Errorf("--repo %s: conflicting paths %s and %s", cur.Display, cur.Path, lk.Path)
		}
		l.byslug[lk.Slug] = lk
		return nil
	}
	l.byslug[lk.Slug] = lk
	l.order = append(l.order, lk.Slug)
	return nil
}

// AddImplicit registers a detected link unless the slug already has one
// (§4.2: first registered wins, silently).
func AddImplicit(l *Links, lk Link) {
	lk.Slug = strings.ToLower(lk.Slug)
	if _, ok := l.byslug[lk.Slug]; ok {
		return
	}
	l.byslug[lk.Slug] = lk
	l.order = append(l.order, lk.Slug)
}

// Detect looks for a git repository containing dir whose origin remote points
// at GitHub and returns the link to register. ok is false when dir is not in
// a git repo, git is unavailable, or the remote is not a GitHub URL.
func Detect(dir string, src Source) (Link, bool) {
	root, ok := GitRoot(dir)
	if !ok {
		return Link{}, false
	}
	// Negative results are cached too (medium review 4 N12): a checkout
	// with no origin would otherwise spawn git once per file per walk.
	// Tests that add a remote after a lookup call ResetMemo.
	remote, ok := memoised("origin:"+root, true, func() (string, bool) {
		out, err := git(root, "remote", "get-url", "origin")
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(out), true
	})
	if !ok {
		return Link{}, false
	}
	slug, ok := ParseRemote(remote)
	if !ok {
		return Link{}, false
	}
	return Link{Slug: strings.ToLower(slug), Display: slug, Path: root, Source: src}, true
}

// gitMemo caches subprocess results per directory for the process
// lifetime: a repo with hundreds of workflows would otherwise launch git
// for every entry and every local callee.
var gitMemo sync.Map // key -> memoResult

type memoResult struct {
	val string
	ok  bool
}

// ResetMemo forgets every cached git result (tests that change a repo's
// remotes after a lookup).
func ResetMemo() {
	gitMemo.Range(func(k, _ any) bool {
		gitMemo.Delete(k)
		return true
	})
	gitFlights.Range(func(k, _ any) bool {
		gitFlights.Delete(k)
		return true
	})
}

// memoised caches fn's result. With negatives=false a failed lookup is not
// cached.
// It is single-flight (medium review 5 N13): concurrent callers for one
// key wait for one subprocess instead of each spawning git.
func memoised(key string, negatives bool, fn func() (string, bool)) (string, bool) {
	if v, ok := gitMemo.Load(key); ok {
		return v.(memoResult).val, v.(memoResult).ok
	}
	v, _ := gitFlights.LoadOrStore(key, &flight{})
	fl := v.(*flight)
	fl.once.Do(func() {
		fl.val, fl.ok = fn()
		if fl.ok || negatives {
			gitMemo.Store(key, memoResult{fl.val, fl.ok})
		}
	})
	if !fl.ok && !negatives {
		// A negative that is not cached: let a later caller retry.
		gitFlights.Delete(key)
	}
	return fl.val, fl.ok
}

// flight is one in-progress or completed lookup.
type flight struct {
	once sync.Once
	val  string
	ok   bool
}

var gitFlights sync.Map // key -> *flight

// GitRoot returns the top-level directory of the git repo containing dir.
func GitRoot(dir string) (string, bool) {
	return memoised("root:"+dir, true, func() (string, bool) {
		out, err := git(dir, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", false
		}
		root := strings.TrimSpace(out)
		if root == "" {
			return "", false
		}
		return filepath.Clean(root), true
	})
}

func git(dir string, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", err
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

var remoteRes = []*regexp.Regexp{
	regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^ssh://git@github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
}

// ParseRemote extracts owner/name from a GitHub remote URL (§4.2). Anything
// that is not github.com is rejected.
func ParseRemote(url string) (string, bool) {
	for _, re := range remoteRes {
		if m := re.FindStringSubmatch(url); m != nil {
			return m[1] + "/" + m[2], true
		}
	}
	return "", false
}
