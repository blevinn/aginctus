package presentation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/observability/presentation"
	contract "github.com/blevinn/aginctus/pkg/observability"
)

func fixtureSnapshot() contract.Snapshot {
	e := contract.Event{SchemaVersion: contract.SchemaVersion, RunID: "run-one", Sequence: 2, Time: time.Now().UTC(),
		Kind: contract.Condition, State: contract.Unknown, StepID: "step-1", OperationID: "step-1",
		Scope: contract.Scope{Deployment: "deployment", Instance: "one", Target: "target"},
		Phase: "postcondition", CheckID: "ready", Required: true, AcceptedRevision: "rev\"quoted\\path\u2028"}
	run := e
	run.Sequence, run.Kind, run.State = 1, contract.Run, contract.Running
	run.StepID, run.OperationID, run.Scope, run.Phase, run.CheckID, run.Required, run.AcceptedRevision = "", "", contract.Scope{}, "", "", false, ""
	return contract.Snapshot{SchemaVersion: contract.SchemaVersion, RunID: "run-one", Watermark: 2, Run: run, Entries: []contract.Event{e}}
}

func TestTextQuotesMetadataAndNeverInfersHealthOrRefreshesObservation(t *testing.T) {
	var output bytes.Buffer
	sink := presentation.Text(&output)
	s := fixtureSnapshot()
	if err := sink.WriteSnapshot(s); err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteUpdate(contract.Update{Gap: true, Watermark: 3}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{`execution_state="unknown"`, `revision="rev\"quoted\\path\u2028"`, `observed_at="0001-01-01T00:00:00Z"`, `gap run="run-one" through=3 action=resnapshot`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "\u2028") || strings.Contains(text, "healthy") {
		t.Fatal("unsafe or invented presentation", text)
	}
}

func TestJSONLinesAreTypedRoundTrippableAndCorrelateGaps(t *testing.T) {
	var output bytes.Buffer
	sink := presentation.JSONLines(&output)
	s := fixtureSnapshot()
	if err := sink.WriteSnapshot(s); err != nil {
		t.Fatal(err)
	}
	e := s.Entries[0]
	if err := sink.WriteUpdate(contract.Update{Event: e, Watermark: e.Sequence}); err != nil {
		t.Fatal(err)
	}
	if err := sink.WriteUpdate(contract.Update{Gap: true, Watermark: 3}); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	for i, kind := range []string{"snapshot", "update", "update"} {
		var r struct {
			Schema   string             `json:"schema_version"`
			Type     string             `json:"type"`
			RunID    string             `json:"run_id"`
			Snapshot *contract.Snapshot `json:"snapshot"`
			Update   *contract.Update   `json:"update"`
		}
		if err := decoder.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Schema != contract.SchemaVersion || r.Type != kind || r.RunID != s.RunID {
			t.Fatalf("invalid envelope: %+v", r)
		}
		if i == 0 && (r.Snapshot.Validate() != nil || r.Snapshot.Entries[0].AcceptedRevision != e.AcceptedRevision) {
			t.Fatal("snapshot did not round-trip")
		}
		if i == 1 && (r.Update.Event.Validate() != nil || r.Update.Event != e) {
			t.Fatal("event did not round-trip")
		}
		if i == 2 && (!r.Update.Gap || r.Update.Watermark != 3 || r.Update.Event != (contract.Event{})) {
			t.Fatal("gap did not round-trip")
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRenderersRejectInvalidPayloadsAndPropagateOnlyConsumerWriteFailure(t *testing.T) {
	for _, factory := range []func(io.Writer) presentation.Sink{presentation.Text, presentation.JSONLines} {
		var output bytes.Buffer
		sink := factory(&output)
		s := fixtureSnapshot()
		s.Entries[0].AcceptedRevision = "SECRET\x1b[31m\nINJECTED"
		if err := sink.WriteSnapshot(s); err == nil || output.Len() != 0 {
			t.Fatal("invalid metadata escaped validation")
		}
		s = fixtureSnapshot()
		if err := sink.WriteSnapshot(s); err != nil {
			t.Fatal(err)
		}
		before := output.Len()
		if err := sink.WriteUpdate(contract.Update{Gap: true, Watermark: 3, Event: s.Run}); err == nil || output.Len() != before {
			t.Fatal("malformed gap published")
		}
		e := s.Entries[0]
		e.RunID = "another-run"
		if err := sink.WriteUpdate(contract.Update{Event: e, Watermark: e.Sequence}); err == nil {
			t.Fatal("renderer mixed runs")
		}
		failure := errors.New("consumer write failed")
		if err := factory(failingWriter{failure}).WriteSnapshot(fixtureSnapshot()); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if err := factory(failingWriter{}).WriteSnapshot(fixtureSnapshot()); !errors.Is(err, io.ErrShortWrite) {
			t.Fatal(err)
		}
	}
}
