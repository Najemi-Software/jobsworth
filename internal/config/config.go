// Package config reads .jobsworth.yml (spec-check-basic §9, spec-init §4):
// repo links, skip lists, checkout wrappers, fetch and cache settings,
// per-rule severity overrides, the fail threshold, and default entries.
// Parse validates per spec-init §4.3 and fills every omitted key with its
// built-in default, so a Config is always complete.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/strutil"
)

// FileName is the default config file name at a repo root.
const FileName = ".jobsworth.yml"

// SchemaVersion is the highest config schema this binary understands.
const SchemaVersion = 1

// Config is the parsed file with defaults applied.
type Config struct {
	// Repos maps owner/name (as written) to a path (as written; the caller
	// expands ~ and resolves relative paths against Dir).
	Repos map[string]string
	// Skip lists are lower-cased owner/repo[/path] slugs.
	SkipActions   map[string]bool
	SkipWorkflows map[string]bool
	// Rules maps rule id to its override: a severity, or Off.
	Rules             map[string]Override
	FetchAPIURL       string
	CacheDir          string
	ShellcheckEnabled string // auto | true | false
	ShellcheckPath    string
	ScriptsPython     string // auto | true | false
	ScriptsPwsh       string
	// Dir is the directory the config was read from; "" when no file.
	Dir string
	// Path is the file the config was loaded from ("" for defaults).
	Path string
	// BashOpaqueCommands are extra commands whose output is opaque.
	BashOpaqueCommands []string
	// BashRunnerCommands are extra package runners that execute the
	// script named by their arguments (medium review 2 D8).
	BashRunnerCommands []string
	// Entries are the default globs when check runs with no arguments.
	Entries []string
	// Advanced-spec sections (advanced §13).
	SecurityTrustedOwners []string
	SecurityOIDCActions   []string
	SelfHostedLabels      []string
	TriggersExtraEvents   []string
	StructureRunnerLabels []string
	ShellcheckExtraArgs   []string
	PerformanceFSClean    []string
	// Notes are one-line informational messages (schema missing).
	Notes                  []string
	Schema                 int
	FetchTimeout           time.Duration
	FetchParallel          int
	CacheTagTTL            time.Duration
	CacheBranchTTL         time.Duration
	FailOn                 finding.Severity
	ShellcheckMinSeverity  finding.Severity
	EnrichTTL              time.Duration
	ShapesGitHubEventOpen  bool
	SecurityStrict         bool
	ShapesFetchStaticFiles bool
	// EnrichActions enables the per-action metadata lookups under --enrich.
	EnrichActions bool
}

// Override is one rules: entry.
type Override struct {
	Off      bool
	Severity finding.Severity
}

// DefaultEntries are the built-in default entry globs.
var DefaultEntries = []string{
	".github/workflows/*.yml",
	".github/workflows/*.yaml",
	".github/actions/**/action.yml",
	".github/actions/**/action.yaml",
}

// Defaults returns the built-in configuration (spec-init §4.1).
func Defaults() Config {
	return Config{
		Schema:                SchemaVersion,
		Repos:                 map[string]string{},
		SkipActions:           map[string]bool{},
		SkipWorkflows:         map[string]bool{},
		BashOpaqueCommands:    []string{},
		BashRunnerCommands:    []string{},
		ShapesGitHubEventOpen: true,
		FetchAPIURL:           "https://api.github.com",
		FetchTimeout:          10 * time.Second,
		FetchParallel:         8,
		CacheTagTTL:           4 * time.Hour,
		CacheBranchTTL:        10 * time.Minute,
		Rules:                 map[string]Override{},
		FailOn:                finding.Error,
		Entries:               append([]string(nil), DefaultEntries...),
		ShellcheckEnabled:     "auto",
		ShellcheckMinSeverity: finding.Warning,
		ScriptsPython:         "auto",
		ScriptsPwsh:           "auto",
		EnrichTTL:             time.Hour,
		EnrichActions:         true,
	}
}

// Load reads and validates a config file. knownRule reports whether a rule
// id exists; nearestRule suggests ids for typos (nil disables suggestions).
func Load(path string, knownRule func(string) bool) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := Parse(data, knownRule)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	c.Dir = filepath.Dir(path)
	c.Path = path
	return c, nil
}

// LoadDefault loads <repoRoot>/.jobsworth.yml if present; a missing file
// yields Defaults() with Dir "" and no error.
func LoadDefault(repoRoot string, knownRule func(string) bool) (Config, error) {
	c, err := Load(filepath.Join(repoRoot, FileName), knownRule)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	return c, err
}

