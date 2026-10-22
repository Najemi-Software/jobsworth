package contextschema

import (
	"reflect"
	"testing"

	"github.com/najemi-software/jobsworth/internal/shape"
)

func TestEventSingle(t *testing.T) {
	t.Parallel()
	e := Event([]string{"push"}, true)
	if e.Kind != shape.Object || !e.Open {
		t.Fatalf("push = %s", e)
	}
	if _, err := shape.Access(e, shape.Seg{Field: "head_commit"}); err != nil {
		t.Fatal(err)
	}
	if s, err := shape.Access(e, shape.Seg{Field: "pull_request"}); err != nil || s.Kind != shape.Any {
		t.Fatalf("open payload must accept unknown fields: %v %v", s, err)
	}
	closed := Event([]string{"push"}, false)
	if _, err := shape.Access(closed, shape.Seg{Field: "pull_request"}); err == nil {
		t.Fatal("closed payload must reject unknown fields")
	}
}

func TestEventMulti(t *testing.T) {
	t.Parallel()
	e := Event([]string{"push", "pull_request"}, false)
	for _, f := range []string{"head_commit", "pull_request", "repository"} {
		if _, err := shape.Access(e, shape.Seg{Field: f}); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := shape.Access(e, shape.Seg{Field: "release"}); err == nil {
		t.Error("field from neither event should be rejected when closed")
	}
}

func TestEventUnknown(t *testing.T) {
	t.Parallel()
	if e := Event([]string{"push", "custom_thing"}, true); e.Kind != shape.UnknownObject {
		t.Fatalf("unknown event = %s", e)
	}
	if e := Event(nil, true); e.Kind != shape.UnknownObject {
		t.Fatalf("no events = %s", e)
	}
}

// TestPayloadSnapshot pins the curated payload list so it cannot silently
// shrink.
func TestPayloadSnapshot(t *testing.T) {
	t.Parallel()
	want := []string{"issue_comment", "issues", "pull_request", "pull_request_target", "push", "release", "schedule", "workflow_call", "workflow_dispatch", "workflow_run"}
	if got := PayloadNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("payloads = %v", got)
	}
}

func TestStaticContexts(t *testing.T) {
	t.Parallel()
	if s, err := shape.Access(Runner(), shape.Seg{Field: "os"}); err != nil || s.Kind != shape.String {
		t.Fatalf("runner.os: %v %v", s, err)
	}
	if _, err := shape.Access(Runner(), shape.Seg{Field: "nope"}); err == nil {
		t.Fatal("runner is closed")
	}
	g := GitHub([]string{"push"}, true)
	ev, _ := shape.Access(g, shape.Seg{Field: "event"})
	if _, err := shape.Access(ev, shape.Seg{Field: "commits"}); err != nil {
		t.Fatal(err)
	}
	if s, err := shape.Access(g, shape.Seg{Field: "custom"}); err != nil || s.Kind != shape.Any {
		t.Fatal("github context is open")
	}
	if s, _ := shape.Access(Job(), shape.Seg{Field: "status"}); s.Kind != shape.String {
		t.Fatal("job.status")
	}
}
