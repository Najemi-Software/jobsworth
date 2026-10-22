package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/strutil"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family triggers (advanced §2): on: as data, cron, dispatch and call
// input declarations, concurrency.

// EventSpec is one documented trigger: its activity types and the filter
// keys it accepts. Structural keys (inputs, outputs, secrets, workflows)
// are listed per event too.
type EventSpec struct {
	Types []string
	Keys  []string
}

var branchPathKeys = []string{"branches", "branches-ignore", "paths", "paths-ignore"}

// filterKeys are the pattern-list keys; the other allowed keys (inputs,
// outputs, secrets, types) are structural mappings.
var filterKeys = map[string]bool{"branches": true, "branches-ignore": true, "paths": true, "paths-ignore": true, "tags": true, "tags-ignore": true, "workflows": true}

// Events is the curated trigger table. A test pins that every event with
// a payload schema in contextschema is listed here.
var Events = map[string]EventSpec{
	"branch_protection_rule":      {Types: []string{"created", "edited", "deleted"}},
	"check_run":                   {Types: []string{"created", "rerequested", "completed", "requested_action"}},
	"check_suite":                 {Types: []string{"completed"}},
	"create":                      {},
	"delete":                      {},
	"deployment":                  {},
	"deployment_status":           {},
	"discussion":                  {Types: []string{"created", "edited", "deleted", "transferred", "pinned", "unpinned", "labeled", "unlabeled", "locked", "unlocked", "category_changed", "answered", "unanswered"}},
	"discussion_comment":          {Types: []string{"created", "edited", "deleted"}},
	"fork":                        {},
	"gollum":                      {},
	"issue_comment":               {Types: []string{"created", "edited", "deleted"}},
	"issues":                      {Types: []string{"opened", "edited", "deleted", "transferred", "pinned", "unpinned", "closed", "reopened", "assigned", "unassigned", "labeled", "unlabeled", "locked", "unlocked", "milestoned", "demilestoned", "typed", "untyped"}},
	"label":                       {Types: []string{"created", "edited", "deleted"}},
	"merge_group":                 {Types: []string{"checks_requested"}},
	"milestone":                   {Types: []string{"created", "closed", "opened", "edited", "deleted"}},
	"page_build":                  {},
	"project":                     {Types: []string{"created", "closed", "reopened", "edited", "deleted"}},
	"project_card":                {Types: []string{"created", "moved", "converted", "edited", "deleted"}},
	"project_column":              {Types: []string{"created", "updated", "moved", "deleted"}},
	"public":                      {},
	"pull_request":                {Types: []string{"assigned", "unassigned", "labeled", "unlabeled", "opened", "edited", "closed", "reopened", "synchronize", "converted_to_draft", "locked", "unlocked", "enqueued", "dequeued", "milestoned", "demilestoned", "ready_for_review", "review_requested", "review_request_removed", "auto_merge_enabled", "auto_merge_disabled"}, Keys: branchPathKeys},
	"pull_request_review":         {Types: []string{"submitted", "edited", "dismissed"}},
	"pull_request_review_comment": {Types: []string{"created", "edited", "deleted"}},
	"pull_request_target":         {Types: []string{"assigned", "unassigned", "labeled", "unlabeled", "opened", "edited", "closed", "reopened", "synchronize", "converted_to_draft", "ready_for_review", "locked", "unlocked", "review_requested", "review_request_removed", "auto_merge_enabled", "auto_merge_disabled"}, Keys: branchPathKeys},
	"push":                        {Keys: []string{"branches", "branches-ignore", "tags", "tags-ignore", "paths", "paths-ignore"}},
	"registry_package":            {Types: []string{"published", "updated"}},
	"release":                     {Types: []string{"published", "unpublished", "created", "edited", "deleted", "prereleased", "released"}},
	"repository_dispatch":         {Types: nil, Keys: []string{"types"}},
	"schedule":                    {},
	"status":                      {},
	"watch":                       {Types: []string{"started"}},
	"workflow_call":               {Keys: []string{"inputs", "outputs", "secrets"}},
	"workflow_dispatch":           {Keys: []string{"inputs"}},
	"workflow_run":                {Types: []string{"completed", "requested", "in_progress"}, Keys: []string{"workflows", "branches", "branches-ignore"}},
}

