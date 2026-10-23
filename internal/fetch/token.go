package fetch

import (
	"os/exec"
	"strings"
)

// TokenSource names where a token came from.
type TokenSource string

const (
	SourceJobsworth TokenSource = "JOBSWORTH_TOKEN"
	SourceGitHub    TokenSource = "GITHUB_TOKEN"
	SourceGH        TokenSource = "GH_TOKEN"
	SourceGHCLI     TokenSource = "gh auth token"
	SourceNone      TokenSource = ""
)

// ResolveToken implements §5.1: JOBSWORTH_TOKEN, then GITHUB_TOKEN/GH_TOKEN,
// then `gh auth token`, then unauthenticated. getenv and ghToken are injected
// so tests never read the real environment or shell out.
func ResolveToken(getenv func(string) string, ghToken func() (string, bool)) (string, TokenSource) {
	if v := strings.TrimSpace(getenv("JOBSWORTH_TOKEN")); v != "" {
		return v, SourceJobsworth
	}
	if v := strings.TrimSpace(getenv("GITHUB_TOKEN")); v != "" {
		return v, SourceGitHub
	}
	if v := strings.TrimSpace(getenv("GH_TOKEN")); v != "" {
		return v, SourceGH
	}
	if ghToken != nil {
		if v, ok := ghToken(); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), SourceGHCLI
		}
	}
	return "", SourceNone
}

// GHCLIToken runs `gh auth token` if gh is installed. It is the production
// ghToken for ResolveToken.
func GHCLIToken() (string, bool) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", false
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
