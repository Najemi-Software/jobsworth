package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/najemi-software/jobsworth/internal/bash"
	"github.com/najemi-software/jobsworth/internal/checkout"
	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/uses"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

// Family performance (advanced §9): checkouts nothing reads, jobs that
// could be one, uploads of the whole workspace.

// PerformanceInputs is what CheckPerformance knows beyond the file.
type PerformanceInputs struct {
	// FSCleanActions are non-composite actions configured as not touching
	// the filesystem.
	FSCleanActions []string
	// Checkouts are the effective checkouts per job and step
	// (checkout-inference §3); nil falls back to direct checkouts.
	Checkouts map[string]Checkouts
}

var reTouchesTree = regexp.MustCompile(`(^|[\s;&|(=])\./|(^|[\s;&|(])git\s|\$GITHUB_WORKSPACE|github\.workspace|\$\{GITHUB_WORKSPACE`)

// cleanRun reports whether a run: body is filesystem-clean (§9): only
// echo, printf, assignments and $GITHUB_* writes, no relative paths, no
// git, no workspace reference, no opaque call.
func cleanRun(run string) bool {
	if reTouchesTree.MatchString(run) {
		return false
	}
	a := bash.Analyse(run, false, nil)
	if a.ParseErr != nil {
		return false
	}
	for _, s := range a.Statements {
		switch {
		case s.Class == "echo" || s.Class == "printf" || s.Class == "assignment" || s.Class == "export":
			if !cleanSubstitutions(s.Text) {
				return false // a command substitution may read the tree (medium review 8 D18)
			}
		case strings.HasPrefix(s.Class, "opaque"):
			return false
		default:
			return false
		}
		if s.Redirect != "" && !strings.Contains(s.Redirect, "GITHUB_") {
			return false
		}
	}
	return true
}

var reSubst = regexp.MustCompile("\\$\\(\\s*([^\\s()]+)|`\\s*([^\\s`]+)")

// cleanSubstitutions reports whether every command substitution in an
// assignment's text runs only echo or printf; any other command may read
// the tree.
func cleanSubstitutions(text string) bool {
	for _, m := range reSubst.FindAllStringSubmatch(text, -1) {
		cmd := m[1] + m[2]
		if cmd != "echo" && cmd != "printf" {
			return false
		}
	}
	return true
}

// CleanStep is cleanStep for callers outside the package (the checkout
// profiler's TrailingClean): fsClean are the configured clean actions.
func CleanStep(st workflow.Step, callee *workflow.File, fsClean []string) bool {
	return cleanStep(st, callee, PerformanceInputs{FSCleanActions: fsClean}, 0)
}

// cleanStep reports whether a step is filesystem-clean; callee resolves a
// uses: to its composite file when loaded.
func cleanStep(st workflow.Step, callee *workflow.File, in PerformanceInputs, depth int) bool {
	if st.HasRun {
		return cleanRun(st.Run)
	}
	if st.Uses == "" {
		return true
	}
	if ref, err := uses.Parse(st.Uses); err == nil && ref.Kind == uses.Local {
		return false // loading a ./ action reads the tree
	}
	for _, a := range in.FSCleanActions {
		if strings.HasPrefix(strings.ToLower(st.Uses), strings.ToLower(a)) {
			return true
		}
	}
	if callee == nil || callee.Kind != workflow.KindCompositeAction || depth > 3 {
		return false
	}
	for _, s := range callee.Steps {
		if !cleanStep(s, nil, in, depth+1) {
			return false
		}
	}
	return true
}

// CheckPerformance implements §9 over one file.
func CheckPerformance(f *workflow.File, scopes map[string]*Scope, in PerformanceInputs) []finding.Finding {
	var out []finding.Finding
	for _, sc := range scopes {
		out = append(out, checkCheckouts(sc, in, checkoutsOf(in.Checkouts, f, sc.Job), f.Kind == workflow.KindCompositeAction)...)
		out = append(out, checkSetupCache(sc)...)
		for _, st := range sc.Steps {
			if isUploadArtifact(st.Uses) {
				for _, w := range st.With {
					v := strings.TrimSpace(w.Value.Value)
					if w.Name == "path" && (v == "." || v == "./" || v == "${{ github.workspace }}" || v == "$GITHUB_WORKSPACE") {
						out = append(out, at("artifact-whole-workspace", w.Value.Pos, fmt.Sprintf("step %s uploads the whole workspace (path: %s); name the files the artifact needs", StepLabel(st), v)))
					}
				}
			}
		}
	}
	if f.Kind == workflow.KindWorkflow {
		out = append(out, checkMergeable(f)...)
	}
	return out
}

