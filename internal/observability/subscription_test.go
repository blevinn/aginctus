package observability_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/blevinn/aginctus/internal/observability"
	contract "github.com/blevinn/aginctus/pkg/observability"
)

func nextUpdate(t *testing.T, s contract.Subscription) contract.Update {
	t.Helper()
	u, err := s.Next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSnapshotSubscribeRaceReplaysThenContinuesAfterWatermark(t *testing.T) {
	m := observability.NewMonitor()
	var stream contract.Stream = m
	c := m.Claim()
	c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
	snapshot := stream.Snapshot()
	c.Record(contract.Event{Kind: contract.Validation, State: contract.Running})
	s, err := stream.Subscribe(snapshot.Cursor())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c.Record(contract.Event{Kind: contract.Validation, State: contract.Completed})
	c.Record(contract.Event{Kind: contract.Run, State: contract.Completed})
	for sequence := uint64(2); sequence <= 4; sequence++ {
		u := nextUpdate(t, s)
		if u.Gap || u.Event.Sequence != sequence || u.Watermark != sequence {
			t.Fatalf("lost/duplicated replay/live transition: %+v", u)
		}
		if status := stream.Snapshot(); status.Watermark < sequence || !status.Finished {
			t.Fatal("event published before authoritative status")
		}
	}
	if _, err := s.Next(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if _, err := stream.Subscribe(contract.Cursor{RunID: "foreign"}); !errors.Is(err, contract.ErrCursor) {
		t.Fatal(err)
	}
	if _, err := stream.Subscribe(contract.Cursor{RunID: snapshot.RunID, Sequence: 5}); !errors.Is(err, contract.ErrCursor) {
		t.Fatal(err)
	}
}

func TestSlowSubscriberGetsGapAndTerminalSnapshotWhileFastReaderContinues(t *testing.T) {
	c := observability.New("run-one")
	slow, _ := c.Subscribe(c.Snapshot().Cursor())
	fast, _ := c.Subscribe(c.Snapshot().Cursor())
	defer slow.Close()
	defer fast.Close()
	// Exhaust history as well as the slow queue. Fast delivery remains complete.
	for i := range observability.EventLimit + 10 {
		c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
		u := nextUpdate(t, fast)
		if u.Gap || u.Event.Sequence != uint64(i+1) {
			t.Fatalf("fast reader lost event: %+v", u)
		}
	}
	c.Record(contract.Event{Kind: contract.Run, State: contract.Uncertain})
	if u := nextUpdate(t, fast); u.Event.State != contract.Uncertain {
		t.Fatal(u)
	}
	u := nextUpdate(t, slow)
	if !u.Gap || u.Event != (contract.Event{}) || u.Watermark != observability.SubscriptionBuffer+1 {
		t.Fatal(u)
	}
	if _, err := slow.Next(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	status := c.Snapshot()
	if !status.Finished || status.Run.State != contract.Uncertain || status.Dropped == 0 {
		t.Fatal(status)
	}
	// Resume from an obsolete prefix cannot silently skip missing transitions.
	old, _ := c.Subscribe(contract.Cursor{RunID: status.RunID, Sequence: 1})
	if u := nextUpdate(t, old); !u.Gap || u.Watermark != status.Watermark {
		t.Fatal(u)
	}
	fresh, _ := c.Subscribe(status.Cursor())
	if _, err := fresh.Next(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestRejectedPayloadSignalsGapWithoutLeakingOrChangingStatus(t *testing.T) {
	c := observability.New("run-one")
	s, _ := c.Subscribe(c.Snapshot().Cursor())
	c.Record(contract.Event{Kind: "SECRET_INVALID_KIND", State: contract.Running})
	u := nextUpdate(t, s)
	if !u.Gap || u.Watermark != 1 || u.Event != (contract.Event{}) || c.Snapshot().Run != (contract.Event{}) {
		t.Fatal(u)
	}
	fresh, _ := c.Subscribe(c.Snapshot().Cursor())
	defer fresh.Close()
	c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
	if nextUpdate(t, fresh).Event.Sequence != 2 {
		t.Fatal("resnapshot did not recover")
	}
}

func TestSubscriptionBoundsCancellationAndDisconnectReleaseSlots(t *testing.T) {
	c := observability.New("run-one")
	var subscriptions []contract.Subscription
	for range observability.SubscriberLimit {
		s, err := c.Subscribe(c.Snapshot().Cursor())
		if err != nil {
			t.Fatal(err)
		}
		subscriptions = append(subscriptions, s)
		defer s.Close()
	}
	if _, err := c.Subscribe(c.Snapshot().Cursor()); !errors.Is(err, contract.ErrSubscriptionLimit) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := subscriptions[0].Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s, err := c.Subscribe(c.Snapshot().Cursor())
	if err != nil {
		t.Fatal("cancellation leaked subscriber slot", err)
	}
	s.Close()
	s.Close()
	if _, err := s.Next(t.Context()); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func TestWaitingConsumerCancellationAndTerminalQueueOverflow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := observability.New("run-one")
		s, _ := c.Subscribe(c.Snapshot().Cursor())
		ctx, cancel := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		wg.Go(func() {
			if _, err := s.Next(ctx); !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		})
		synctest.Wait() // Next is blocked on an empty queue.
		cancel()
		wg.Wait()
		slow, _ := c.Subscribe(c.Snapshot().Cursor())
		defer slow.Close()
		for range observability.SubscriptionBuffer {
			c.Record(contract.Event{Kind: contract.Run, State: contract.Running})
		}
		c.Record(contract.Event{Kind: contract.Run, State: contract.Blocked})
		if u := nextUpdate(t, slow); !u.Gap || u.Watermark != observability.SubscriptionBuffer+1 {
			t.Fatal(u)
		}
		if status := c.Snapshot(); !status.Finished || status.Run.State != contract.Blocked || status.Dropped != 0 {
			t.Fatalf("observer overflow changed final status or history loss count: %+v", status)
		}
	})
}

func TestConcurrentPublicationSnapshotsAndDisconnect(t *testing.T) {
	c := observability.New("run-one")
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				snapshot := c.Snapshot()
				s, err := c.Subscribe(snapshot.Cursor())
				if err != nil {
					t.Error(err)
					return
				}
				c.Record(contract.Event{Kind: contract.Validation, State: contract.Running})
				u := nextUpdate(t, s)
				if !u.Gap && (u.Event.Sequence <= snapshot.Watermark || c.Snapshot().Watermark < u.Event.Sequence) {
					t.Error("snapshot/subscription ordering violated")
				}
				s.Close()
			}
		})
	}
	wg.Wait()
}
