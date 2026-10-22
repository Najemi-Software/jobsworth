// Package enrich makes the optional read-only GitHub API lookups of
// advanced §11 and uses them only to suppress a finding, upgrade one, or
// produce an enrich-only finding. Results live in the cache's meta
// partition for the configured TTL.
package enrich

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/rules"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Client bundles the fetcher, the store and the TTL.
type Client struct {
	Fetcher *fetch.Client
	Store   *cache.Cache
	TTL     time.Duration
	// Actions enables the per-action lookups (uses-archived,
	// uses-deprecated-tag); enrich.actions: false turns them off
	// (advanced review 1 N1).
	Actions bool
	// memo dedupes lookups within one run, with or without a store.
	memo sync.Map
}

// New returns a client; a nil fetcher disables every lookup.
func New(f *fetch.Client, store *cache.Cache, ttl time.Duration) *Client {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &Client{Fetcher: f, Store: store, TTL: ttl, Actions: true}
}

// get fetches an API path through the meta cache. ok is false on any
// failure (the lookup is best-effort).
func (c *Client) get(path string, v any) bool {
	ok, _ := c.getStatus(path, v)
	return ok
}

type memoEntry struct {
	body   []byte
	status int
	err    bool
}

// getStatus is get returning also the HTTP status of a failed call (0
// when the call itself failed), so callers can tell "not there" (404)
// from "not allowed" (403).
func (c *Client) getStatus(path string, v any) (bool, int) {
	if c == nil || c.Fetcher == nil {
		return false, 0
	}
	key := "meta:" + path
	if m, ok := c.memo.Load(key); ok {
		e := m.(memoEntry)
		if e.err {
			return false, e.status
		}
		return json.Unmarshal(e.body, v) == nil, e.status
	}
	if data, ok := cache.GetMeta(c.Store, key, c.TTL); ok {
		c.memo.Store(key, memoEntry{body: data, status: 200})
		return json.Unmarshal(data, v) == nil, 200
	}
	body, status, err := fetch.API(c.Fetcher, path, "enrich "+path)
	if err != nil {
		c.memo.Store(key, memoEntry{status: status, err: true})
		return false, status
	}
	_ = cache.PutMeta(c.Store, key, body)
	c.memo.Store(key, memoEntry{body: body, status: status})
	return json.Unmarshal(body, v) == nil, status
}

// DefaultPermissions is the repository's default GITHUB_TOKEN permission:
// "read", "write", or "" when unknown.
func (c *Client) DefaultPermissions(owner, repo string) string {
	var v struct {
		Default string `json:"default_workflow_permissions"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s/actions/permissions/workflow", url.PathEscape(owner), url.PathEscape(repo)), &v) {
		return ""
	}
	return v.Default
}

// Environment is one deployment environment.
type Environment struct {
	Name      string
	Protected bool
}

// Environments lists the repository's environments; ok is false when the
// lookup failed.
func (c *Client) Environments(owner, repo string) ([]Environment, bool) {
	var v struct {
		Environments []struct {
			Name  string            `json:"name"`
			Rules []json.RawMessage `json:"protection_rules"`
		} `json:"environments"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s/environments", url.PathEscape(owner), url.PathEscape(repo)), &v) {
		return nil, false
	}
	var out []Environment
	for _, e := range v.Environments {
		out = append(out, Environment{Name: e.Name, Protected: len(e.Rules) > 0})
	}
	return out, true
}

