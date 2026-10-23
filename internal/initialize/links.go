package initialize

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/najemi-software/jobsworth/internal/repolink"
)

// Checkout is a local checkout found for an external repo (§3.2).
type Checkout struct {
	Slug string
	// Path is absolute; Display is how it is written in the config: relative
	// to the repo root when under the same parent, else absolute with ~.
	Path    string
	Display string
	// OriginVerified is true when git confirmed the origin remote.
	OriginVerified bool
}

// LookupEnv is the environment a lookup needs; injected for tests.
type LookupEnv struct {
	Home string
	// ReposPath is $JOBSWORTH_REPOS, a colon-separated list of directories.
	ReposPath string
	// GitAvailable says whether origin can be verified; nil means detect.
	GitAvailable *bool
}

// FindCheckout looks for a local checkout of owner/repo (§3.2): a sibling
// of root named <repo> or <owner>-<repo>, then the same names under each
// $JOBSWORTH_REPOS directory. When git is available the candidate's origin
// must match; otherwise the name match is accepted.
func FindCheckout(root, slug string, env LookupEnv) (Checkout, bool) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return Checkout{}, false
	}
	owner, repo := parts[0], parts[1]
	gitOK := gitAvailable(env)
	var dirs []string
	dirs = append(dirs, filepath.Dir(root))
	for _, d := range strings.Split(env.ReposPath, string(os.PathListSeparator)) {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, expandHome(d, env.Home))
		}
	}
	for _, dir := range dirs {
		for _, name := range []string{repo, owner + "-" + repo} {
			cand := filepath.Join(dir, name)
			st, err := os.Stat(cand)
			if err != nil || !st.IsDir() {
				continue
			}
			if filepath.Clean(cand) == filepath.Clean(root) {
				continue
			}
			c := Checkout{Slug: slug, Path: cand}
			if gitOK {
				// Keep cand rather than git's resolved path: git follows
				// symlinks (/var → /private/var on macOS), which would stop
				// the checkout looking like a sibling of root.
				lk, ok := repolink.Detect(cand, repolink.Explicit)
				if !ok || !strings.EqualFold(lk.Slug, slug) {
					continue
				}
				c.OriginVerified = true
			}
			c.Display = DisplayPath(root, c.Path, env.Home)
			return c, true
		}
	}
	return Checkout{}, false
}

func gitAvailable(env LookupEnv) bool {
	if env.GitAvailable != nil {
		return *env.GitAvailable
	}
	_, err := exec.LookPath("git")
	return err == nil
}

// DisplayPath renders a checkout path for the config (§3.2): relative to
// root when both share a parent directory, else absolute with ~ for home.
func DisplayPath(root, path, home string) string {
	if filepath.Dir(root) == filepath.Dir(path) {
		return "../" + filepath.Base(path)
	}
	if home != "" && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
		return "~" + filepath.ToSlash(strings.TrimPrefix(path, home))
	}
	return path
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
