package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse builds the File model from YAML bytes. A YAML syntax error is
// returned as an error (the caller attributes it); a well-formed file of an
// unrecognised shape parses to KindUnknown with no error.
func Parse(data []byte) (*File, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	root := documentRoot(&doc)
	f := &File{}
	if root == nil || root.Kind != yaml.MappingNode {
		return f, nil
	}
	f.Kind = kindOf(root)

	switch f.Kind {
	case KindWorkflow:
		parseWorkflow(f, root)
	case KindCompositeAction, KindOtherAction:
		if runs := mapValue(root, "runs"); runs != nil {
			if u := mapValue(runs, "using"); u != nil {
				f.Using = strings.TrimSpace(u.Value)
			}
		}
		parseAction(f, root)
	default:
		return f, nil
	}

	collectExprsAndUses(f, root, exprCtx{step: -1}, "", f.Kind == KindWorkflow)
	return f, nil
}

// --- workflow ---------------------------------------------------------------

func parseWorkflow(f *File, root *yaml.Node) {
	on := mapValue(root, "on")
	f.HasWorkflowCall = hasTrigger(on, "workflow_call")
	f.HasWorkflowDispatch = hasTrigger(on, "workflow_dispatch")
	f.Events = triggerNames(on)
	if on != nil && on.Kind == yaml.MappingNode {
		if wc := mapValue(on, "workflow_call"); wc != nil {
			f.Inputs = append(f.Inputs, parseInputs(mapValue(wc, "inputs"), "workflow_call")...)
			f.Outputs = parseOutputs(mapValue(wc, "outputs"), true)
			f.Secrets = parseSecrets(mapValue(wc, "secrets"))
		}
		if wd := mapValue(on, "workflow_dispatch"); wd != nil {
			f.Inputs = append(f.Inputs, parseInputs(mapValue(wd, "inputs"), "workflow_dispatch")...)
		}
	}
	f.DefaultShell = defaultShell(root)
	f.Name = scalarOrEmpty(mapValue(root, "name"))
	f.RunName = scalarOrEmpty(mapValue(root, "run-name"))
	f.Env = parseWith(mapValue(root, "env"))
	f.On = parseOn(on)
	f.Crons = parseSchedule(on)
	f.Permissions = parsePermissions(mapValue(root, "permissions"))
	f.Concurrency = parseConcurrency(mapValue(root, "concurrency"))
	f.Keys = keysOf(root)

	jobs := mapValue(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		k, v := jobs.Content[i], jobs.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		job := Job{Name: k.Value, Pos: posOf(k)}
		if v.Kind == yaml.MappingNode {
			job.Needs = parseNeeds(mapValue(v, "needs"))
			job.Outputs = parseJobOutputs(mapValue(v, "outputs"))
			if u := mapValue(v, "uses"); u != nil && u.Kind == yaml.ScalarNode {
				job.Uses = &Uses{Raw: u.Value, Pos: posOf(u), Job: job.Name, JobLevel: true}
			}
			job.With = parseWith(mapValue(v, "with"))
			job.Steps = parseSteps(mapValue(v, "steps"))
			job.DefaultShell = defaultShell(v)
			job.DisplayName = scalarOrEmpty(mapValue(v, "name"))
			job.Env = parseWith(mapValue(v, "env"))
			job.If = scalarOrEmpty(mapValue(v, "if"))
			if sec := mapValue(v, "secrets"); sec != nil {
				job.HasSecrets = true
				job.SecretsPos = posOf(sec)
				switch {
				case sec.Kind == yaml.ScalarNode && sec.Value == "inherit":
					job.SecretsInherit = true
				case sec.Kind == yaml.ScalarNode && containsExpr(sec.Value):
					job.SecretsExpr = true
				case sec.Kind == yaml.MappingNode:
					job.Secrets = parseWith(sec)
				}
			}
			if st := mapValue(v, "strategy"); st != nil {
				job.Matrix = parseMatrix(mapValue(st, "matrix"))
				job.Strategy = parseStrategy(st)
			}
			job.Permissions = parsePermissions(mapValue(v, "permissions"))
			job.RunsOn = parseRunsOn(mapValue(v, "runs-on"))
			job.Concurrency = parseConcurrency(mapValue(v, "concurrency"))
			if s := mapValue(v, "steps"); s != nil {
				job.HasSteps = true
				job.StepsEmpty = s.Kind == yaml.SequenceNode && len(s.Content) == 0
			}
			job.TimeoutMinutes = mapValue(v, "timeout-minutes") != nil
			if e := mapValue(v, "environment"); e != nil {
				sc := scalarOf(e)
				if e.Kind == yaml.MappingNode {
					if n := mapValue(e, "name"); n != nil {
						sc = scalarOf(n)
					}
				}
				job.Environment = &sc
			}
			job.Container = rawText(mapValue(v, "container"))
			job.Services = rawText(mapValue(v, "services"))
			job.Keys = keysOf(v)
		}
		f.Jobs = append(f.Jobs, job)
	}
}

