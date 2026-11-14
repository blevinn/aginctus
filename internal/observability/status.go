package observability

import (
	"crypto/rand"
	"slices"
	"sync/atomic"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

// Monitor is a host-created, single-execution handle. Distribute it as a public
// Reader, not as the concrete internal host capability. It has no sink callbacks.
type Monitor struct {
	collector *Collector
	claimed   atomic.Bool
}

func NewMonitor() *Monitor { return &Monitor{collector: New(rand.Text())} }

// Claim binds the monitor once. A reused handle cannot mix runs; nil tells the
// engine to collect a separate local report instead. Telemetry misuse does not
// fail or change execution. A zero-value Monitor is unavailable.
func (m *Monitor) Claim() *Collector {
	if m == nil || m.collector == nil || !m.claimed.CompareAndSwap(false, true) {
		return nil
	}
	return m.collector
}

func (m *Monitor) Snapshot() contract.Snapshot {
	if m == nil || m.collector == nil {
		return contract.Snapshot{}
	}
	return m.collector.Snapshot()
}

var _ contract.Reader = (*Monitor)(nil)

type slot struct {
	kind  contract.Kind
	step  string
	scope contract.Scope
	phase string
	check string
}

func eventSlot(e contract.Event) slot {
	return slot{e.Kind, e.StepID, e.Scope, e.Phase, e.CheckID}
}

// apply runs before retention/publication under the collector's ordering lock.
// Retry count and arbitrary activity-code cardinality cannot grow this map.
func (c *Collector) apply(e contract.Event) {
	if e.Kind == contract.Run {
		c.run = e
		return
	}
	if e.Kind == contract.CheckAttempt && e.State == contract.Running {
		key := eventSlot(e)
		key.kind = contract.DomainProgress
		delete(c.status, key) // progress from a previous attempt is not current
	}
	c.status[eventSlot(e)] = e
}

func (c *Collector) snapshot() contract.Snapshot {
	s := contract.Snapshot{SchemaVersion: contract.SchemaVersion, RunID: c.report.RunID,
		Watermark: c.sequence, Dropped: c.report.Dropped, Finished: c.finished, Run: c.run,
		Entries: make([]contract.Event, 0, len(c.status))}
	for _, e := range c.status {
		s.Entries = append(s.Entries, e)
	}
	slices.SortFunc(s.Entries, func(a, b contract.Event) int {
		if a.Sequence < b.Sequence {
			return -1
		}
		if a.Sequence > b.Sequence {
			return 1
		}
		return 0
	})
	return s
}

func (c *Collector) Snapshot() contract.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot()
}
