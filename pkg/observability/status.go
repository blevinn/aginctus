package observability

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