// triggerNames lists the event names under on: in any of its shapes.
func triggerNames(on *yaml.Node) []string {
	if on == nil {
		return nil
	}
	switch on.Kind {
	case yaml.ScalarNode:
		return []string{on.Value}
	case yaml.SequenceNode:
		var out []string
		for _, c := range on.Content {
			if c.Kind == yaml.ScalarNode {
				out = append(out, c.Value)
			}
		}
		return out
	case yaml.MappingNode:
		var out []string
		for i := 0; i+1 < len(on.Content); i += 2 {
			if on.Content[i].Kind == yaml.ScalarNode {
				out = append(out, on.Content[i].Value)
			}
		}
		return out
	}
	return nil
}

func defaultShell(m *yaml.Node) string {
	d := mapValue(m, "defaults")
	if d == nil {
		return ""
	}
	r := mapValue(d, "run")
	if r == nil {
		return ""
	}
	sh := mapValue(r, "shell")
	if sh == nil || sh.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(sh.Value)
}

func scalarOrEmpty(n *yaml.Node) Scalar {
	if n == nil {
		return Scalar{}
	}
	return scalarOf(n)
}

func parseSecrets(n *yaml.Node) []Secret {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []Secret
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		sec := Secret{Name: k.Value, Pos: posOf(k)}
		if r := mapValue(v, "required"); r != nil && r.Kind == yaml.ScalarNode {
			sec.Required = r.Value == "true"
		}
		out = append(out, sec)
	}
	return out
}

// parseSteps reads a steps: sequence.
func parseSteps(n *yaml.Node) []Step {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	var out []Step
	for i, c := range n.Content {
		st := Step{Index: i, Pos: posOf(c)}
		if c.Kind == yaml.ScalarNode && c.Tag == "!!null" {
			st.IsNull = true
		}
		if c.Kind == yaml.MappingNode {
			if coe := mapValue(c, "continue-on-error"); coe != nil {
				sc := scalarOf(coe)
				st.ContinueOnError = &sc
			}
			st.TimeoutMinutes = mapValue(c, "timeout-minutes") != nil
			if id := mapValue(c, "id"); id != nil && id.Kind == yaml.ScalarNode {
				st.ID, st.IDPos = id.Value, posOf(id)
			}
			st.Name = scalarOrEmpty(mapValue(c, "name"))
			if u := mapValue(c, "uses"); u != nil && u.Kind == yaml.ScalarNode {
				st.Uses, st.UsesPos = u.Value, posOf(u)
			}
			if r := mapValue(c, "run"); r != nil {
				st.HasRun, st.RunPos = true, posOf(r)
				if r.Kind == yaml.ScalarNode {
					st.Run = r.Value
					st.RunBlock = r.Style == yaml.LiteralStyle || r.Style == yaml.FoldedStyle
				}
			}
			if sh := mapValue(c, "shell"); sh != nil && sh.Kind == yaml.ScalarNode {
				st.Shell = strings.TrimSpace(sh.Value)
			}
			if w := mapValue(c, "with"); w != nil {
				st.HasWith, st.WithPos = true, posOf(w)
				st.With = parseWith(w)
			}
			st.Env = parseWith(mapValue(c, "env"))
			st.If = scalarOrEmpty(mapValue(c, "if"))
			if wd := mapValue(c, "working-directory"); wd != nil && wd.Kind == yaml.ScalarNode {
				st.WorkingDirectory = wd.Value
			}
		}
		out = append(out, st)
	}
	return out
}