// NearestRule is the suggestion source for unknown rule ids; set by the
// rules package's importer (cmd) to avoid an import cycle. nil means no
// suggestions.
var NearestRule func(id string) []string

// knownKeys maps each top-level key to its allowed sub-keys (nil = scalar
// or free-form mapping).
var knownKeys = map[string][]string{
	"schema":      nil,
	"repos":       nil,
	"skip":        {"actions", "workflows"},
	"checkout":    {"actions"},
	"bash":        {"opaque_commands", "runner_commands"},
	"shapes":      {"github_event_open", "fetch_static_files"},
	"fetch":       {"api_url", "timeout", "parallel"},
	"cache":       {"dir", "tag_ttl", "branch_ttl"},
	"rules":       nil,
	"fail_on":     nil,
	"entries":     nil,
	"security":    {"trusted_owners", "oidc_actions", "strict", "self_hosted_labels"},
	"triggers":    {"extra_events"},
	"structure":   {"runner_labels", "self_hosted_labels"},
	"scripts":     {"shellcheck", "python", "pwsh"},
	"performance": {"fs_clean_actions"},
	"enrich":      {"ttl", "actions"},
}

// TopLevelKeys lists the valid top-level keys in schema order.
var TopLevelKeys = []string{"schema", "repos", "skip", "checkout", "bash", "shapes", "fetch", "cache", "rules", "fail_on", "entries",
	"security", "triggers", "structure", "scripts", "performance", "enrich"}

type raw struct {
	Schema *int              `yaml:"schema"`
	Repos  map[string]string `yaml:"repos"`
	Skip   struct {
		Actions   []string `yaml:"actions"`
		Workflows []string `yaml:"workflows"`
	} `yaml:"skip"`
	Checkout struct {
		Actions []string `yaml:"actions"`
	} `yaml:"checkout"`
	Bash struct {
		OpaqueCommands []string `yaml:"opaque_commands"`
		RunnerCommands []string `yaml:"runner_commands"`
	} `yaml:"bash"`
	Shapes struct {
		GitHubEventOpen  *bool `yaml:"github_event_open"`
		FetchStaticFiles *bool `yaml:"fetch_static_files"`
	} `yaml:"shapes"`
	Security struct {
		TrustedOwners    []string `yaml:"trusted_owners"`
		OIDCActions      []string `yaml:"oidc_actions"`
		Strict           *bool    `yaml:"strict"`
		SelfHostedLabels []string `yaml:"self_hosted_labels"`
	} `yaml:"security"`
	Triggers struct {
		ExtraEvents []string `yaml:"extra_events"`
	} `yaml:"triggers"`
	Structure struct {
		RunnerLabels     []string `yaml:"runner_labels"`
		SelfHostedLabels []string `yaml:"self_hosted_labels"`
	} `yaml:"structure"`
	Scripts struct {
		Shellcheck struct {
			Enabled     string   `yaml:"enabled"`
			Path        string   `yaml:"path"`
			MinSeverity string   `yaml:"min_severity"`
			ExtraArgs   []string `yaml:"extra_args"`
		} `yaml:"shellcheck"`
		Python string `yaml:"python"`
		Pwsh   string `yaml:"pwsh"`
	} `yaml:"scripts"`
	Performance struct {
		FSCleanActions []string `yaml:"fs_clean_actions"`
	} `yaml:"performance"`
	Enrich struct {
		TTL     string `yaml:"ttl"`
		Actions *bool  `yaml:"actions"`
	} `yaml:"enrich"`
	Fetch struct {
		APIURL   string `yaml:"api_url"`
		Timeout  string `yaml:"timeout"`
		Parallel string `yaml:"parallel"`
	} `yaml:"fetch"`
	Cache struct {
		Dir       string `yaml:"dir"`
		TagTTL    string `yaml:"tag_ttl"`
		BranchTTL string `yaml:"branch_ttl"`
	} `yaml:"cache"`
	Rules   map[string]string `yaml:"rules"`
	FailOn  string            `yaml:"fail_on"`
	Entries []string          `yaml:"entries"`
}