// EventNames lists the table's events, sorted.
func EventNames() []string {
	var out []string
	for n := range Events {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TriggerInputs is what CheckTriggers knows beyond the file.
type TriggerInputs struct {
	// ExtraEvents are config-added permissive event names.
	ExtraEvents []string
	// WorkflowNames are the name: values of the repo's workflows, for
	// on-workflow-run-unknown; nil when the repo is not local.
	WorkflowNames []string
	HasRepo       bool
}

// CheckTriggers implements advanced §2.1–§2.4 over one workflow.
func CheckTriggers(f *workflow.File, in TriggerInputs) []finding.Finding {
	if f.Kind != workflow.KindWorkflow {
		return nil
	}
	var out []finding.Finding
	on := f.On
	if on == nil || on.Missing {
		out = append(out, at("on-missing", workflow.Pos{Line: 1, Column: 1}, "workflow has no on: block; it never runs"))
		return out
	}
	extra := map[string]bool{}
	for _, e := range in.ExtraEvents {
		extra[e] = true
	}
	known := EventNames()
	var pushTags, pushBranches bool
	for _, ev := range on.Events {
		spec, ok := Events[ev.Name]
		if !ok {
			if extra[ev.Name] {
				continue
			}
			hint := ""
			if near := strutil.Nearest(ev.Name, known, 1); len(near) > 0 {
				hint = fmt.Sprintf("; did you mean %s?", near[0])
			}
			out = append(out, at("on-unknown-event", ev.Pos, fmt.Sprintf("on: %q is not a GitHub Actions event%s", ev.Name, hint)))
			continue
		}
		if ev.Name == "workflow_run" {
			hasWorkflows := false
			for _, k := range ev.Keys {
				if k.Name == "workflows" {
					hasWorkflows = true
					out = append(out, workflowRunUnknown(k, in)...)
				}
			}
			if !hasWorkflows {
				out = append(out, at("on-workflow-run-target", ev.Pos, "on.workflow_run has no workflows: list; GitHub requires the triggering workflows to be named"))
			}
		}
		seen := map[string]bool{}
		for _, k := range ev.Keys {
			seen[k.Name] = true
			switch {
			case k.Name == "types":
				out = append(out, checkTypes(ev, k, spec)...)
			case contains(spec.Keys, k.Name):
				if filterKeys[k.Name] {
					out = append(out, checkFilter(ev, k)...)
				}
				if ev.Name == "push" {
					switch k.Name {
					case "tags":
						pushTags = true
					case "branches":
						pushBranches = true
					}
				}
			default:
				allowed := append([]string{"types"}, spec.Keys...)
				hint := ""
				if near := strutil.Nearest(k.Name, allowed, 1); len(near) > 0 && len(allowed) > 0 {
					hint = fmt.Sprintf("; did you mean %s?", near[0])
				}
				out = append(out, at("on-unknown-key", k.Pos, fmt.Sprintf("on.%s.%s is not a key the %s event accepts (allowed: %s)%s", ev.Name, k.Name, ev.Name, orNoneList(allowed), hint)))
			}
		}
		for _, pair := range [][2]string{{"branches", "branches-ignore"}, {"paths", "paths-ignore"}, {"tags", "tags-ignore"}} {
			if seen[pair[0]] && seen[pair[1]] {
				out = append(out, at("on-filter-conflict", ev.Pos, fmt.Sprintf("on.%s declares both %s and %s; GitHub rejects the workflow, use one", ev.Name, pair[0], pair[1])))
			}
		}
	}
	if pushTags && !pushBranches {
		for _, ev := range on.Events {
			if ev.Name == "push" {
				out = append(out, at("on-push-tags-only", ev.Pos, "on.push has tags: but no branches:, so branch pushes no longer trigger this workflow; add branches: if both were meant"))
			}
		}
	}
	out = append(out, checkCrons(f)...)
	out = append(out, checkInputDecls(f)...)
	out = append(out, checkConcurrency(f)...)
	return out
}

func workflowRunUnknown(k workflow.OnKey, in TriggerInputs) []finding.Finding {
	if !in.HasRepo {
		return nil
	}
	var out []finding.Finding
	vals := k.List
	if k.Scalar != nil {
		vals = []workflow.Scalar{*k.Scalar}
	}
	for _, v := range vals {
		if v.IsExpr() || contains(in.WorkflowNames, v.Value) {
			continue
		}
		hint := ""
		if near := strutil.Nearest(v.Value, in.WorkflowNames, 1); len(near) > 0 && len(in.WorkflowNames) > 0 {
			hint = fmt.Sprintf("; did you mean %q?", near[0])
		}
		out = append(out, at("on-workflow-run-unknown", v.Pos, fmt.Sprintf("on.workflow_run.workflows names %q, but no workflow in this repository has that name:%s", v.Value, hint)))
	}
	return out
}

func checkTypes(ev workflow.Event, k workflow.OnKey, spec EventSpec) []finding.Finding {
	var out []finding.Finding
	vals := k.List
	if k.Scalar != nil {
		vals = []workflow.Scalar{*k.Scalar}
	}
	if ev.Name == "repository_dispatch" {
		return nil // free-form custom types
	}
	for _, v := range vals {
		if contains(spec.Types, v.Value) {
			continue
		}
		hint := ""
		if near := strutil.Nearest(v.Value, spec.Types, 1); len(near) > 0 && len(spec.Types) > 0 {
			hint = fmt.Sprintf("; did you mean %s?", near[0])
		}
		msg := fmt.Sprintf("on.%s.types: %q is not an activity type of %s (allowed: %s)%s; GitHub ignores it and the workflow never fires for the intended type", ev.Name, v.Value, ev.Name, orNoneList(spec.Types), hint)
		if len(spec.Types) == 0 {
			msg = fmt.Sprintf("on.%s has no activity types, so types: is ignored", ev.Name)
		}
		out = append(out, at("on-unknown-type", v.Pos, msg))
	}
	return out
}

// checkFilter is on-filter-shape, on-filter-expression, on-filter-glob
// and on-filter-empty over one branches/paths/tags/workflows key.
func checkFilter(ev workflow.Event, k workflow.OnKey) []finding.Finding {
	var out []finding.Finding
	if k.IsMapping || k.Other {
		return append(out, at("on-filter-shape", k.Pos, fmt.Sprintf("on.%s.%s must be a string or a list of strings", ev.Name, k.Name)))
	}
	if k.Empty {
		return append(out, at("on-filter-empty", k.Pos, fmt.Sprintf("on.%s.%s is an empty list; it matches nothing and the workflow never runs on %s", ev.Name, k.Name, ev.Name)))
	}
	vals := k.List
	if k.Scalar != nil {
		vals = []workflow.Scalar{*k.Scalar}
	}
	for _, v := range vals {
		if v.Tag != "" && v.Tag != "!!str" && v.Tag != "!!int" && v.Tag != "!!float" {
			out = append(out, at("on-filter-shape", v.Pos, fmt.Sprintf("on.%s.%s: %q is not a string pattern", ev.Name, k.Name, v.Value)))
			continue
		}
		if v.IsExpr() {
			out = append(out, at("on-filter-expression", v.Pos, fmt.Sprintf("on.%s.%s contains ${{ }}; expressions are not evaluated in on: and the pattern is taken literally", ev.Name, k.Name)))
			continue
		}
		if msg := globProblem(v.Value); msg != "" {
			out = append(out, at("on-filter-glob", v.Pos, fmt.Sprintf("on.%s.%s: pattern %q %s", ev.Name, k.Name, v.Value, msg)))
		}
	}
	return out
}

// globProblem reports an unbalanced bracket or a trailing backslash.
func globProblem(p string) string {
	if strings.HasSuffix(p, "\\") {
		return "ends with a backslash, which escapes nothing"
	}
	depth := 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		}
	}
	if depth > 0 {
		return "has an unbalanced ["
	}
	return ""
}

