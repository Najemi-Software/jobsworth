package initialize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/uses"
)

// ProbeResult is one reference that could not be fetched (§3.3).
type ProbeResult struct {
	Base   string
	Ref    string
	Status int // 401, 403, or 404
	Files  int
	// Skip is the slug to write under skip.actions/skip.workflows.
	Skip       string
	IsWorkflow bool
	// Linked marks a reference into a repo that has a local checkout: it
	// is never offered as a skip, but a 401/403 means CI's GITHUB_TOKEN
	// cannot fetch it (spec-init §5).
	Linked bool
}

// Probe resolves every external reference through the normal fetch path.
// The repo's own references are skipped (check links them implicitly);
// references into linked repos are probed and tagged Linked so a private
// one can be named in the generated workflow. It returns the unfetchable
// ones, grouped by status, and a non-nil abort error when a rate limit or
// network error stopped the probe (§3.3).
func Probe(in Inspection, client *fetch.Client, linked []Checkout) (results []ProbeResult, abort error) {
	for _, r := range SortedRefs(in) {
		ref, err := uses.Parse(r.Base + "@" + r.Refs[0])
		if err != nil {
			continue
		}
		slug := ref.Owner + "/" + ref.Repo
		if in.Self != "" && strings.EqualFold(slug, in.Self) {
			continue
		}
		isLinkedRepo := isLinked(linked, slug)
		for _, rv := range r.Refs {
			status, err := probeOne(client, ref, rv)
			if err != nil {
				return results, err
			}
			if status == 0 {
				continue
			}
			results = append(results, ProbeResult{
				Base: r.Base, Ref: rv, Status: status, Files: r.Files,
				Skip: uses.SlugNoRef(ref), IsWorkflow: ref.IsWorkflow, Linked: isLinkedRepo,
			})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Status != results[j].Status {
			return results[i].Status < results[j].Status
		}
		return strings.ToLower(results[i].Base) < strings.ToLower(results[j].Base)
	})
	return results, nil
}

// probeOne returns 0 on success, the HTTP-ish status for 401/403/404, or an
// error for conditions that abort the probe.
func probeOne(client *fetch.Client, ref uses.Ref, rv string) (int, error) {
	resolved, err := fetch.ResolveRef(client, ref.Owner, ref.Repo, rv)
	if err != nil {
		return classifyProbe(err)
	}
	var candidates []string
	if ref.IsWorkflow {
		candidates = []string{ref.Path}
	} else {
		base := ref.Path
		if base != "" {
			base += "/"
		}
		candidates = []string{base + "action.yml", base + "action.yaml"}
	}
	var last error
	for _, c := range candidates {
		if _, err := fetch.Content(client, ref.Owner, ref.Repo, resolved.SHA, c); err == nil {
			return 0, nil
		} else {
			last = err
			if k, _ := fetch.KindOf(err); k != fetch.NotFound {
				break
			}
		}
	}
	return classifyProbe(last)
}

func classifyProbe(err error) (int, error) {
	var fe *fetch.Error
	kind, ok := fetch.KindOf(err)
	if !ok {
		return 0, err
	}
	switch kind {
	case fetch.NotFound:
		return 404, nil
	case fetch.Forbidden:
		if asFetchError(err, &fe) && fe.Status == 401 {
			return 401, nil
		}
		return 403, nil
	case fetch.RateLimited:
		return 0, fmt.Errorf("probe aborted: %v", err)
	case fetch.Offline:
		return 0, fmt.Errorf("probe aborted: offline")
	default:
		return 0, fmt.Errorf("probe aborted: %v", err)
	}
}

func isLinked(links []Checkout, slug string) bool {
	for _, l := range links {
		if strings.EqualFold(l.Slug, slug) {
			return true
		}
	}
	return false
}

func asFetchError(err error, into **fetch.Error) bool {
	for e := err; e != nil; {
		if fe, ok := e.(*fetch.Error); ok {
			*into = fe
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
