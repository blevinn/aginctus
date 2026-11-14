// Package presentation adapts public reporting contracts to optional text and
// JSON-lines consumers. It neither executes operations nor decides convergence.
package presentation

import (
	"context"
	"errors"
	"io"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

// Sink is consumer-owned and used serially. Errors end only this consumer.
// A host must never call sinks from driver/evaluator hooks or the collector lock.
type Sink interface {
	WriteSnapshot(contract.Snapshot) error
	WriteUpdate(contract.Update) error
}

// Follow emits initial knowledge, subsequent updates, and final authoritative
// knowledge. Gaps are emitted explicitly before resnapshotting/resubscribing.
// Call it in a host-owned consumer goroutine with a separate consumer context;
// do not join its error into the execution outcome. No goroutine is created here.
// Writers must support host-controlled deadlines/close when they can block:
// context cancellation cannot interrupt an arbitrary io.Writer call.
func Follow(ctx context.Context, stream contract.Stream, sink Sink) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot := stream.Snapshot()
		subscription, err := stream.Subscribe(snapshot.Cursor())
		if err != nil {
			return err
		}
		gap, err := follow(ctx, subscription, snapshot, sink)
		if err != nil {
			return err
		}
		if gap {
			continue
		}
		if snapshot.Finished {
			return nil
		}
		final := stream.Snapshot()
		if !final.Finished {
			return errors.New("reporting stream ended before run completion")
		}
		return sink.WriteSnapshot(final)
	}
}

func follow(ctx context.Context, subscription contract.Subscription, snapshot contract.Snapshot, sink Sink) (bool, error) {
	defer subscription.Close()
	if err := sink.WriteSnapshot(snapshot); err != nil {
		return false, err
	}
	if snapshot.Finished {
		return false, nil
	}
	for {
		u, err := subscription.Next(ctx)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if err := sink.WriteUpdate(u); err != nil {
			return false, err
		}
		if u.Gap {
			return true, nil
		}
	}
}
