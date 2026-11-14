package observability_test

import (
	"sync"
	"testing"

	"github.com/blevinn/aginctus/internal/observability"
	contract "github.com/blevinn/aginctus/pkg/observability"
)

func checkEvent(kind contract.Kind, state contract.State, attempt int) contract.Event {
	return contract.Event{Kind: kind, State: state, StepID: "step-1", OperationID: "step-1",
		Scope: contract.Scope{Deployment: "deploy", Instance: "one", Target: "target"},
		Phase: "postcondition", CheckID: "ready", Attempt: attempt, Required: true}
}

func TestStatusSurvivesTraceTruncationAndDoesNotRefreshEvidence(t *testing.T) {
	c := observability.New("run-one")
	c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
	for range observability.EventLimit + 10 {
		c.Record(contract.Event{Kind: contract.Operation, State: contract.Running, StepID: "step-1", OperationID: "step-1"})
	}
	c.Record(contract.Event{Kind: contract.Operation, State: contract.Completed, StepID: "step-1", OperationID: "step-1"})
	c.Record(checkEvent(contract.Condition, contract.Unknown, 0))
	c.Record(contract.Event{Kind: contract.Run, State: contract.Blocked})
	s := c.Snapshot()
	if !s.Finished || s.Run.State != contract.Blocked || s.Dropped == 0 || len(s.Entries) != 2 ||
		s.Entries[0].State != contract.Completed || s.Entries[1].State != contract.Unknown || !s.Entries[1].ObservedAt.IsZero() {
		t.Fatalf("snapshot lost knowledge after prefix truncation: %+v", s)
	}
	r := c.Report()
	if r.Status.Watermark != s.Watermark || r.Status.Dropped != r.Dropped || s.Run.Sequence != s.Watermark {
		t.Fatal("report/snapshot do not share the terminal watermark")
	}
	s.Entries[0].State = contract.Running
	if c.Snapshot().Entries[0].State != contract.Completed {
		t.Fatal("snapshot aliases projection")
	}
	before := c.Snapshot()
	c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
	if c.Snapshot().Watermark != before.Watermark {
		t.Fatal("terminal state changed")
	}
}

func TestStatusSlotsAreBoundedAcrossRetriesAndProgressCannotSetLifecycle(t *testing.T) {
	c := observability.New("run-one")
	for attempt := 1; attempt < 2000; attempt++ {
		c.Record(checkEvent(contract.CheckAttempt, contract.Running, attempt))
		for i := range 2 {
			e := checkEvent(contract.DomainProgress, contract.Running, attempt)
			e.Progress = contract.Progress{Code: []string{"first", "second"}[i], Current: 1, Total: 1}
			c.Record(e)
		}
		c.Record(checkEvent(contract.CheckAttempt, contract.Failed, attempt))
	}
	c.Record(checkEvent(contract.Condition, contract.Failed, 0))
	s := c.Snapshot()
	if len(s.Entries) != 3 || s.Entries[1].State != contract.Failed || s.Entries[2].State != contract.Failed {
		t.Fatalf("progress changed lifecycle or history grew: %+v", s)
	}
	c.Record(checkEvent(contract.CheckAttempt, contract.Running, 2000))
	for _, e := range c.Snapshot().Entries {
		if e.Kind == contract.DomainProgress {
			t.Fatal("previous attempt activity survived a fresh attempt")
		}
	}
	c.Record(checkEvent(contract.DomainProgress, contract.Running, 2000)) // missing code
	if next := c.Snapshot(); len(next.Entries) != 2 || next.Dropped != c.Report().Dropped {
		t.Fatal("invalid progress entered projection or loss count diverged")
	}
}

func TestConcurrentSnapshotsAreAtomicAndMonitorClaimsAreIsolated(t *testing.T) {
	m := observability.NewMonitor()
	var reader contract.Reader = m
	if reader.Snapshot().RunID == "" || reader.Snapshot().Watermark != 0 {
		t.Fatal("new monitor not inspectable")
	}
	c := m.Claim()
	if c == nil || m.Claim() != nil {
		t.Fatal("monitor can be bound to multiple executions")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
				s := reader.Snapshot()
				if s.Run.Sequence != s.Watermark || s.Run.RunID != s.RunID {
					t.Error("snapshot contains mixed watermarks")
				}
			}
		})
	}
	wg.Wait()
	if observability.NewMonitor().Snapshot().RunID == reader.Snapshot().RunID {
		t.Fatal("independent monitors share run identity")
	}
}
