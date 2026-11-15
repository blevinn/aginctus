// Package observability owns process-local ordered event retention. Delivery to
// observers and durable state storage are separate responsibilities.
package observability

import (
	"context"
	"slices"
	"sync"
	"time"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

// EventLimit includes the reserved terminal event. Core lifecycle events and
// concurrent scoped progress share one lock/order. There
// are no sink callbacks or publisher goroutines; reporting cannot wait for a sink.
const EventLimit = 1024

type Collector struct {
	mu          sync.Mutex
	report      contract.Report
	sequence    uint64
	finished    bool
	run         contract.Event
	status      map[slot]contract.Event
	subscribers map[*subscription]struct{}
}

func New(runID string) *Collector {
	return &Collector{report: contract.Report{RunID: runID}, status: make(map[slot]contract.Event), subscribers: make(map[*subscription]struct{})}
}

// Record stamps and orders trusted core transitions. Invalid events are dropped
// without changing execution; their missing sequences count toward Dropped.
func (c *Collector) Record(event contract.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record(event)
}

func (c *Collector) record(event contract.Event) {
	if c.finished {
		return
	}
	c.sequence++
	event.SchemaVersion, event.RunID = contract.SchemaVersion, c.report.RunID
	event.Sequence, event.Time = c.sequence, time.Now()
	terminal := event.Kind == contract.Run && event.State != contract.Running
	if event.Validate() != nil {
		c.report.Dropped++
		c.publishGap()
		return
	}
	c.apply(event)
	c.finished = terminal
	if !terminal && len(c.report.Events) >= EventLimit-1 {
		c.report.Dropped++
	} else {
		c.report.Events = append(c.report.Events, event)
	}
	c.publish(event)
}

// Report returns detached retained events under the same ordering lock.
// A final event remains available even after progress retention is exhausted.
func (c *Collector) Report() contract.Report {
	c.mu.Lock()
	defer c.mu.Unlock()
	report := c.report
	report.Events = slices.Clone(report.Events)
	report.Status = c.snapshot()
	return report
}

type progressKey struct{}

type progressBinding struct {
	collector *Collector
	event     contract.Event
}

// WithProgress binds host-owned correlation for one actual check attempt. A nil
// collector explicitly masks inherited bindings when reporting is disabled.
func WithProgress(ctx context.Context, collector *Collector, event contract.Event) context.Context {
	return context.WithValue(ctx, progressKey{}, progressBinding{collector, event})
}

// Progress returns a reporter bound to this exact context, including a runtime
// adapter's shorter callback lifetime. Only internal hosts can create bindings.
func Progress(ctx context.Context) contract.Reporter {
	binding, ok := ctx.Value(progressKey{}).(progressBinding)
	if !ok || binding.collector == nil {
		return nil
	}
	return scopedProgress{ctx, binding}
}

type scopedProgress struct {
	ctx context.Context
	progressBinding
}

func (p scopedProgress) Report(progress contract.Progress) {
	p.collector.mu.Lock()
	defer p.collector.mu.Unlock()
	if p.ctx.Err() != nil {
		return
	}
	if deadline, ok := p.ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return
	}
	event := p.event
	event.Kind, event.State, event.Reason = contract.DomainProgress, contract.Running, contract.NoReason
	event.Progress = progress
	p.collector.record(event)
}