// --- cron (§2.2) ----------------------------------------------------------------

var monthNames = map[string]int{"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12}
var dayNames = map[string]int{"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6}

type cronField struct {
	min, max int
	names    map[string]int
	label    string
}

var cronFields = []cronField{
	{0, 59, nil, "minute"}, {0, 23, nil, "hour"}, {1, 31, nil, "day-of-month"}, {1, 12, monthNames, "month"}, {0, 6, dayNames, "day-of-week"},
}

func checkCrons(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	for _, c := range f.Crons {
		if c.Invalid {
			out = append(out, at("cron-syntax", c.Pos, "on.schedule entries must be `- cron: '<expression>'` mappings"))
			continue
		}
		sets, err := ParseCron(c.Expr)
		if err != "" {
			out = append(out, at("cron-syntax", c.Pos, fmt.Sprintf("cron %q: %s", c.Expr, err)))
			continue
		}
		if !c.Quoted {
			out = append(out, at("cron-quoted", c.Pos, fmt.Sprintf("cron %s is not quoted; `* * * * *` would not parse as YAML, quoting is the convention", c.Expr)))
		}
		if gap := minGap(sets[0], 60); gap < 5 {
			out = append(out, at("cron-too-frequent", c.Pos, fmt.Sprintf("cron %q fires every %d minute(s); GitHub throttles schedules to every 5 minutes at most", c.Expr, gap)))
		}
		if msg := impossibleDate(sets[2], sets[3]); msg != "" {
			out = append(out, at("cron-impossible-date", c.Pos, fmt.Sprintf("cron %q %s", c.Expr, msg)))
		}
	}
	return out
}

// ParseCron expands each field into its value set; err names the problem.
func ParseCron(expr string) ([]map[int]bool, string) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Sprintf("expected 5 fields, got %d", len(fields))
	}
	var sets []map[int]bool
	for i, fld := range fields {
		spec := cronFields[i]
		set := map[int]bool{}
		for _, part := range strings.Split(fld, ",") {
			lo, hi, step := spec.min, spec.max, 1
			rng := part
			if s := strings.SplitN(part, "/", 2); len(s) == 2 {
				rng = s[0]
				n, err := strconv.Atoi(s[1])
				if err != nil || n < 1 {
					return nil, fmt.Sprintf("%s field: bad step %q", spec.label, part)
				}
				step = n
			}
			if rng != "*" {
				bounds := strings.SplitN(rng, "-", 2)
				a, ok := cronValue(bounds[0], spec)
				if !ok {
					return nil, fmt.Sprintf("%s field: %q is not a value in %d-%d", spec.label, part, spec.min, spec.max)
				}
				lo, hi = a, a
				if len(bounds) == 2 {
					b, ok := cronValue(bounds[1], spec)
					if !ok || b < a {
						return nil, fmt.Sprintf("%s field: bad range %q", spec.label, part)
					}
					hi = b
				} else if strings.Contains(part, "/") {
					hi = spec.max
				}
			}
			for v := lo; v <= hi; v += step {
				set[v] = true
			}
		}
		sets = append(sets, set)
	}
	return sets, ""
}

