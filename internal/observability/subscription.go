package observability

import (
	"context"
	"io"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

// Bounds apply per run. No publisher goroutine, sink callback or unbounded queue.
const (
	SubscriptionBuffer = 64
	SubscriberLimit    = 32
)

type subscription struct {
	collector *Collector
	updates   chan contract.Update
	closed    bool // protected by collector.mu
}

func (m *Monitor) Subscribe(cursor contract.Cursor) (contract.Subscription, error) {
	if m == nil || m.collector == nil {
		return nil, contract.ErrCursor
	}
	return m.collector.Subscribe(cursor)
}

var _ contract.Stream = (*Monitor)(nil)

func (c *Collector) Subscribe(cursor contract.Cursor) (contract.Subscription, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cursor.RunID != c.report.RunID || cursor.Sequence > c.sequence {
		return nil, contract.ErrCursor
	}
	if len(c.subscribers) >= SubscriberLimit {
		return nil, contract.ErrSubscriptionLimit
	}
	s := &subscription{collector: c, updates: make(chan contract.Update, SubscriptionBuffer)}
	expected := cursor.Sequence + 1
	for _, e := range c.report.Events {
		if e.Sequence <= cursor.Sequence {
			continue
		}
		if e.Sequence != expected || len(s.updates) == SubscriptionBuffer {
			s.gap(c.sequence)
			return s, nil
		}
		s.updates <- contract.Update{Event: e, Watermark: e.Sequence}
		expected++
	}
	if expected != c.sequence+1 {
		s.gap(c.sequence)
	} else if c.finished {
		s.close()
	} else {
		c.subscribers[s] = struct{}{}
	}
	return s, nil
}

func (s *subscription) Next(ctx context.Context) (contract.Update, error) {
	if err := ctx.Err(); err != nil {
		s.Close()
		return contract.Update{}, err
	}
	select {
	case <-ctx.Done():
		s.Close()
		return contract.Update{}, ctx.Err()
	case u, ok := <-s.updates:
		if !ok {
			return contract.Update{}, io.EOF
		}
		return u, nil
	}
}

func (s *subscription) Close() {
	s.collector.mu.Lock()
	defer s.collector.mu.Unlock()
	s.close()
}

func (s *subscription) close() {
	if s.closed {
		return
	}
	s.closed = true
	delete(s.collector.subscribers, s)
	close(s.updates)
}

func (s *subscription) gap(watermark uint64) {
	// Replace pending updates with a reserved gap; no sequence-continuity claim
	// survives overflow. A concurrent consumer may already have read an earlier
	// event, but must still resnapshot on receiving this notification.
	for {
		select {
		case <-s.updates:
		default:
			s.updates <- contract.Update{Gap: true, Watermark: watermark}
			s.close()
			return
		}
	}
}

func (c *Collector) publish(e contract.Event) {
	for s := range c.subscribers {
		select {
		case s.updates <- contract.Update{Event: e, Watermark: e.Sequence}:
			if c.finished {
				s.close()
			}
		default:
			s.gap(c.sequence)
		}
	}
}

func (c *Collector) publishGap() {
	for s := range c.subscribers {
		s.gap(c.sequence)
	}
}