// Parse validates config bytes (spec-init §4.3) and applies defaults.
func Parse(data []byte, knownRule func(string) bool) (Config, error) {
	c := Defaults()
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Config{}, fmt.Errorf("yaml: %w", err)
	}
	empty, err := checkKeys(&node)
	if err != nil {
		return Config{}, err
	}
	if empty {
		c.Notes = append(c.Notes, "schema missing, assuming 1")
		return c, nil
	}
	var r raw
	if err := node.Decode(&r); err != nil {
		return Config{}, fmt.Errorf("yaml: %w", err)
	}

	if r.Schema == nil {
		c.Notes = append(c.Notes, "schema missing, assuming 1")
	} else if *r.Schema > SchemaVersion {
		return Config{}, fmt.Errorf("schema: version %d is newer than this binary supports (%d)", *r.Schema, SchemaVersion)
	} else if *r.Schema < 1 {
		return Config{}, fmt.Errorf("schema: version must be at least 1")
	} else {
		c.Schema = *r.Schema
	}

	for slug, p := range r.Repos {
		parts := strings.Split(slug, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return Config{}, fmt.Errorf("repos: %q is not owner/name", slug)
		}
		if strings.TrimSpace(p) == "" {
			return Config{}, fmt.Errorf("repos: %s has an empty path", slug)
		}
		c.Repos[slug] = p
	}
	for _, s := range r.Skip.Actions {
		if strings.Contains(s, "@") {
			return Config{}, fmt.Errorf("skip.actions: %q has an @ref; the ref is not part of the match", s)
		}
		c.SkipActions[strings.ToLower(strings.TrimSpace(s))] = true
	}
	for _, s := range r.Skip.Workflows {
		if strings.Contains(s, "@") {
			return Config{}, fmt.Errorf("skip.workflows: %q has an @ref; the ref is not part of the match", s)
		}
		c.SkipWorkflows[strings.ToLower(strings.TrimSpace(s))] = true
	}
	if len(r.Checkout.Actions) > 0 {
		// Accepted for one minor release and ignored (checkout-inference §4).
		c.Notes = append(c.Notes, "checkout.actions is no longer needed; checkout wrappers are detected from their content")
	}
	c.BashOpaqueCommands = append(c.BashOpaqueCommands, r.Bash.OpaqueCommands...)
	c.BashRunnerCommands = append(c.BashRunnerCommands, r.Bash.RunnerCommands...)
	if r.Shapes.GitHubEventOpen != nil {
		c.ShapesGitHubEventOpen = *r.Shapes.GitHubEventOpen
	}
	if r.Shapes.FetchStaticFiles != nil {
		c.ShapesFetchStaticFiles = *r.Shapes.FetchStaticFiles
	}
	c.SecurityTrustedOwners = append(c.SecurityTrustedOwners, r.Security.TrustedOwners...)
	c.SecurityOIDCActions = append(c.SecurityOIDCActions, r.Security.OIDCActions...)
	if r.Security.Strict != nil {
		c.SecurityStrict = *r.Security.Strict
		if c.SecurityStrict {
			// strict turns on the default-off credential rules (advanced §1.6).
			for _, id := range []string{"checkout-persist-credentials"} {
				if _, set := c.Rules[id]; !set {
					c.Rules[id] = Override{Severity: finding.Note}
				}
			}
		}
	}
	c.SelfHostedLabels = append(append(c.SelfHostedLabels, r.Security.SelfHostedLabels...), r.Structure.SelfHostedLabels...)
	c.TriggersExtraEvents = append(c.TriggersExtraEvents, r.Triggers.ExtraEvents...)
	c.StructureRunnerLabels = append(c.StructureRunnerLabels, r.Structure.RunnerLabels...)
	if v := r.Scripts.Shellcheck.Enabled; v != "" {
		if v != "auto" && v != "true" && v != "false" {
			return Config{}, fmt.Errorf("scripts.shellcheck.enabled: %q is not auto, true or false", v)
		}
		c.ShellcheckEnabled = v
	}
	c.ShellcheckPath = r.Scripts.Shellcheck.Path
	if v := r.Scripts.Shellcheck.MinSeverity; v != "" {
		sev, ok := finding.ParseSeverity(v)
		if !ok {
			return Config{}, fmt.Errorf("scripts.shellcheck.min_severity: %q is not error, warning or note", v)
		}
		c.ShellcheckMinSeverity = sev
	}
	c.ShellcheckExtraArgs = append(c.ShellcheckExtraArgs, r.Scripts.Shellcheck.ExtraArgs...)
	for _, kv := range []struct {
		name string
		val  string
		dst  *string
	}{{"scripts.python", r.Scripts.Python, &c.ScriptsPython}, {"scripts.pwsh", r.Scripts.Pwsh, &c.ScriptsPwsh}} {
		if kv.val == "" {
			continue
		}
		if kv.val != "auto" && kv.val != "true" && kv.val != "false" {
			return Config{}, fmt.Errorf("%s: %q is not auto, true or false", kv.name, kv.val)
		}
		*kv.dst = kv.val
	}
	c.PerformanceFSClean = append(c.PerformanceFSClean, r.Performance.FSCleanActions...)
	if r.Enrich.Actions != nil {
		c.EnrichActions = *r.Enrich.Actions
	}
	if r.Enrich.TTL != "" {
		d, err := time.ParseDuration(r.Enrich.TTL)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("enrich.ttl: %q is not a positive duration", r.Enrich.TTL)
		}
		c.EnrichTTL = d
	}
	if r.Fetch.APIURL != "" {
		c.FetchAPIURL = strings.TrimRight(r.Fetch.APIURL, "/")
	}
	if err := duration("fetch.timeout", r.Fetch.Timeout, &c.FetchTimeout); err != nil {
		return Config{}, err
	}
	if r.Fetch.Parallel != "" {
		n, err := strconv.Atoi(r.Fetch.Parallel)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("fetch.parallel: %q is not a positive integer", r.Fetch.Parallel)
		}
		c.FetchParallel = n
	}
	c.CacheDir = r.Cache.Dir
	if err := duration("cache.tag_ttl", r.Cache.TagTTL, &c.CacheTagTTL); err != nil {
		return Config{}, err
	}
	if err := duration("cache.branch_ttl", r.Cache.BranchTTL, &c.CacheBranchTTL); err != nil {
		return Config{}, err
	}

	ids := make([]string, 0, len(r.Rules))
	for id := range r.Rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if knownRule != nil && !knownRule(id) {
			msg := fmt.Sprintf("rules: unknown rule %q", id)
			if NearestRule != nil {
				if near := NearestRule(id); len(near) > 0 {
					msg += "; did you mean " + near[0] + "?"
				}
			}
			return Config{}, errors.New(msg)
		}
		v := r.Rules[id]
		if v == "off" || v == "false" {
			c.Rules[id] = Override{Off: true}
			continue
		}
		sev, ok := finding.ParseSeverity(v)
		if !ok {
			return Config{}, fmt.Errorf("rules: %s: %q is not off, error, warning or note", id, v)
		}
		c.Rules[id] = Override{Severity: sev}
	}
	if r.FailOn != "" {
		sev, ok := finding.ParseSeverity(r.FailOn)
		if !ok {
			return Config{}, fmt.Errorf("fail_on: %q is not error, warning or note", r.FailOn)
		}
		c.FailOn = sev
	}
	if r.Entries != nil {
		if len(r.Entries) == 0 {
			return Config{}, errors.New("entries: must list at least one glob")
		}
		c.Entries = append([]string(nil), r.Entries...)
	}
	return c, nil
}

