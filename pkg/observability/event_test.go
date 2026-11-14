package observability_test

import (
	"strings"
	"testing"
	"time"

	report "github.com/blevinn/aginctus/pkg/observability"
)

func TestPublicEventEnvelopeRejectsAmbiguousEvidence(t *testing.T) {
	event := report.Event{SchemaVersion: report.SchemaVersion, RunID: "run-1", Sequence: 1, Time: time.Now(),
		Kind: report.CheckAttempt, State: report.Passed, StepID: "step-1", OperationID: "step-1",
		Scope: report.Scope{Deployment: "lab", Instance: "one", Target: "target"}, Phase: "postcondition", CheckID: "ready", Attempt: 1}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*report.Event){
		func(e *report.Event) { e.SchemaVersion = "future" },
		func(e *report.Event) { e.RunID = "unsafe\nrun" },
		func(e *report.Event) { e.Sequence = 0 },
		func(e *report.Event) { e.Time = time.Time{} },
		func(e *report.Event) { e.Kind = "invented" },
		func(e *report.Event) { e.State = "invented" },
		func(e *report.Event) { e.State = report.Completed },
		func(e *report.Event) { e.Reason = "raw error" },
		func(e *report.Event) { e.Scope.Instance = "" },
		func(e *report.Event) { e.Attempt = 0 },
		func(e *report.Event) { e.StepID = "" },
		func(e *report.Event) { e.OperationID = "" },
		func(e *report.Event) { e.Phase = "unscoped" },
		func(e *report.Event) { e.AcceptedRevision = "raw\nrevision" },
		func(e *report.Event) { e.AcceptedRevision = strings.Repeat("x", 2049) },
		func(e *report.Event) { e.Kind = report.Run; e.State = report.Completed },
	} {
		invalid := event
		modify(&invalid)
		if invalid.Validate() == nil {
			t.Fatalf("invalid evidence accepted: %+v", invalid)
		}
	}
}
