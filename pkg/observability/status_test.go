package observability_test

import (
	"testing"
	"time"

	contract "github.com/blevinn/aginctus/pkg/observability"
)

func TestSnapshotValidationRejectsMixedWatermarksAndInventedTerminalState(t *testing.T) {
	e := contract.Event{SchemaVersion: contract.SchemaVersion, RunID: "run-one", Sequence: 1, Time: time.Now(), Kind: contract.Run, State: contract.Running}
	base := contract.Snapshot{SchemaVersion: contract.SchemaVersion, RunID: e.RunID, Watermark: 1, Run: e}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*contract.Snapshot){
		func(s *contract.Snapshot) { s.SchemaVersion = "unknown" },
		func(s *contract.Snapshot) { s.RunID = "another-run" },
		func(s *contract.Snapshot) { s.Dropped = 2 },
		func(s *contract.Snapshot) { s.Finished = true },
		func(s *contract.Snapshot) { s.Watermark = 0 },
		func(s *contract.Snapshot) { s.Entries = []contract.Event{e} },
		func(s *contract.Snapshot) { s.Run.State = contract.Completed; s.Finished = true; s.Watermark = 2 },
	} {
		s := base
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid snapshot accepted: %+v", s)
		}
	}
}