func duration(key, s string, into *time.Duration) error {
	if s == "" {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return fmt.Errorf("%s: %q is not a duration (e.g. 10s, 4h)", key, s)
	}
	*into = d
	return nil
}

// checkKeys rejects unknown keys with a nearest-key suggestion. empty is
// true for a null document.
func checkKeys(doc *yaml.Node) (empty bool, err error) {
	root := doc
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return true, nil
		}
		root = root.Content[0]
	}
	if root.Kind == 0 || (root.Kind == yaml.ScalarNode && root.Tag == "!!null") {
		return true, nil
	}
	if root.Kind != yaml.MappingNode {
		return false, errors.New("config must be a mapping")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		subs, ok := knownKeys[k.Value]
		if !ok {
			return false, fmt.Errorf("unknown key %q (line %d); did you mean %q?", k.Value, k.Line, strutil.Nearest(k.Value, TopLevelKeys, 1)[0])
		}
		if subs == nil || v.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(v.Content); j += 2 {
			sk := v.Content[j]
			if !contains(subs, sk.Value) {
				return false, fmt.Errorf("unknown key %s.%s (line %d); did you mean %s.%s?", k.Value, sk.Value, sk.Line, k.Value, strutil.Nearest(sk.Value, subs, 1)[0])
			}
		}
	}
	return false, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Apply rewrites a finding's severity per the overrides, or drops it when
// the rule is off. The second result is false for a dropped finding.
func Apply(c Config, f finding.Finding) (finding.Finding, bool) {
	o, ok := c.Rules[f.Rule]
	if !ok {
		return f, true
	}
	if o.Off {
		return f, false
	}
	f.Severity = o.Severity
	return f, true
}

// ApplyAll filters and rewrites a slice.
func ApplyAll(c Config, fs []finding.Finding) []finding.Finding {
	if len(c.Rules) == 0 {
		return fs
	}
	out := fs[:0:0]
	for _, f := range fs {
		if g, keep := Apply(c, f); keep {
			out = append(out, g)
		}
	}
	return out
}