func parseMatrix(n *yaml.Node) *MatrixSpec {
	if n == nil {
		return nil
	}
	m := &MatrixSpec{Pos: posOf(n), Axes: map[string][]Scalar{}}
	if n.Kind == yaml.ScalarNode {
		if containsExpr(n.Value) {
			m.Expr = n.Value
		}
		return m
	}
	if n.Kind != yaml.MappingNode {
		return m
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "include":
			if v.Kind == yaml.ScalarNode && containsExpr(v.Value) {
				m.IncludeExpr = v.Value
			}
			if v.Kind == yaml.SequenceNode {
				for _, row := range v.Content {
					if row.Kind != yaml.MappingNode {
						continue
					}
					fields := map[string]Scalar{}
					for j := 0; j+1 < len(row.Content); j += 2 {
						if row.Content[j].Kind == yaml.ScalarNode {
							fields[row.Content[j].Value] = scalarOf(row.Content[j+1])
						}
					}
					m.Include = append(m.Include, fields)
				}
			}
		case "exclude":
			if v.Kind == yaml.SequenceNode {
				for _, row := range v.Content {
					if row.Kind != yaml.MappingNode {
						continue
					}
					fields := map[string]Scalar{}
					for j := 0; j+1 < len(row.Content); j += 2 {
						if row.Content[j].Kind == yaml.ScalarNode {
							fields[row.Content[j].Value] = scalarOf(row.Content[j+1])
						}
					}
					m.Exclude = append(m.Exclude, fields)
				}
			}
		default:
			switch v.Kind {
			case yaml.SequenceNode:
				if len(v.Content) == 0 {
					m.Empty = append(m.Empty, k.Value)
				}
				for _, el := range v.Content {
					if el.Kind == yaml.MappingNode {
						if m.Objects == nil {
							m.Objects = map[string][]map[string]Scalar{}
						}
						row := map[string]Scalar{}
						for j := 0; j+1 < len(el.Content); j += 2 {
							if el.Content[j].Kind == yaml.ScalarNode {
								row[el.Content[j].Value] = scalarOf(el.Content[j+1])
							}
						}
						m.Objects[k.Value] = append(m.Objects[k.Value], row)
					}
					m.Axes[k.Value] = append(m.Axes[k.Value], scalarOf(el))
				}
			case yaml.ScalarNode:
				if containsExpr(v.Value) {
					m.IncludeExpr = v.Value // an axis built from an expression
				}
				m.Axes[k.Value] = []Scalar{scalarOf(v)}
			}
		}
	}
	return m
}

// hasTrigger reports whether on: declares the named trigger in any of its
// three shapes: scalar, sequence, or mapping.
func hasTrigger(on *yaml.Node, name string) bool {
	if on == nil {
		return false
	}
	switch on.Kind {
	case yaml.ScalarNode:
		return on.Value == name
	case yaml.SequenceNode:
		for _, c := range on.Content {
			if c.Kind == yaml.ScalarNode && c.Value == name {
				return true
			}
		}
	case yaml.MappingNode:
		return mapValue(on, name) != nil
	}
	return false
}

func parseNeeds(n *yaml.Node) []Need {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return []Need{{Name: n.Value, Pos: posOf(n)}}
	case yaml.SequenceNode:
		var out []Need
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode {
				out = append(out, Need{Name: c.Value, Pos: posOf(c)})
			}
		}
		return out
	}
	return nil
}

func parseJobOutputs(n *yaml.Node) []JobOutput {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []JobOutput
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		o := JobOutput{Name: k.Value, Pos: posOf(k)}
		if v.Kind == yaml.ScalarNode {
			o.Value = v.Value
		}
		out = append(out, o)
	}
	return out
}

func parseWith(n *yaml.Node) []With {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []With
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		out = append(out, With{Name: k.Value, Value: scalarOf(v)})
	}
	return out
}

// --- action -----------------------------------------------------------------

func parseAction(f *File, root *yaml.Node) {
	f.Inputs = parseInputs(mapValue(root, "inputs"), "")
	f.Outputs = parseOutputs(mapValue(root, "outputs"), true)
	f.Name = scalarOrEmpty(mapValue(root, "name"))
	f.Description = scalarOrEmpty(mapValue(root, "description"))
	f.Keys = keysOf(root)
	if runs := mapValue(root, "runs"); runs != nil {
		f.Steps = parseSteps(mapValue(runs, "steps"))
	}
}

// --- shared declarations ----------------------------------------------------

