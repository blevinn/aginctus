package environment

import (
	"errors"
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestAcceptedRevisionEnvironmentScopedAndContentAddressed(t *testing.T) {
	first := candidateFixture()
	r1, err := BuildAcceptedRevision(nil, nil, first)
	if err != nil || r1.Generation != 1 || r1.ParentID != "" {
		t.Fatalf("genesis: %+v %v", r1, err)
	}
	if err := r1.ValidateAgainst(first); err != nil {
		t.Fatal(err)
	}
	reordered := first
	reordered.SelectedWorkloads = []string{"alpha", "beta"}
	reordered.Workloads = []acceptedstate.WorkloadSpec{first.Workloads[1], first.Workloads[0]}
	if _, err := BuildAcceptedRevision(&r1, &first, reordered); !errors.Is(err, ErrAcceptedUnchanged) {
		t.Fatalf("reordered selection changed content identity: %v", err)
	}
	renamed := first
	renamed.Environment.Name = "renamed"
	r2, err := BuildAcceptedRevision(&r1, &first, renamed)
	if err != nil || r2.Generation != 2 || r2.ParentID != r1.ID || r2.ID == r1.ID {
		t.Fatalf("new accepted revision: %+v %v", r2, err)
	}
	if err := r2.ValidateAgainst(renamed); err != nil {
		t.Fatal(err)
	}
	other := first
	other.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	genesisOther, err := BuildAcceptedRevision(nil, nil, other)
	if err != nil || genesisOther.ID == r1.ID || genesisOther.Generation != 1 {
		t.Fatalf("two environment histories collapsed: %+v %v", genesisOther, err)
	}
	if _, err := BuildAcceptedRevision(&r1, &first, other); err == nil {
		t.Fatal("accepted cross-environment rewrite")
	}
	foreign := first
	foreign.Environment.Binding.Project = "other"
	if _, err := BuildAcceptedRevision(&r1, &first, foreign); err == nil {
		t.Fatal("accepted cross-project rewrite")
	}
}

func TestAcceptedRevisionRejectsForgedChainAndBody(t *testing.T) {
	first := candidateFixture()
	rev, err := BuildAcceptedRevision(nil, nil, first)
	if err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*AcceptedRevision){
		func(r *AcceptedRevision){r.ID = strings.Repeat("f",64)},
		func(r *AcceptedRevision){r.CandidateDigest = strings.Repeat("c",64)},
		func(r *AcceptedRevision){r.Generation++},
		func(r *AcceptedRevision){r.ParentID = strings.Repeat("0",64)},
		func(r *AcceptedRevision){r.ServerID = "other-server"},
		func(r *AcceptedRevision){r.Project = "default"},
		func(r *AcceptedRevision){r.EnvironmentID = "other"},
		func(r *AcceptedRevision){r.APIVersion = "unknown"},
	} {
		corrupt := rev
		modify(&corrupt)
		if err := corrupt.ValidateIdentity(); err == nil {
			t.Fatalf("accepted forged revision %+v", corrupt)
		}
	}
	tampered := first
	tampered.Workloads[0].EnsureRunning = !tampered.Workloads[0].EnsureRunning
	if err := rev.ValidateAgainst(tampered); err == nil {
		t.Fatal("accepted altered workload intent")
	}
	other := first
	other.Environment.Binding.ServerID = "foreign-server"
	if _, err := BuildAcceptedRevision(&rev, &other, first); err == nil {
		t.Fatal("accepted mismatched prior candidate")
	}
	if _, err := BuildAcceptedRevision(&rev, nil, first); err == nil {
		t.Fatal("accepted missing prior candidate")
	}
}
