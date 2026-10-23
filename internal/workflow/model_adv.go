package workflow

// This file holds the model additions of spec-check-advanced: the on:
// block as data (§2), permissions (§1.5), runs-on (§3.1), concurrency
// (§2.4), schedule (§2.2) and the job and step keys the structure and
// condition families read.

// On is the parsed on: block.
type On struct {
	Pos Pos
	// Missing is true when the workflow has no on: at all.
	Missing bool
	// Shape: "scalar", "sequence", "mapping" or "other".
	Shape  string
	Events []Event
}

// Event is one trigger under on:.
type Event struct {
	Name string
	Pos  Pos
	// Keys are the event's child keys (mapping form only).
	Keys []OnKey
	// HasBody is true in the mapping form (even `push:` with a null body).
	HasBody bool
}

// OnKey is one child key of an event: types:, branches:, paths:, ...
type OnKey struct {
	Name string
	Pos  Pos
	// Scalar is set for a scalar value; List for a sequence of scalars;
	// IsMapping for a mapping; Other for anything else (a sequence with
	// non-scalar items).
	Scalar    *Scalar
	List      []Scalar
	IsMapping bool
	Other     bool
	// Empty is true for an empty sequence.
	Empty bool
}

// Cron is one schedule: entry.
type Cron struct {
	Expr string
	Pos  Pos
	// Quoted is true when the YAML scalar was quoted.
	Quoted bool
	// Invalid is set when the entry is not a `cron:` mapping.
	Invalid bool
}

// Permissions is a permissions: block at workflow or job level.
type Permissions struct {
	Pos Pos
	// Form: "read-all", "write-all", "map", "empty" (`{}`), "other".
	Form   string
	Scopes []PermissionScope
}

// PermissionScope is one key of a permissions map.
type PermissionScope struct {
	Name  string
	Level string
	Pos   Pos
}

// Concurrency is a concurrency: block.
type Concurrency struct {
	Pos Pos
	// Group is the group value (the scalar itself when the block is a
	// bare string).
	Group            Scalar
	CancelInProgress Scalar
	HasCancel        bool
}

// RunsOn is a job's runs-on:.
type RunsOn struct {
	Pos Pos
	// Labels are the literal or expression labels (scalar or list form).
	Labels []Scalar
	// Group is the group: of the mapping form; HasGroup marks the form.
	Group     Scalar
	HasGroup  bool
	HasLabels bool
}

// Strategy holds the non-matrix strategy keys.
type Strategy struct {
	Pos         Pos
	MaxParallel *Scalar
	FailFast    *Scalar
}
