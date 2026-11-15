package observability

import "fmt"

// Snapshot is authoritative process-local execution knowledge through Watermark,
// including transitions omitted from retained history. Entries contain the latest
// event per lifecycle/check slot, not every attempt or activity code. Run is zero
// before execution starts. Absent slots have not been observed, not succeeded.
// ObservedAt remains historical evidence; neither capture nor read refreshes it.
// This is not current application health, accepted state or a durable journal.
type Snapshot struct {
	SchemaVersion string
	RunID         string
	Watermark     uint64
	Dropped       uint64
	Finished      bool
	Run           Event
	Entries       []Event
}

// Reader exposes detached snapshots of a single host-selected run. Possession is
// access to that run's nonsecret metadata, not authorization for backend reads.
// Hosts must scope distribution to the intended audience; no global registry is
// provided. Snapshot and event publication share a per-run ordering boundary.
type Reader interface {
	Snapshot() Snapshot
}

// Validate checks snapshot shape and internal correlation, not evidence truth,
// freshness, authorization or lifecycle ordering across snapshots.
func (s Snapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion || !identifier.MatchString(s.RunID) || s.Dropped > s.Watermark {
		return fmt.Errorf("invalid snapshot envelope")
	}
	if s.Run == (Event{}) {
		if s.Finished || len(s.Entries) != 0 || s.Watermark != s.Dropped {
			return fmt.Errorf("snapshot missing run state")
		}
	} else if s.Run.Validate() != nil || s.Run.RunID != s.RunID || s.Run.Kind != Run || s.Run.Sequence > s.Watermark ||
		s.Finished != (s.Run.State != Running) || (s.Finished && s.Run.Sequence != s.Watermark) {
		return fmt.Errorf("invalid snapshot run state")
	}
	var previous uint64
	for _, e := range s.Entries {
		if e.Validate() != nil || e.RunID != s.RunID || e.Kind == Run || e.Sequence <= previous || e.Sequence > s.Watermark || e.Sequence == s.Run.Sequence {
			return fmt.Errorf("invalid snapshot entry")
		}
		previous = e.Sequence
	}
	return nil
}
