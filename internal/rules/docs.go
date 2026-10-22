package rules

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed doc/*.md
var docFS embed.FS

// RequiredSections are the level-2 headings every rule doc must have, in
// order (spec-rules §3.4).
var RequiredSections = []string{
	"What it checks",
	"Why it matters",
	"Example: flagged",
	"Example: passes",
	"When it does not fire",
	"Suppress",
	"Related",
}

// SectionKeys maps each required heading to its JSON key (spec-rules §4.3).
var SectionKeys = map[string]string{
	"What it checks":        "what",
	"Why it matters":        "why",
	"Example: flagged":      "flagged",
	"Example: passes":       "passes",
	"When it does not fire": "not_fire",
	"Suppress":              "suppress",
	"Related":               "related",
}

// Doc is a parsed rule document.
type Doc struct {
	ID string
	// Markdown is the whole file verbatim.
	Markdown string
	// Sections maps heading to body (trimmed), in file order via Order.
	Sections map[string]string
	Order    []string
	// Flagged/Passes are the YAML bodies of the example code blocks.
	Flagged, Passes string
	// TestHints are `<!-- jobsworth-test: ... -->` directives, e.g. "offline".
	TestHints []string
}

// LoadDoc reads and parses doc/<id>.md.
func LoadDoc(id string) (Doc, error) {
	data, err := fs.ReadFile(docFS, "doc/"+id+".md")
	if err != nil {
		return Doc{}, err
	}
	return ParseDoc(id, string(data))
}

// DocIDs lists the ids that have a doc file.
func DocIDs() []string {
	entries, _ := fs.ReadDir(docFS, "doc")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	return out
}

var (
	reH1   = regexp.MustCompile(`(?m)^# (.+)$`)
	reH2   = regexp.MustCompile(`(?m)^## (.+)$`)
	reHint = regexp.MustCompile(`<!--\s*jobsworth-test:\s*([^>]+?)\s*-->`)
	reYAML = regexp.MustCompile("(?s)```ya?ml\n(.*?)```")
)

// ParseDoc validates the title and splits the markdown into sections.
func ParseDoc(id, md string) (Doc, error) {
	d := Doc{ID: id, Markdown: md, Sections: map[string]string{}}
	t := reH1.FindStringSubmatch(md)
	if t == nil {
		return d, fmt.Errorf("doc %s: missing '# <id>' title", id)
	}
	if strings.TrimSpace(t[1]) != id {
		return d, fmt.Errorf("doc %s: title is %q", id, t[1])
	}
	for _, m := range reHint.FindAllStringSubmatch(md, -1) {
		d.TestHints = append(d.TestHints, strings.Fields(m[1])...)
	}
	locs := reH2.FindAllStringSubmatchIndex(md, -1)
	for i, loc := range locs {
		heading := strings.TrimSpace(md[loc[2]:loc[3]])
		end := len(md)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := strings.TrimSpace(md[loc[1]:end])
		d.Sections[heading] = body
		d.Order = append(d.Order, heading)
	}
	for i, want := range RequiredSections {
		if i >= len(d.Order) || d.Order[i] != want {
			return d, fmt.Errorf("doc %s: section %d must be %q (sections: %v)", id, i+1, want, d.Order)
		}
		if d.Sections[want] == "" {
			return d, fmt.Errorf("doc %s: section %q is empty", id, want)
		}
	}
	if m := reYAML.FindStringSubmatch(d.Sections["Example: flagged"]); m != nil {
		d.Flagged = m[1]
	} else {
		return d, fmt.Errorf("doc %s: 'Example: flagged' has no yaml block", id)
	}
	if m := reYAML.FindStringSubmatch(d.Sections["Example: passes"]); m != nil {
		d.Passes = m[1]
	} else {
		return d, fmt.Errorf("doc %s: 'Example: passes' has no yaml block", id)
	}
	return d, nil
}