// RunnerLabels lists the labels of the organisation's self-hosted
// runners; ok is false when the token cannot read them.
func (c *Client) RunnerLabels(owner string) ([]string, bool) {
	var v struct {
		Runners []struct {
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"runners"`
	}
	if !c.get(fmt.Sprintf("/orgs/%s/actions/runners", url.PathEscape(owner)), &v) {
		return nil, false
	}
	var out []string
	for _, r := range v.Runners {
		for _, l := range r.Labels {
			out = append(out, l.Name)
		}
	}
	return out, true
}

// RunnerGroups lists the organisation's runner groups.
func (c *Client) RunnerGroups(owner string) ([]string, bool) {
	var v struct {
		Groups []struct {
			Name string `json:"name"`
		} `json:"runner_groups"`
	}
	if !c.get(fmt.Sprintf("/orgs/%s/actions/runner-groups", url.PathEscape(owner)), &v) {
		return nil, false
	}
	var out []string
	for _, g := range v.Groups {
		out = append(out, g.Name)
	}
	return out, true
}

// SecretNames lists the repository's and organisation's secret names. ok
// is false when either list is unreadable: a 403 on the organisation list
// (it needs admin:org) means the set is unknown, not empty (advanced
// review 1 D1). A 404 means the owner is a user, not an organisation.
func (c *Client) SecretNames(owner, repo string) ([]string, bool) {
	var v struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s/actions/secrets", url.PathEscape(owner), url.PathEscape(repo)), &v) {
		return nil, false
	}
	var out []string
	for _, s := range v.Secrets {
		out = append(out, s.Name)
	}
	var org struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	ok, status := c.getStatus(fmt.Sprintf("/orgs/%s/actions/secrets", url.PathEscape(owner)), &org)
	switch {
	case ok:
		for _, s := range org.Secrets {
			out = append(out, s.Name)
		}
	case status == 404:
		// not an organisation: nothing to add
	default:
		return out, false
	}
	return out, true
}

// EnvironmentSecrets lists the secret names of one deployment
// environment; ok is false when they cannot be read.
func (c *Client) EnvironmentSecrets(owner, repo, env string) ([]string, bool) {
	var v struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s/environments/%s/secrets", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(env)), &v) {
		return nil, false
	}
	var out []string
	for _, s := range v.Secrets {
		out = append(out, s.Name)
	}
	return out, true
}

// Branches lists the first 300 branch names.
func (c *Client) Branches(owner, repo string) ([]string, bool) {
	var out []string
	for page := 1; page <= 3; page++ {
		var v []struct {
			Name string `json:"name"`
		}
		if !c.get(fmt.Sprintf("/repos/%s/%s/branches?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page), &v) {
			if page == 1 {
				return nil, false
			}
			break
		}
		for _, b := range v {
			out = append(out, b.Name)
		}
		if len(v) < 100 {
			break
		}
	}
	return out, true
}

// Archived reports whether a repository is archived.
func (c *Client) Archived(owner, repo string) (bool, bool) {
	var v struct {
		Archived bool `json:"archived"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo)), &v) {
		return false, false
	}
	return v.Archived, true
}

