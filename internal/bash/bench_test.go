package bash

import "testing"

// BenchmarkBashAnalyse analyses a representative release script (test spec
// §4.2).
func BenchmarkBashAnalyse(b *testing.B) {
	script := `set -euo pipefail
VERSION=$(jq -r '.version' package.json)
if [ -z "$VERSION" ]; then
  echo "no version" >&2
  exit 1
fi
echo "version=$VERSION" >> "$GITHUB_OUTPUT"
echo "sha=$(git rev-parse HEAD)" >> "$GITHUB_OUTPUT"
{
  echo "notes<<EOF"
  git log --oneline -20
  echo "EOF"
} >> "$GITHUB_OUTPUT"
case "$GITHUB_REF" in
  refs/tags/*) echo "channel=stable" >> "$GITHUB_OUTPUT" ;;
  *) echo "channel=edge" >> "$GITHUB_OUTPUT" ;;
esac
echo "RELEASE_DIR=dist" >> "$GITHUB_ENV"
echo "$HOME/.local/bin" >> "$GITHUB_PATH"
`
	for i := 0; i < b.N; i++ {
		Analyse(script, false, nil)
	}
}
