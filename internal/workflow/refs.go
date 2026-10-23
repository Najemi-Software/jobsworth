package workflow

import "regexp"

// Reference extraction over expression text (§7.1, §7.4). These are regex
// scans, not a full expression parser: enough for the context paths the
// basic rules care about, and deliberately blind to github.event.inputs.*.

// name is one path segment: letters, digits, '-', '_'.
const name = `[A-Za-z0-9_-]+`

// boundary rejects matches preceded by '.' or a name character, so
// `github.event.inputs.x` and `myinputs.x` do not count as `inputs.x`.
const boundary = `(?:^|[^A-Za-z0-9_.-])`

var (
	reInputDot      = regexp.MustCompile(boundary + `inputs\.(` + name + `)`)
	reInputBracket  = regexp.MustCompile(boundary + `inputs\[\s*'([^']*)'\s*\]`)
	reInputBracket2 = regexp.MustCompile(boundary + `inputs\[\s*"([^"]*)"\s*\]`)

	reNeedsOutput  = regexp.MustCompile(boundary + `needs\.(` + name + `)\.outputs\.(` + name + `)`)
	reNeedsOutputB = regexp.MustCompile(boundary + `needs\[\s*'(` + name + `)'\s*\]\.outputs\.(` + name + `)`)
	reNeedsOutputC = regexp.MustCompile(boundary + `needs\.(` + name + `)\.outputs\[\s*'(` + name + `)'\s*\]`)

	reStepOut   = regexp.MustCompile(boundary + `steps\.(` + name + `)\.outputs\.(` + name + `)`)
	reStepOutB  = regexp.MustCompile(boundary + `steps\[\s*'(` + name + `)'\s*\]\.outputs\.(` + name + `)`)
	reStepOutC  = regexp.MustCompile(boundary + `steps\.(` + name + `)\.outputs\[\s*'(` + name + `)'\s*\]`)
	reStepField = regexp.MustCompile(boundary + `steps\.(` + name + `)\.(outcome|conclusion)\b`)

	reJobsOutput  = regexp.MustCompile(boundary + `jobs\.(` + name + `)\.outputs\.(` + name + `)`)
	reJobsOutputB = regexp.MustCompile(boundary + `jobs\[\s*'(` + name + `)'\s*\]\.outputs\.(` + name + `)`)
	reJobsOutputC = regexp.MustCompile(boundary + `jobs\.(` + name + `)\.outputs\[\s*'(` + name + `)'\s*\]`)
)

// InputRefs returns every input name referenced as inputs.<name> or
// inputs['<name>'] in the expression text, in order of appearance.
func InputRefs(expr string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{reInputDot, reInputBracket, reInputBracket2} {
		for _, m := range re.FindAllStringSubmatch(expr, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// OutputRef is a <job, output> pair referenced from an expression.
type OutputRef struct {
	Job    string
	Output string
}

// NeedsOutputRefs returns every needs.<job>.outputs.<output> reference in
// the expression text.
func NeedsOutputRefs(expr string) []OutputRef {
	return pairs(expr, reNeedsOutput, reNeedsOutputB, reNeedsOutputC)
}

// JobsOutputRefs returns every jobs.<job>.outputs.<output> reference in the
// expression text (used in on.workflow_call.outputs.<x>.value).
func JobsOutputRefs(expr string) []OutputRef {
	return pairs(expr, reJobsOutput, reJobsOutputB, reJobsOutputC)
}

// StepRef is a steps.<id>.outputs.<name> (Field "outputs") or
// steps.<id>.outcome / .conclusion reference.
type StepRef struct {
	ID     string
	Field  string
	Output string
}

// StepRefs returns every steps.* reference in the expression text.
func StepRefs(expr string) []StepRef {
	var out []StepRef
	for _, re := range []*regexp.Regexp{reStepOut, reStepOutB, reStepOutC} {
		for _, m := range re.FindAllStringSubmatch(expr, -1) {
			out = append(out, StepRef{ID: m[1], Field: "outputs", Output: m[2]})
		}
	}
	for _, m := range reStepField.FindAllStringSubmatch(expr, -1) {
		out = append(out, StepRef{ID: m[1], Field: m[2]})
	}
	return out
}

func pairs(expr string, res ...*regexp.Regexp) []OutputRef {
	var out []OutputRef
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatch(expr, -1) {
			out = append(out, OutputRef{Job: m[1], Output: m[2]})
		}
	}
	return out
}

var reEventInputRef = regexp.MustCompile(`\bgithub\.event\.inputs\.([A-Za-z0-9_-]+)`)

// EventInputRefs lists the names referenced as github.event.inputs.<name>.
func EventInputRefs(expr string) []string {
	var out []string
	for _, m := range reEventInputRef.FindAllStringSubmatch(expr, -1) {
		out = append(out, m[1])
	}
	return out
}
