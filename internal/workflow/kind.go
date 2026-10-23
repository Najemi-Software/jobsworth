// Package workflow parses GitHub workflow and action YAML into the model the
// rules operate on (spec §2). It never reads the network; callers hand it
// bytes and a display name.
package workflow

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Kind is what a YAML file turned out to be (spec §2).
type Kind int

const (
	// KindUnknown: parsed, but neither a workflow nor an action.
	KindUnknown Kind = iota
	// KindWorkflow: top-level `jobs:`.
	KindWorkflow
	// KindCompositeAction: top-level `runs.using: composite`.
	KindCompositeAction
	// KindOtherAction: top-level `runs.using` is node*/docker (or anything
	// else non-composite); only its inputs/outputs metadata is used.
	KindOtherAction
)

func (k Kind) String() string {
	switch k {
	case KindWorkflow:
		return "workflow"
	case KindCompositeAction:
		return "composite action"
	case KindOtherAction:
		return "action"
	}
	return "unknown"
}

// IsAction reports whether the kind is any kind of action.
func IsAction(k Kind) bool {
	return k == KindCompositeAction || k == KindOtherAction
}

// ProbeKind classifies raw YAML bytes by their top-level keys without
// building the full model. Malformed YAML, or YAML whose root is not a
// mapping, is KindUnknown with ok=false; a well-formed file that is neither a
// workflow nor an action is KindUnknown with ok=true.
func ProbeKind(data []byte) (kind Kind, ok bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return KindUnknown, false
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return KindUnknown, false
	}
	return kindOf(root), true
}

// kindOf classifies a root mapping node.
func kindOf(root *yaml.Node) Kind {
	if mapValue(root, "jobs") != nil {
		return KindWorkflow
	}
	runs := mapValue(root, "runs")
	if runs == nil || runs.Kind != yaml.MappingNode {
		return KindUnknown
	}
	using := mapValue(runs, "using")
	if using == nil || using.Kind != yaml.ScalarNode {
		return KindUnknown
	}
	if strings.TrimSpace(using.Value) == "composite" {
		return KindCompositeAction
	}
	return KindOtherAction
}

// documentRoot unwraps a DocumentNode to its content.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