func parseInputs(n *yaml.Node, trigger string) []Input {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []Input
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		in := Input{Name: k.Value, Pos: posOf(k), Trigger: trigger}
		if v.Kind == yaml.MappingNode {
			if t := mapValue(v, "type"); t != nil && t.Kind == yaml.ScalarNode {
				in.Type = t.Value
			}
			if r := mapValue(v, "required"); r != nil && r.Kind == yaml.ScalarNode {
				in.Required = r.Value == "true"
			}
			if d := mapValue(v, "default"); d != nil {
				in.HasDefault = true
				in.Default = scalarOf(d)
			}
			in.HasDescription = mapValue(v, "description") != nil
			if o := mapValue(v, "options"); o != nil && o.Kind == yaml.SequenceNode {
				for _, c := range o.Content {
					if c.Kind == yaml.ScalarNode {
						in.Options = append(in.Options, c.Value)
					}
				}
			}
		}
		out = append(out, in)
	}
	return out
}

func parseOutputs(n *yaml.Node, withValue bool) []Output {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []Output
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		o := Output{Name: k.Value, Pos: posOf(k)}
		if withValue && v.Kind == yaml.MappingNode {
			if val := mapValue(v, "value"); val != nil && val.Kind == yaml.ScalarNode {
				o.Value = val.Value
			}
		}
		out = append(out, o)
	}
	return out
}

// --- expressions and uses ---------------------------------------------------

// exprCtx is the position in the tree the walker is at.
type exprCtx struct {
	job       string
	step      int
	inOutputs bool
	section   string
	parent    string
}

// collectExprsAndUses walks the whole tree once, recording every ${{ }}
// expression, every `if:` value, and every job- or step-level `uses:`.
// key is the mapping key the node sits under; the sentinels "<job>" and
// "<step>" mark job and step mappings.
func collectExprsAndUses(f *File, n *yaml.Node, ctx exprCtx, key string, isWorkflow bool) {
	switch n.Kind {
	case yaml.ScalarNode:
		for _, e := range ExtractExprs(n.Value, posOf(n)) {
			f.Exprs = append(f.Exprs, Expr{Text: e.Text, Pos: e.Pos, Job: ctx.job, Step: ctx.step, InOutputs: ctx.inOutputs, Key: key, Raw: n.Value, Section: ctx.section, Parent: ctx.parent})
		}
		if key == "if" && !containsExpr(n.Value) && strings.TrimSpace(n.Value) != "" {
			f.Exprs = append(f.Exprs, Expr{Text: n.Value, Pos: posOf(n), Job: ctx.job, Step: ctx.step, InOutputs: ctx.inOutputs, Key: key, IsIf: true, Raw: n.Value, Section: ctx.section, Parent: ctx.parent})
		}
	case yaml.SequenceNode:
		// Elements of a steps: sequence are steps; any other sequence
		// element (matrix include rows, etc.) never carries a uses:.
		for i, c := range n.Content {
			childKey, childCtx := "", ctx
			childCtx.parent = key
			if key == "steps" {
				childKey = "<step>"
				childCtx.step = i
				childCtx.parent = "<step>"
			}
			collectExprsAndUses(f, c, childCtx, childKey, isWorkflow)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			childKey := ""
			if k.Kind == yaml.ScalarNode {
				childKey = k.Value
			}
			if childKey == "jobs" && isWorkflow && ctx.job == "" && v.Kind == yaml.MappingNode {
				collectJobs(f, v, isWorkflow)
				continue
			}
			// uses: counts only directly under a job ("<job>") or a step
			// ("<step>"), never under with:/env:/matrix rows/etc.
			if childKey == "uses" && v.Kind == yaml.ScalarNode && (key == "<job>" || key == "<step>") {
				f.Uses = append(f.Uses, Uses{Raw: v.Value, Pos: posOf(v), Job: ctx.job, JobLevel: key == "<job>", Comment: strings.TrimSpace(strings.TrimPrefix(v.LineComment, "#"))})
			}
			childCtx := ctx
			childCtx.parent = key
			if key == "" && ctx.job == "" {
				childCtx.section = childKey // a top-level key
			}
			// A job's outputs: mapping and an action's outputs: block run
			// after every step and may reference any step id.
			if childKey == "outputs" && (key == "<job>" || (key == "" && !isWorkflow)) {
				childCtx.inOutputs = true
			}
			collectExprsAndUses(f, v, childCtx, childKey, isWorkflow)
		}
	case yaml.DocumentNode:
		for _, c := range n.Content {
			collectExprsAndUses(f, c, ctx, key, isWorkflow)
		}
	}
}