// checkCheckouts reports the checkout findings of one scope. In a
// composite action the caller's later steps read the tree, so only
// checkout-redundant applies there (medium review 8 D16).
func checkCheckouts(sc *Scope, in PerformanceInputs, cos Checkouts, composite bool) []finding.Finding {
	var out []finding.Finding
	var seen []checkout.Step
	for _, st := range sc.Steps {
		entries := cos[st.Index]
		if len(entries) == 0 {
			continue
		}
		known, trailingClean := false, true
		for _, cs := range entries {
			known = known || !cs.Unknown
			trailingClean = trailingClean && (cs.Unknown || cs.TrailingClean)
		}
		if !known {
			continue
		}
		label := StepLabel(st)
		if composite || !trailingClean {
			// Inside a wrapper, later steps already read the tree (review 1
			// D2); in a composite action the caller's steps do.
			// fall through to the redundancy check only
		} else if st.Index == len(sc.Steps)-1 {
			out = append(out, at("checkout-trailing", st.UsesPos, fmt.Sprintf("checkout %s is the last step of the job; nothing reads the tree", label)))
		} else {
			allClean := true
			for _, later := range sc.Steps {
				if later.Index <= st.Index {
					continue
				}
				if !cleanStep(later, sc.Callees[later.Index], in, 0) {
					allClean = false
					break
				}
			}
			if allClean {
				out = append(out, at("checkout-unused", st.UsesPos, fmt.Sprintf("checkout %s is followed only by steps that do not touch the filesystem; drop it and save the clone", label)))
			}
		}
		for _, cs := range entries {
			if cs.Unknown || cs.Target != checkout.Root {
				continue
			}
			for _, prev := range seen {
				if prev.Repository == cs.Repository && prev.Ref == cs.Ref && !prev.RefDyn && !cs.RefDyn && !prev.Conditional && !cs.Conditional && sparseSubset(cs.Sparse, prev.Sparse) {
					out = append(out, at("checkout-redundant", st.UsesPos, fmt.Sprintf("checkout %s repeats step #%d's checkout of the same repository and ref to the root; the first already provides everything this one does", label, prev.Index+1)))
					break
				}
			}
			seen = append(seen, cs)
		}
	}
	return out
}

// sparseSubset reports whether second's sparse list is covered by first's
// (nil means the whole tree).
func sparseSubset(second, first *checkout.Sparse) bool {
	if first == nil {
		return true
	}
	if second == nil {
		return false
	}
	for _, e := range second.Entries {
		if !checkout.SparseCovers(*first, strings.TrimSuffix(e, "/")) {
			return false
		}
	}
	return true
}

var reInstalls = regexp.MustCompile(`(^|[\s;&|(])(npm (ci|install)|yarn( install)?\b|pnpm install|pip3? install|poetry install|go mod download|go build|gradle|\./gradlew|mvn)`)

func checkSetupCache(sc *Scope) []finding.Finding {
	var out []finding.Finding
	installs := false
	for _, st := range sc.Steps {
		if st.HasRun && reInstalls.MatchString(st.Run) {
			installs = true
		}
	}
	if !installs {
		return nil
	}
	for _, st := range sc.Steps {
		u := strings.ToLower(st.Uses)
		if !(strings.HasPrefix(u, "actions/setup-node@") || strings.HasPrefix(u, "actions/setup-python@") || strings.HasPrefix(u, "actions/setup-go@") || strings.HasPrefix(u, "actions/setup-java@")) {
			continue
		}
		if !withHas(st, "cache") {
			out = append(out, at("setup-no-cache", st.UsesPos, fmt.Sprintf("%s runs without cache: in a job that installs packages; the built-in cache saves the install", st.Uses)))
		}
	}
	return out
}

// checkMergeable is jobs-mergeable: A's only consumer is B and the two
// could be one job.
func checkMergeable(f *workflow.File) []finding.Finding {
	var out []finding.Finding
	consumers := map[string][]string{}
	for _, j := range f.Jobs {
		for _, n := range j.Needs {
			consumers[n.Name] = append(consumers[n.Name], j.Name)
		}
	}
	jobEnv := func(j *workflow.Job) string {
		runs := ""
		if j.RunsOn != nil {
			for _, l := range j.RunsOn.Labels {
				runs += l.Value + ","
			}
			runs += j.RunsOn.Group.Value
		}
		return runs + "|" + j.Container + "|" + j.Services + "|" + j.DefaultShell
	}
	for i := range f.Jobs {
		a := &f.Jobs[i]
		// A job with outputs needs its plumbing rewritten to merge; that is
		// not the one-runner saving this rule is about.
		if a.Uses != nil || a.Matrix != nil || a.If.Value != "" || len(a.Outputs) > 0 || len(consumers[a.Name]) != 1 {
			continue
		}
		b := workflow.JobByName(f, consumers[a.Name][0])
		if b == nil || b.Name == a.Name || b.Uses != nil || b.Matrix != nil || b.If.Value != "" || len(b.Needs) != 1 || len(a.Needs) > 0 || jobEnv(a) != jobEnv(b) {
			continue
		}
		exposed := false
		for _, o := range f.Outputs {
			if strings.Contains(o.Value, "jobs."+a.Name+".outputs") {
				exposed = true
			}
		}
		if exposed {
			continue
		}
		out = append(out, at("jobs-mergeable", b.Pos, fmt.Sprintf("job '%s' only exists to run after '%s' on the same runner; one job with both step lists saves a runner spin-up", b.Name, a.Name)))
	}
	return out
}
