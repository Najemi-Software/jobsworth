// Package contextschema holds the static shapes of the runner, job and
// github contexts (medium §6.2.5), including curated github.event payloads
// per trigger. Payload objects are open by default: GitHub adds fields.
package contextschema

import (
	"fmt"
	"sort"
	"sync"

	"github.com/najemi-software/jobsworth/internal/shape"
)

func str() *shape.Shape { return shape.StringShape() }
func strIn(values ...string) *shape.Shape {
	return shape.WithValues(shape.StringShape(), values...)
}
func num() *shape.Shape   { return shape.NumberShape() }
func boolS() *shape.Shape { return shape.BoolShape() }

func open(fields map[string]*shape.Shape) *shape.Shape { return shape.OpenObject(fields) }

// Runner is the runner context.
func Runner() *shape.Shape {
	return shape.ObjectShape(map[string]*shape.Shape{
		"name": str(), "os": strIn("Linux", "Windows", "macOS"), "arch": strIn("X86", "X64", "ARM", "ARM64"), "temp": str(), "tool_cache": str(),
		"debug": str(), "environment": str(),
	})
}

// Job is the job context.
func Job() *shape.Shape {
	return shape.ObjectShape(map[string]*shape.Shape{
		"status":    strIn("success", "failure", "cancelled"),
		"container": open(map[string]*shape.Shape{"id": str(), "network": str()}),
		"services":  shape.UnknownObj(),
	})
}

// user is the shape of a GitHub user object.
func user() *shape.Shape {
	return open(map[string]*shape.Shape{
		"login": str(), "id": num(), "node_id": str(), "avatar_url": str(), "html_url": str(),
		"type": str(), "site_admin": boolS(), "name": str(), "email": str(),
	})
}

func repository() *shape.Shape {
	return open(map[string]*shape.Shape{
		"id": num(), "node_id": str(), "name": str(), "full_name": str(), "private": boolS(),
		"owner": user(), "html_url": str(), "description": str(), "fork": boolS(), "url": str(),
		"default_branch": str(), "clone_url": str(), "ssh_url": str(), "git_url": str(),
		"visibility": str(), "archived": boolS(), "disabled": boolS(), "topics": shape.ArrayOf(str()),
		"pushed_at": str(), "created_at": str(), "updated_at": str(), "master_branch": str(),
	})
}

func commit() *shape.Shape {
	return open(map[string]*shape.Shape{
		"id": str(), "tree_id": str(), "distinct": boolS(), "message": str(), "timestamp": str(), "url": str(),
		"author":    open(map[string]*shape.Shape{"name": str(), "email": str(), "username": str()}),
		"committer": open(map[string]*shape.Shape{"name": str(), "email": str(), "username": str()}),
		"added":     shape.ArrayOf(str()), "removed": shape.ArrayOf(str()), "modified": shape.ArrayOf(str()),
	})
}

func pullRequest() *shape.Shape {
	ref := open(map[string]*shape.Shape{"label": str(), "ref": str(), "sha": str(), "user": user(), "repo": repository()})
	return open(map[string]*shape.Shape{
		"id": num(), "node_id": str(), "number": num(), "state": str(), "locked": boolS(), "title": str(),
		"body": str(), "user": user(), "html_url": str(), "url": str(), "diff_url": str(), "patch_url": str(),
		"draft": boolS(), "merged": boolS(), "mergeable": boolS(), "merged_at": str(), "merge_commit_sha": str(),
		"head": ref, "base": ref, "labels": shape.ArrayOf(open(map[string]*shape.Shape{"id": num(), "name": str(), "color": str(), "description": str()})),
		"assignees": shape.ArrayOf(user()), "requested_reviewers": shape.ArrayOf(user()),
		"created_at": str(), "updated_at": str(), "closed_at": str(), "commits": num(), "additions": num(), "deletions": num(), "changed_files": num(),
		"author_association": str(), "milestone": shape.UnknownObj(),
	})
}

func issue() *shape.Shape {
	return open(map[string]*shape.Shape{
		"id": num(), "node_id": str(), "number": num(), "title": str(), "body": str(), "state": str(),
		"user": user(), "html_url": str(), "url": str(), "locked": boolS(),
		"labels":    shape.ArrayOf(open(map[string]*shape.Shape{"id": num(), "name": str(), "color": str()})),
		"assignees": shape.ArrayOf(user()), "created_at": str(), "updated_at": str(), "closed_at": str(),
		"pull_request": shape.UnknownObj(), "author_association": str(),
	})
}

func common() map[string]*shape.Shape {
	return map[string]*shape.Shape{
		"repository": repository(), "sender": user(), "organization": shape.UnknownObj(),
		"installation": shape.UnknownObj(), "enterprise": shape.UnknownObj(),
	}
}