// collectJobs walks jobs: so every nested node knows its job name, and marks
// the job mapping itself with the sentinel key "<job>" so a direct `uses:`
// child is recognised as job-level.
func collectJobs(f *File, jobs *yaml.Node, isWorkflow bool) {
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		k, v := jobs.Content[i], jobs.Content[i+1]
		name := ""
		if k.Kind == yaml.ScalarNode {
			name = k.Value
		}
		collectExprsAndUses(f, v, exprCtx{job: name, step: -1, section: "jobs", parent: "<job>"}, "<job>", isWorkflow)
	}
}

// ExprSpan is one expression found in a scalar.
type ExprSpan struct {
	Text string
	Pos  Pos
}

// ExtractExprs finds every ${{ ... }} in s and returns the inner text. Pos is
// the scalar's own position: yaml.v3 does not give intra-scalar offsets, so
// every expression in a scalar is attributed to the scalar's start.
func ExtractExprs(s string, at Pos) []ExprSpan {
	var out []ExprSpan
	for {
		start := strings.Index(s, "${{")
		if start < 0 {
			return out
		}
		rest := s[start+3:]
		end := exprEnd(rest)
		if end < 0 {
			// An unterminated string: fall back to the first }} so the
			// expression parser can report the quoting.
			end = strings.Index(rest, "}}")
			if end < 0 {
				return out
			}
		}
		out = append(out, ExprSpan{Text: strings.TrimSpace(rest[:end]), Pos: at})
		s = rest[end+2:]
	}
}

func containsExpr(s string) bool {
	return strings.Contains(s, "${{")
}

func posOf(n *yaml.Node) Pos {
	return Pos{Line: n.Line, Column: n.Column}
}

// scalarOf captures a scalar node's value and tag; non-scalars yield an empty
// Scalar with Tag "" and the node's position.
func scalarOf(n *yaml.Node) Scalar {
	if n == nil {
		return Scalar{}
	}
	s := Scalar{Pos: posOf(n)}
	if n.Kind == yaml.ScalarNode {
		s.Value = n.Value
		s.Tag = n.ShortTag()
	}
	return s
}

// --- advanced-spec blocks ----------------------------------------------------

// keysOf lists a mapping's scalar keys in order.
func keysOf(m *yaml.Node) []string {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode {
			out = append(out, m.Content[i].Value)
		}
	}
	return out
}

// rawText re-encodes a node compactly for equality comparisons.
func rawText(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	out, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseOn reads the on: block in any of its shapes.
func parseOn(on *yaml.Node) *On {
	if on == nil {
		return &On{Missing: true}
	}
	o := &On{Pos: posOf(on)}
	switch on.Kind {
	case yaml.ScalarNode:
		o.Shape = "scalar"
		o.Events = []Event{{Name: on.Value, Pos: posOf(on)}}
	case yaml.SequenceNode:
		o.Shape = "sequence"
		for _, c := range on.Content {
			if c.Kind == yaml.ScalarNode {
				o.Events = append(o.Events, Event{Name: c.Value, Pos: posOf(c)})
			}
		}
	case yaml.MappingNode:
		o.Shape = "mapping"
		for i := 0; i+1 < len(on.Content); i += 2 {
			k, v := on.Content[i], on.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				continue
			}
			ev := Event{Name: k.Value, Pos: posOf(k), HasBody: true}
			if v.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(v.Content); j += 2 {
					ck, cv := v.Content[j], v.Content[j+1]
					if ck.Kind != yaml.ScalarNode {
						continue
					}
					ev.Keys = append(ev.Keys, parseOnKey(ck, cv))
				}
			}
			o.Events = append(o.Events, ev)
		}
	default:
		o.Shape = "other"
	}
	return o
}

func parseOnKey(k, v *yaml.Node) OnKey {
	ok := OnKey{Name: k.Value, Pos: posOf(k)}
	switch v.Kind {
	case yaml.ScalarNode:
		sc := scalarOf(v)
		ok.Scalar = &sc
	case yaml.SequenceNode:
		ok.Empty = len(v.Content) == 0
		for _, c := range v.Content {
			if c.Kind != yaml.ScalarNode {
				ok.Other = true
				continue
			}
			ok.List = append(ok.List, scalarOf(c))
		}
	case yaml.MappingNode:
		ok.IsMapping = true
	default:
		ok.Other = true
	}
	return ok
}