func cronValue(s string, spec cronField) (int, bool) {
	if spec.names != nil {
		if v, ok := spec.names[strings.ToUpper(s)]; ok {
			return v, true
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < spec.min || n > spec.max {
		if spec.label == "day-of-week" && n == 7 {
			return 0, true
		}
		return 0, false
	}
	return n, true
}

// minGap is the smallest distance between two consecutive minutes in the
// set, wrapping at modulo; 60 for a single value.
func minGap(set map[int]bool, modulo int) int {
	var vals []int
	for v := range set {
		vals = append(vals, v)
	}
	sort.Ints(vals)
	if len(vals) <= 1 {
		return modulo
	}
	gap := modulo
	for i := range vals {
		next := vals[(i+1)%len(vals)]
		d := next - vals[i]
		if d <= 0 {
			d += modulo
		}
		if d < gap {
			gap = d
		}
	}
	return gap
}

var daysInMonth = map[int]int{1: 31, 2: 29, 3: 31, 4: 30, 5: 31, 6: 30, 7: 31, 8: 31, 9: 30, 10: 31, 11: 30, 12: 31}

// impossibleDate reports a day-of-month no listed month has.
func impossibleDate(days, months map[int]bool) string {
	if len(days) == 31 {
		return "" // every day: nothing to check
	}
	maxDays := 0
	for m := range months {
		if daysInMonth[m] > maxDays {
			maxDays = daysInMonth[m]
		}
	}
	anyPossible := false
	for d := range days {
		if d <= maxDays {
			anyPossible = true
		}
	}
	if !anyPossible {
		return fmt.Sprintf("names a day of month that no listed month has; it never fires")
	}
	if len(days) == 1 && days[29] && len(months) == 1 && months[2] {
		return "fires only on 29 February, every four years"
	}
	return ""
}

// --- dispatch and call input declarations (§2.3) -------------------------------

var dispatchTypes = []string{"string", "boolean", "choice", "environment", "number"}
var callTypes = []string{"string", "boolean", "number"}

func checkInputDecls(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	dispatch := 0
	for _, in := range f.Inputs {
		switch in.Trigger {
		case "workflow_dispatch":
			dispatch++
			if in.Type != "" && !contains(dispatchTypes, in.Type) {
				out = append(out, at("dispatch-input-type", in.Pos, fmt.Sprintf("workflow_dispatch input '%s' has type %q; allowed: %s", in.Name, in.Type, strings.Join(dispatchTypes, ", "))))
			}
		case "workflow_call":
			if in.Type != "" && !contains(callTypes, in.Type) {
				hint := ""
				if in.Type == "choice" {
					hint = " (choice is workflow_dispatch-only; declare a string and validate it in the caller)"
				}
				out = append(out, at("call-input-type-decl", in.Pos, fmt.Sprintf("workflow_call input '%s' has type %q; allowed: %s%s", in.Name, in.Type, strings.Join(callTypes, ", "), hint)))
			}
		}
	}
	if dispatch > 25 {
		out = append(out, at("dispatch-input-limit", f.On.Pos, fmt.Sprintf("workflow_dispatch declares %d inputs; GitHub allows at most 25", dispatch)))
	}
	if f.HasWorkflowDispatch {
		for _, e := range f.Exprs {
			for _, name := range workflow.EventInputRefs(e.Text) {
				out = append(out, at("dispatch-input-event-spelling", e.Pos, fmt.Sprintf("github.event.inputs.%s works, but inputs.%s is the same value and also works under workflow_call", name, name)))
			}
		}
	}
	return out
}

// --- concurrency (§2.4) ---------------------------------------------------------

func checkConcurrency(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	prOrPush := contains(f.Events, "push") || contains(f.Events, "pull_request")
	deploysDefault := false
	for _, ev := range f.On.Events {
		if ev.Name != "push" {
			continue
		}
		for _, k := range ev.Keys {
			if k.Name != "branches" {
				continue
			}
			vals := k.List
			if k.Scalar != nil {
				vals = []workflow.Scalar{*k.Scalar}
			}
			for _, v := range vals {
				if v.Value == "main" || v.Value == "master" {
					deploysDefault = true
				}
			}
		}
	}
	hasEnvironment := false
	for _, j := range f.Jobs {
		if j.Environment != nil {
			hasEnvironment = true
		}
	}
	check := func(c *workflow.Concurrency, where string) {
		if c == nil {
			return
		}
		g := c.Group
		if g.Value != "" && !g.IsExpr() && prOrPush {
			out = append(out, at("concurrency-group-static", g.Pos, fmt.Sprintf("%s concurrency group %q has no expression, so every run across all branches queues behind or cancels every other; ${{ github.workflow }}-${{ github.ref }} is the usual group", where, g.Value)))
		}
		if c.HasCancel && c.CancelInProgress.Value == "true" && strings.Contains(g.Value, "github.ref") && deploysDefault && hasEnvironment {
			out = append(out, at("concurrency-cancel-on-push-main", c.Pos, fmt.Sprintf("%s cancels in-progress runs of a group keyed on github.ref in a workflow that deploys from the default branch; a second push cancels a half-finished deploy", where)))
		}
	}
	check(f.Concurrency, "workflow")
	for _, j := range f.Jobs {
		check(j.Concurrency, "job '"+j.Name+"'")
	}
	return out
}

func orNoneList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