func merge(base map[string]*shape.Shape, extra map[string]*shape.Shape) map[string]*shape.Shape {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// Payloads are the curated github.event shapes per trigger name.
var Payloads = map[string]func() *shape.Shape{
	"push": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{
			"ref": str(), "before": str(), "after": str(), "created": boolS(), "deleted": boolS(), "forced": boolS(),
			"base_ref": str(), "compare": str(), "commits": shape.ArrayOf(commit()), "head_commit": commit(),
			"pusher": open(map[string]*shape.Shape{"name": str(), "email": str()}),
		}))
	},
	"pull_request":        pullRequestPayload,
	"pull_request_target": pullRequestPayload,
	"workflow_dispatch": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{"ref": str(), "inputs": shape.UnknownObj(), "workflow": str()}))
	},
	"workflow_call": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{"inputs": shape.UnknownObj()}))
	},
	"schedule": func() *shape.Shape { return open(merge(common(), map[string]*shape.Shape{"schedule": str()})) },
	"release": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{
			"action": str(),
			"release": open(map[string]*shape.Shape{
				"id": num(), "tag_name": str(), "target_commitish": str(), "name": str(), "body": str(),
				"draft": boolS(), "prerelease": boolS(), "html_url": str(), "url": str(), "upload_url": str(),
				"author": user(), "created_at": str(), "published_at": str(),
				"assets": shape.ArrayOf(open(map[string]*shape.Shape{"id": num(), "name": str(), "browser_download_url": str(), "size": num(), "content_type": str()})),
			}),
		}))
	},
	"issues": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{
			"action": str(), "issue": issue(), "changes": shape.UnknownObj(), "assignee": user(),
			"label": open(map[string]*shape.Shape{"id": num(), "name": str(), "color": str()}),
		}))
	},
	"issue_comment": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{
			"action": str(), "issue": issue(),
			"comment": open(map[string]*shape.Shape{"id": num(), "body": str(), "user": user(), "html_url": str(), "created_at": str(), "updated_at": str(), "author_association": str()}),
		}))
	},
	"workflow_run": func() *shape.Shape {
		return open(merge(common(), map[string]*shape.Shape{
			"action":   str(),
			"workflow": open(map[string]*shape.Shape{"id": num(), "name": str(), "path": str(), "state": str()}),
			"workflow_run": open(map[string]*shape.Shape{
				"id": num(), "name": str(), "head_branch": str(), "head_sha": str(), "run_number": num(), "run_attempt": num(),
				"event": str(), "status": str(), "conclusion": str(), "workflow_id": num(), "html_url": str(), "url": str(),
				"created_at": str(), "updated_at": str(), "head_repository": repository(), "repository": repository(),
				"actor": user(), "triggering_actor": user(), "pull_requests": shape.ArrayOf(shape.UnknownObj()),
			}),
		}))
	},
}

func pullRequestPayload() *shape.Shape {
	return open(merge(common(), map[string]*shape.Shape{
		"action": str(), "number": num(), "pull_request": pullRequest(),
		"label":  open(map[string]*shape.Shape{"id": num(), "name": str(), "color": str()}),
		"before": str(), "after": str(), "requested_reviewer": user(),
	}))
}

// PayloadNames lists the curated triggers, sorted.
func PayloadNames() []string {
	var out []string
	for k := range Payloads {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Event returns the github.event shape for a file's on: events (medium
// §6.2.5): one event gives its payload; several give the union, where a
// field present in any is accessible; an unknown event gives an unknown
// object. When eventOpen is false, payload objects are closed.
func Event(events []string, eventOpen bool) *shape.Shape {
	if len(events) == 0 {
		return shape.UnknownObj()
	}
	var out *shape.Shape
	for _, ev := range events {
		mk, ok := Payloads[ev]
		if !ok {
			return shape.UnknownObj()
		}
		out = shape.Union(out, mk())
	}
	if !eventOpen {
		out = closeAll(out)
	}
	return out
}

func closeAll(s *shape.Shape) *shape.Shape {
	if s == nil || s.Kind != shape.Object {
		return s
	}
	c := *s
	c.Open = false
	c.Fields = map[string]*shape.Shape{}
	for k, f := range s.Fields {
		c.Fields[k] = closeAll(f)
	}
	return &c
}

var githubCache sync.Map // key -> *shape.Shape

// GitHub is the github context for a file with the given events. The tree
// is built once per (events, open) key and shared: nothing mutates shapes
// after construction (medium review N3).
func GitHub(events []string, eventOpen bool) *shape.Shape {
	key := fmt.Sprintf("%v|%v", events, eventOpen)
	if v, ok := githubCache.Load(key); ok {
		return v.(*shape.Shape)
	}
	s := buildGitHub(events, eventOpen)
	githubCache.Store(key, s)
	return s
}

// eventName is github.event_name: bounded by the file's own triggers, and
// unknowable in a reusable workflow (the caller's event).
func eventName(events []string) *shape.Shape {
	var own []string
	for _, ev := range events {
		if ev == "workflow_call" {
			return str()
		}
		own = append(own, ev)
	}
	if len(own) == 0 {
		return str()
	}
	return strIn(own...)
}

func buildGitHub(events []string, eventOpen bool) *shape.Shape {
	return open(map[string]*shape.Shape{
		"event": Event(events, eventOpen), "event_name": eventName(events), "event_path": str(),
		"ref": str(), "ref_name": str(), "ref_type": strIn("branch", "tag"), "ref_protected": boolS(), "sha": str(),
		"actor": str(), "actor_id": str(), "triggering_actor": str(),
		"repository": str(), "repository_owner": str(), "repository_owner_id": str(), "repository_id": str(), "repositoryUrl": str(),
		"run_id": str(), "run_number": str(), "run_attempt": str(), "retention_days": str(),
		"workflow": str(), "workflow_ref": str(), "workflow_sha": str(), "job": str(),
		"action": str(), "action_path": str(), "action_ref": str(), "action_repository": str(), "action_status": str(),
		"workspace": str(), "token": str(), "head_ref": str(), "base_ref": str(),
		"server_url": str(), "api_url": str(), "graphql_url": str(), "path": str(), "env": str(),
		"secret_source": str(),
	})
}