// parseSchedule reads on.schedule entries.
func parseSchedule(on *yaml.Node) []Cron {
	if on == nil || on.Kind != yaml.MappingNode {
		return nil
	}
	s := mapValue(on, "schedule")
	if s == nil || s.Kind != yaml.SequenceNode {
		return nil
	}
	var out []Cron
	for _, c := range s.Content {
		if c.Kind != yaml.MappingNode {
			out = append(out, Cron{Pos: posOf(c), Invalid: true})
			continue
		}
		cron := mapValue(c, "cron")
		if cron == nil || cron.Kind != yaml.ScalarNode {
			out = append(out, Cron{Pos: posOf(c), Invalid: true})
			continue
		}
		out = append(out, Cron{Expr: cron.Value, Pos: posOf(cron), Quoted: cron.Style == yaml.SingleQuotedStyle || cron.Style == yaml.DoubleQuotedStyle})
	}
	return out
}

// parsePermissions reads a permissions: block.
func parsePermissions(n *yaml.Node) *Permissions {
	if n == nil {
		return nil
	}
	p := &Permissions{Pos: posOf(n)}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Value {
		case "read-all", "write-all":
			p.Form = n.Value
		default:
			p.Form = "other"
		}
	case yaml.MappingNode:
		if len(n.Content) == 0 {
			p.Form = "empty"
			return p
		}
		p.Form = "map"
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				continue
			}
			p.Scopes = append(p.Scopes, PermissionScope{Name: k.Value, Level: v.Value, Pos: posOf(k)})
		}
	default:
		p.Form = "other"
	}
	return p
}

// parseConcurrency reads a concurrency: block (string or mapping).
func parseConcurrency(n *yaml.Node) *Concurrency {
	if n == nil {
		return nil
	}
	c := &Concurrency{Pos: posOf(n)}
	switch n.Kind {
	case yaml.ScalarNode:
		c.Group = scalarOf(n)
	case yaml.MappingNode:
		if g := mapValue(n, "group"); g != nil {
			c.Group = scalarOf(g)
		}
		if ci := mapValue(n, "cancel-in-progress"); ci != nil {
			c.HasCancel = true
			c.CancelInProgress = scalarOf(ci)
		}
	}
	return c
}

// parseRunsOn reads runs-on: in its scalar, list and group forms.
func parseRunsOn(n *yaml.Node) *RunsOn {
	if n == nil {
		return nil
	}
	r := &RunsOn{Pos: posOf(n)}
	switch n.Kind {
	case yaml.ScalarNode:
		r.Labels = []Scalar{scalarOf(n)}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode {
				r.Labels = append(r.Labels, scalarOf(c))
			}
		}
	case yaml.MappingNode:
		if g := mapValue(n, "group"); g != nil {
			r.HasGroup = true
			r.Group = scalarOf(g)
		}
		if l := mapValue(n, "labels"); l != nil {
			r.HasLabels = true
			switch l.Kind {
			case yaml.ScalarNode:
				r.Labels = []Scalar{scalarOf(l)}
			case yaml.SequenceNode:
				for _, c := range l.Content {
					if c.Kind == yaml.ScalarNode {
						r.Labels = append(r.Labels, scalarOf(c))
					}
				}
			}
		}
	}
	return r
}

// parseStrategy reads the non-matrix strategy keys.
func parseStrategy(st *yaml.Node) *Strategy {
	s := &Strategy{Pos: posOf(st)}
	if mp := mapValue(st, "max-parallel"); mp != nil {
		sc := scalarOf(mp)
		s.MaxParallel = &sc
	}
	if ff := mapValue(st, "fail-fast"); ff != nil {
		sc := scalarOf(ff)
		s.FailFast = &sc
	}
	return s
}

// exprEnd finds the closing }} of an expression body, skipping any }}
// inside a single-quoted string (” is an escaped quote). -1 when
// unterminated (medium review 6 D13).
func exprEnd(rest string) int {
	inStr := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case c == '\'':
			if inStr && i+1 < len(rest) && rest[i+1] == '\'' {
				i++ // escaped quote
				continue
			}
			inStr = !inStr
		case !inStr && c == '}' && i+1 < len(rest) && rest[i+1] == '}':
			return i
		}
	}
	return -1
}
