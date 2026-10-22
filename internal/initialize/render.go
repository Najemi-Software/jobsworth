package initialize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/config"
)

// Mode selects how much of the schema to write (§2.1).
type Mode int

const (
	// ModeDefault: detected values plus commented examples for the rest.
	ModeDefault Mode = iota
	// ModeMinimal: only non-default keys, no comments.
	ModeMinimal
	// ModeFull: every key with its default, commented.
	ModeFull
)

// Detected is what rendering writes (§4.2). Everything is optional.
type Detected struct {
	// Links are active repos: entries.
	Links []Checkout
	// Unlinked are referenced repos with no checkout, written commented.
	Unlinked []*RepoUse
	// SkipActions/SkipWorkflows come from --probe.
	SkipActions   []string
	SkipWorkflows []string
	// NonBashSteps is the §3.4 count; 0 writes nothing.
	NonBashSteps int
}

const header = `# .jobsworth.yml — configuration for ` + "`jobsworth check`" + `.
# Every key is optional. Paths are relative to this file.
`

const docsLine = "# Docs: https://github.com/najemi-software/jobsworth#configuration\n"

// Render produces the config text for a mode.
func Render(d Detected, mode Mode) string {
	switch mode {
	case ModeMinimal:
		return renderMinimal(d)
	case ModeFull:
		return renderFull(d)
	}
	return renderDefault(d)
}

func renderMinimal(d Detected) string {
	var b strings.Builder
	b.WriteString("schema: 1\n")
	if len(d.Links) > 0 {
		b.WriteString("repos:\n")
		for _, l := range sortedLinks(d.Links) {
			fmt.Fprintf(&b, "  %s: %s\n", l.Slug, l.Display)
		}
	}
	if len(d.SkipActions) > 0 || len(d.SkipWorkflows) > 0 {
		b.WriteString("skip:\n")
		writeList(&b, "  actions", d.SkipActions)
		writeList(&b, "  workflows", d.SkipWorkflows)
	}
	return b.String()
}

func renderDefault(d Detected) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("schema: 1\n")

	if len(d.Links) > 0 || len(d.Unlinked) > 0 {
		b.WriteString("\nrepos:\n")
		rows := repoRows(d)
		width := 0
		for _, r := range rows {
			if len(r[0]) > width {
				width = len(r[0])
			}
		}
		for _, r := range rows {
			fmt.Fprintf(&b, "%-*s  # %s\n", width, r[0], r[1])
		}
	}

	if len(d.SkipActions) > 0 || len(d.SkipWorkflows) > 0 {
		b.WriteString("\n# References to never fetch, parse, or validate (found with --probe).\nskip:\n")
		if len(d.SkipActions) > 0 {
			b.WriteString("  actions:\n")
			for _, s := range d.SkipActions {
				fmt.Fprintf(&b, "    - %s\n", s)
			}
		}
		if len(d.SkipWorkflows) > 0 {
			b.WriteString("  workflows:\n")
			for _, s := range d.SkipWorkflows {
				fmt.Fprintf(&b, "    - %s\n", s)
			}
		}
	} else {
		b.WriteString("\n# skip:\n#   actions:\n#     - some-org/paid-action\n")
	}

	if d.NonBashSteps > 0 {
		fmt.Fprintf(&b, "\n# %s a shell other than bash and will not be analysed by the bash rules.\n", stepsUse(d.NonBashSteps))
		b.WriteString("# bash:\n#   opaque_commands: []\n")
	}

	b.WriteString("\n# rules:\n#   step-output-unused: off\n#   input-unused: warning\n")
	b.WriteString("\n# fail_on: warning\n")
	return b.String()
}

func renderFull(d Detected) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(docsLine)
	b.WriteString("\nschema: 1                       # config schema version; bumped on incompatible changes\n")

	b.WriteString("\n# Map an external owner/repo to a local checkout. `uses:` references to\n")
	b.WriteString("# that repo are read from the checkout (any @ref ignored) instead of\n")
	b.WriteString("# fetched. Flags (--repo) override entries here.\n")
	if len(d.Links) > 0 || len(d.Unlinked) > 0 {
		b.WriteString("repos:                          # detected\n")
		rows := repoRows(d)
		width := 0
		for _, r := range rows {
			if len(r[0]) > width {
				width = len(r[0])
			}
		}
		for _, r := range rows {
			fmt.Fprintf(&b, "%-*s  # %s\n", width, r[0], r[1])
		}
	} else {
		b.WriteString("repos: {}                       # detected\n")
	}

	b.WriteString("\n# References to never fetch, parse, or validate. For actions you trust\n")
	b.WriteString("# but cannot fetch from this environment.\n")
	b.WriteString("skip:\n")
	fmt.Fprintf(&b, "  actions: %s   # owner/repo or owner/repo/sub/path, no @ref    (detected with --probe)\n", flow(d.SkipActions))
	fmt.Fprintf(&b, "  workflows: %s # owner/repo/.github/workflows/name.yml\n", flow(d.SkipWorkflows))

	if d.NonBashSteps > 0 {
		fmt.Fprintf(&b, "\n# %s a shell other than bash and will not be analysed by the bash rules.\n", stepsUse(d.NonBashSteps))
	} else {
		b.WriteString("\n")
	}
	b.WriteString("bash:\n")
	b.WriteString("  opaque_commands: []           # extra commands whose output is treated as opaque for heredoc-delimiter collision\n")

	b.WriteString("\nshapes:\n")
	b.WriteString("  github_event_open: true       # treat github.event payload objects as open (unknown fields allowed)\n")

	b.WriteString("\nfetch:\n")
	b.WriteString("  api_url: https://api.github.com   # GitHub Enterprise: https://ghe.example.com/api/v3\n")
	b.WriteString("  timeout: 10s\n")
	b.WriteString("  parallel: 8\n")

	b.WriteString("\ncache:\n")
	b.WriteString("  dir: \"\"                       # empty = platform default; --cache-dir and $JOBSWORTH_CACHE_DIR override\n")
	b.WriteString("  tag_ttl: 4h\n")
	b.WriteString("  branch_ttl: 10m\n")

	b.WriteString("\n# Per-rule severity override: error | warning | note | off.\n")
	b.WriteString("# `jobsworth rules` lists every id with its default.\n")
	b.WriteString("rules: {}\n")

	b.WriteString("\n# Lowest severity that fails the exit code. --fail-on overrides.\n")
	b.WriteString("fail_on: error\n")

	b.WriteString("\n# Default entries when `check` is run with no arguments.\n")
	b.WriteString("entries:\n")
	for _, e := range config.DefaultEntries {
		fmt.Fprintf(&b, "  - %s\n", e)
	}
	return b.String()
}