// LatestTag is the tag of the latest release.
func (c *Client) LatestTag(owner, repo string) (string, bool) {
	var v struct {
		Tag string `json:"tag_name"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s/releases/latest", url.PathEscape(owner), url.PathEscape(repo)), &v) || v.Tag == "" {
		return "", false
	}
	return v.Tag, true
}

// WorkflowNames lists the names of a repository's workflows.
func (c *Client) WorkflowNames(owner, repo string) ([]string, bool) {
	var v struct {
		Workflows []struct {
			Name string `json:"name"`
		} `json:"workflows"`
	}
	if !c.get(fmt.Sprintf("/repos/%s/%s/actions/workflows", url.PathEscape(owner), url.PathEscape(repo)), &v) {
		return nil, false
	}
	var out []string
	for _, w := range v.Workflows {
		out = append(out, w.Name)
	}
	return out, true
}

// Inputs is what Apply knows about the file.
type Inputs struct {
	// Owner and Repo are the file's repository ("" when unknown).
	Owner, Repo string
	// Local is true for the entry repository's own files.
	Local bool
}

var (
	reMajor = regexp.MustCompile(`^v?(\d+)(?:\.|$)`)
	reSHA   = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// Apply adjusts a file's findings with the lookups: suppresses, upgrades,
// and adds the enrich-only rules (advanced §11).
func (c *Client) Apply(f *workflow.File, fs []finding.Finding, in Inputs) []finding.Finding {
	if c == nil || c.Fetcher == nil {
		return fs
	}
	var out []finding.Finding
	privileged := false
	for _, ev := range f.Events {
		if contains(rules.PrivilegedTriggers, ev) {
			privileged = true
		}
	}
	known := in.Owner != "" && in.Repo != ""
	var labels []string
	labelsOK := false
	defaultPerm := ""
	if known {
		labels, labelsOK = c.RunnerLabels(in.Owner)
		defaultPerm = c.DefaultPermissions(in.Owner, in.Repo)
	}
	for _, fd := range fs {
		switch fd.Rule {
		case "permissions-missing":
			switch {
			case defaultPerm == "read":
				continue // the repository default is read-only
			case defaultPerm == "write" && privileged:
				fd.Severity = finding.Error
				fd.Message += " (the repository default is read-write and the trigger is privileged)"
			}
		case "runs-on-unknown":
			if labelsOK && labelInMessage(fd.Message, labels) {
				continue
			}
		}
		out = append(out, fd)
	}
	if !known || f.Kind != workflow.KindWorkflow {
		return out
	}
	// environment-unknown and environment-no-protection.
	if envs, ok := c.Environments(in.Owner, in.Repo); ok {
		for _, j := range f.Jobs {
			if j.Environment == nil || j.Environment.IsExpr() || j.Environment.Value == "" {
				continue
			}
			found := false
			for _, e := range envs {
				if e.Name == j.Environment.Value {
					found = true
					if !e.Protected && privileged {
						out = append(out, rules.Finding("environment-no-protection", j.Environment.Pos, fmt.Sprintf("job '%s' deploys to environment %q, which has no required reviewers or branch policy, on a privileged trigger", j.Name, e.Name)))
					}
				}
			}
			if !found {
				out = append(out, rules.Finding("environment-unknown", j.Environment.Pos, fmt.Sprintf("job '%s' names environment %q, which %s/%s does not have", j.Name, j.Environment.Value, in.Owner, in.Repo)))
			}
		}
	}
	// runs-on-group-unknown.
	if groups, ok := c.RunnerGroups(in.Owner); ok {
		for _, j := range f.Jobs {
			if j.RunsOn == nil || !j.RunsOn.HasGroup || j.RunsOn.Group.IsExpr() {
				continue
			}
			if !contains(groups, j.RunsOn.Group.Value) {
				out = append(out, rules.Finding("runs-on-group-unknown", j.RunsOn.Pos, fmt.Sprintf("job '%s' runs on group %q, which organisation %s does not have", j.Name, j.RunsOn.Group.Value, in.Owner)))
			}
		}
	}
	// secret-unknown-in-scope: entry workflows' own references, against
	// the repository, organisation and (per job) environment secrets. Any
	// scope the reference could resolve through being unreadable means
	// the set is unknown and nothing fires (advanced review 1 D1).
	if in.Local && !f.HasWorkflowCall {
		if names, ok := c.SecretNames(in.Owner, in.Repo); ok {
			seen := map[string]bool{}
			envSecrets := map[string][]string{}
			envOK := map[string]bool{}
			for _, e := range f.Exprs {
				scope := names
				where := "repository or organisation"
				if j := workflow.JobByName(f, e.Job); j != nil && j.Environment != nil && !j.Environment.IsExpr() && j.Environment.Value != "" {
					env := j.Environment.Value
					if _, done := envOK[env]; !done {
						envSecrets[env], envOK[env] = c.EnvironmentSecrets(in.Owner, in.Repo, env)
					}
					if !envOK[env] {
						continue // unreadable environment scope: unknown
					}
					scope = append(append([]string{}, names...), envSecrets[env]...)
					where = "repository, organisation or " + env + " environment"
				}
				for _, name := range rules.SecretRefs(e.Text) {
					if strings.EqualFold(name, "GITHUB_TOKEN") || seen[name] {
						continue
					}
					seen[name] = true
					if !contains(scope, name) {
						out = append(out, rules.Finding("secret-unknown-in-scope", e.Pos, fmt.Sprintf("secrets.%s is referenced but %s/%s has no %s secret of that name", name, in.Owner, in.Repo, where)))
					}
				}
			}
		}
	}
	// on-filter-no-branch.
	if f.On != nil {
		var branches []string
		branchesOK := false
		for _, ev := range f.On.Events {
			for _, k := range ev.Keys {
				if k.Name != "branches" {
					continue
				}
				vals := k.List
				if k.Scalar != nil {
					vals = []workflow.Scalar{*k.Scalar}
				}
				for _, v := range vals {
					if v.IsExpr() || strings.ContainsAny(v.Value, "*?[") {
						continue
					}
					if !branchesOK {
						branches, branchesOK = c.Branches(in.Owner, in.Repo)
						if !branchesOK {
							break
						}
					}
					if !contains(branches, v.Value) {
						out = append(out, rules.Finding("on-filter-no-branch", v.Pos, fmt.Sprintf("on.%s.branches names %q, which %s/%s does not have", ev.Name, v.Value, in.Owner, in.Repo)))
					}
				}
			}
		}
	}
	// uses-archived and uses-deprecated-tag (per-action lookups, memoised
	// per owner/repo within the run; off with enrich.actions: false).
	if !c.Actions {
		return out
	}
	seenRepo := map[string]bool{}
	for _, u := range f.Uses {
		ref, err := uses.Parse(u.Raw)
		if err != nil || ref.Kind != uses.External {
			continue
		}
		if reSHA.MatchString(ref.Ref) {
			continue // a SHA pin is not a major; no release lookup (N1)
		}
		key := strings.ToLower(ref.Owner + "/" + ref.Repo)
		if archived, ok := c.Archived(ref.Owner, ref.Repo); ok && archived && !seenRepo[key+"@archived"] {
			seenRepo[key+"@archived"] = true
			out = append(out, rules.Finding("uses-archived", u.Pos, fmt.Sprintf("%s/%s is archived; it will receive no fixes", ref.Owner, ref.Repo)))
		}
		if m := reMajor.FindStringSubmatch(ref.Ref); m != nil {
			if latest, ok := c.LatestTag(ref.Owner, ref.Repo); ok {
				if lm := reMajor.FindStringSubmatch(latest); lm != nil {
					have, _ := strconv.Atoi(m[1])
					want, _ := strconv.Atoi(lm[1])
					if want-have > 1 {
						out = append(out, rules.Finding("uses-deprecated-tag", u.Pos, fmt.Sprintf("%s/%s@%s is %d majors behind the latest release %s", ref.Owner, ref.Repo, ref.Ref, want-have, latest)))
					}
				}
			}
		}
	}
	return out
}

func labelInMessage(msg string, labels []string) bool {
	for _, l := range labels {
		if strings.Contains(msg, fmt.Sprintf("runner label %q", l)) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
