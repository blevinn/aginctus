package observability_test

import (
	"testing"

	"github.com/blevinn/aginctus/internal/observability"
	contract "github.com/blevinn/aginctus/pkg/observability"
)

func TestBoundedCollectorRetainsTerminalAndSignalsEveryGap(t *testing.T) {
	c := observability.New("run-one")
	for range observability.EventLimit + 10 {
		c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
	}
	c.Record(contract.Event{Kind: contract.Run, State: contract.Blocked})
	report := c.Report()
	if len(report.Events) != observability.EventLimit || report.Dropped != 11 {
		t.Fatalf("retention bound/gap wrong: events=%d dropped=%d", len(report.Events), report.Dropped)
	}
	last := report.Events[len(report.Events)-1]
	if last.Kind != contract.Run || last.State != contract.Blocked || last.Sequence != uint64(observability.EventLimit+11) {
		t.Fatalf("terminal evidence lost: %+v", last)
	}
	for i, event := range report.Events {
		if event.Validate() != nil || event.RunID != "run-one" || (i < len(report.Events)-1 && event.Sequence != uint64(i+1)) {
			t.Fatalf("invalid sequence/envelope: %+v", event)
		}
	}
	report.Events[0].RunID = "mutated"
	c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
	if next := c.Report(); next.Events[0].RunID != "run-one" || len(next.Events) != observability.EventLimit || next.Dropped != report.Dropped {
		t.Fatal("snapshot aliases collector or terminal collector accepted new events")
	}
}

func TestInvalidTransitionsLeaveExplicitSequenceGaps(t *testing.T) {
	c := observability.New("run-two")
	c.Record(contract.Event{Kind: "unknown", State: contract.Running})
	c.Record(contract.Event{Kind: contract.Run, State: contract.Completed})
	r := c.Report()
	if r.Dropped != 1 || len(r.Events) != 1 || r.Events[0].Sequence != 2 {
		t.Fatalf("invalid transition was hidden or published: %+v", r)
	}
}
