// Package observability owns process-local ordered event retention. Delivery to
// observers and durable state storage are separate responsibilities.
package observability

import (
	"slices"
	"time"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

// EventLimit includes the reserved terminal event. The collector has one core
// writer, no observers and no goroutines; reporting cannot wait for a sink.
const EventLimit = 1024

type Collector struct {
	report   contract.Report
	sequence uint64
	finished bool
}

func New(runID string) *Collector {
	return &Collector{report: contract.Report{RunID: runID}}
}

// Record stamps and orders trusted core transitions. Invalid events are dropped
// without changing execution; their missing sequences count toward Dropped.
func (c *Collector) Record(event contract.Event) {
	if c.finished {
		return
	}
	c.sequence++
	event.SchemaVersion, event.RunID = contract.SchemaVersion, c.report.RunID
	event.Sequence, event.Time = c.sequence, time.Now()
	terminal := event.Kind == contract.Run && event.State != contract.Running
	if event.Validate() != nil || (!terminal && len(c.report.Events) >= EventLimit-1) {
		c.report.Dropped++
		return
	}
	c.report.Events = append(c.report.Events, event)
	c.finished = terminal
}

// Report returns detached retained events; only the core writer may call it.
// A final event remains available even after progress retention is exhausted.
func (c *Collector) Report() contract.Report {
	report := c.report
	report.Events = slices.Clone(report.Events)
	return report
}
