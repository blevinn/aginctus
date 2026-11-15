package observability

import (
	"context"
	"errors"
)

var (
	ErrCursor            = errors.New("invalid run cursor")
	ErrSubscriptionLimit = errors.New("run subscription limit reached")
)

// Cursor has meaning only within RunID. Use a snapshot's watermark, never a wall
// clock or a sequence borrowed from another run.
type Cursor struct {
	RunID    string
	Sequence uint64
}

func (s Snapshot) Cursor() Cursor { return Cursor{s.RunID, s.Watermark} }

// Update is either one event or an explicit gap through Watermark (zero Event).
// On a gap the subscription ends: obtain a new snapshot and subscribe from its
// cursor. No omitted/rejected payload is included in a gap notification.
type Update struct {
	Event     Event
	Gap       bool
	Watermark uint64
}

// Subscription is bounded best-effort delivery. Next returns io.EOF after queued
// updates drain on completion/Close/gap. Cancellation closes the subscription and
// returns the context error. Close is idempotent; callers must close abandoned
// subscriptions. One consumer calls Next; Close may be concurrent. No callbacks
// run inside the executor. Sink failures must not be propagated to execution.
type Subscription interface {
	Next(context.Context) (Update, error)
	Close()
}

// Stream resumes strictly after a cursor, either continuously or with an explicit
// gap requiring a fresh snapshot. Snapshot/subscribe races cannot hide transitions.
// Hosts distribute only the intended run's Stream to authorized consumers.
type Stream interface {
	Reader
	Subscribe(Cursor) (Subscription, error)
}