// repoRows builds the repos: lines as (text, comment) pairs: active links
// first, then commented-out placeholders for unlinked repos.
// IsWellKnownOwner reports whether a repo belongs to an owner whose
// actions are never checked out alongside a project (spec-init review N1).
func IsWellKnownOwner(slug string) bool {
	owner := strings.ToLower(strings.SplitN(slug, "/", 2)[0])
	return owner == "actions" || owner == "github"
}

func repoRows(d Detected) [][2]string {
	var rows [][2]string
	for _, l := range sortedLinks(d.Links) {
		c := "detected: origin matches"
		if !l.OriginVerified {
			c = "detected by directory name; origin not verified"
		}
		rows = append(rows, [2]string{fmt.Sprintf("  %s: %s", l.Slug, l.Display), c})
	}
	for _, u := range d.Unlinked {
		rows = append(rows, [2]string{fmt.Sprintf("  # %s: <path>", u.Slug),
			fmt.Sprintf("referenced in %s, no local checkout found", plural(u.Files, "file"))})
	}
	return rows
}

func sortedLinks(links []Checkout) []Checkout {
	out := append([]Checkout(nil), links...)
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Slug) < strings.ToLower(out[j].Slug) })
	return out
}

func writeList(b *strings.Builder, key string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", key)
	for _, it := range items {
		fmt.Fprintf(b, "%s  - %s\n", strings.Repeat(" ", len(key)-len(strings.TrimLeft(key, " "))), it)
	}
}

func flow(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func stepsUse(n int) string {
	if n == 1 {
		return "1 step uses"
	}
	return fmt.Sprintf("%d steps use", n)
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// ActionOwner is the GitHub owner of the jobsworth action used by
// --workflow and --pre-commit.
const ActionOwner = "najemi-software"

// RenderWorkflow produces .github/workflows/jobsworth.yml (§5). version is
// the running binary's version (e.g. "0.1.0"); privateRepos names linked
// repos that --probe found to be private.
func RenderWorkflow(version string, privateRepos []string) string {
	return RenderWorkflowPinned(version, privateRepos, "")
}

// ActionRef is the tag the generated workflow uses for the tool's action:
// the major of the running version.
func ActionRef(version string) string {
	return "v" + strings.SplitN(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".", 2)[0]
}

// RenderWorkflowPinned is RenderWorkflow with the tag resolved to a commit
// SHA, so the generated workflow follows the tool's own uses-unpinned
// advice (test review 5 D4); "" keeps the tag.
func RenderWorkflowPinned(version string, privateRepos []string, sha string) string {
	major := ActionRef(version)
	var b strings.Builder
	b.WriteString("name: jobsworth\n\non:\n  pull_request:\n    paths:\n      - '.github/**'\n      - '.jobsworth.yml'\n\n")
	b.WriteString("permissions:\n  contents: read\n\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")
	if sha != "" {
		fmt.Fprintf(&b, "      - uses: %s/jobsworth-action@%s # %s, the release matching this binary (v%s)\n",
			ActionOwner, sha, major, strings.TrimPrefix(strings.TrimSpace(version), "v"))
	} else {
		fmt.Fprintf(&b, "      - uses: %s/jobsworth-action@%s          # the release matching this binary (v%s); pin to a commit SHA before merging\n",
			ActionOwner, major, strings.TrimPrefix(strings.TrimSpace(version), "v"))
	}
	b.WriteString("        with:\n")
	b.WriteString("          # Needed to fetch private reusable workflows and actions.\n")
	b.WriteString("          # The default GITHUB_TOKEN only reads the current repo.\n")
	if len(privateRepos) > 0 {
		fmt.Fprintf(&b, "          # Detected private repos: %s. GITHUB_TOKEN cannot fetch them;\n", strings.Join(privateRepos, ", "))
		b.WriteString("          # use a PAT or a GitHub App token with read access to those repos.\n")
	}
	b.WriteString("          token: ${{ secrets.GITHUB_TOKEN }}\n")
	return b.String()
}

// RenderPreCommit produces the .pre-commit-config.yaml snippet (§6).
func RenderPreCommit(version string) string {
	v := "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
	return fmt.Sprintf("repos:\n  - repo: https://github.com/%s/jobsworth\n    rev: %s                                    # the running binary's version\n    hooks:\n      - id: jobsworth\n        files: ^\\.github/\n", ActionOwner, v)
}
