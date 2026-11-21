package environment

import (
	"encoding/json"
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestCompareCandidatesIdenticalAndOrderInvariant(t *testing.T) {
	prior := candidateFixture()
	next := candidateFixture()
	next.SelectedWorkloads[0], next.SelectedWorkloads[1] = next.SelectedWorkloads[1], next.SelectedWorkloads[0]
	next.Workloads[0], next.Workloads[1] = next.Workloads[1], next.Workloads[0]
	report, err := CompareCandidates(prior.Environment, prior.Environment.Binding, prior, next)
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed || report.DisplayNameChanged || report.PreviousDigest != report.ProposedDigest {
		t.Fatalf("order-only changes should be ignored: %+v", report)
	}
	if len(report.PreviousDigest) != 64 || report.EnvironmentID != prior.Environment.ID || report.IncusProject != "dev_team" {
		t.Fatal("comparison omitted expected scope and content digest")
	}
}

func TestCompareCandidatesReportsOnlyNamesAndFields(t *testing.T) {
	prior := candidateFixture()
	next := candidateFixture()
	next.Environment.Name = "renamed"
	next.Workloads[0].ImageFingerprint = strings.Repeat("c", 64)
	next.Workloads[1].EnsureRunning = true
	report, err := CompareCandidates(prior.Environment, prior.Environment.Binding, prior, next)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Changed || !report.DisplayNameChanged || len(report.WorkloadChanges.Modified) != 2 {
		t.Fatalf("missing changed fields: %+v", report)
	}
	for _, change := range report.WorkloadChanges.Modified {
		if change.ID == "beta" && (len(change.Fields) != 1 || change.Fields[0] != "imageFingerprint") {
			t.Fatalf("unexpected beta change: %+v", change)
		}
		if change.ID == "alpha" && (len(change.Fields) != 1 || change.Fields[0] != "ensureRunning") {
			t.Fatalf("unexpected alpha change: %+v", change)
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), strings.Repeat("c", 64)) {
		t.Fatal("comparison disclosed an image value rather than its field name")
	}
	if prior.Workloads[0].ImageFingerprint != strings.Repeat("b", 64) {
		t.Fatal("comparison mutated its source")
	}
}

func TestCompareCandidatesRejectsCrossEnvironmentAndProject(t *testing.T) {
	prior := candidateFixture()
	for _, tc := range []struct {
		name string
		edit func(*Candidate, *Binding)
	}{
		{"different UUID same project", func(c *Candidate, _ *Binding) {
			c.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
		}},
		{"different project", func(c *Candidate, _ *Binding) { c.Environment.Binding.Project = "other" }},
		{"different server", func(c *Candidate, _ *Binding) { c.Environment.Binding.ServerID = "other-server" }},
		{"invalid workload", func(c *Candidate, _ *Binding) { c.Workloads[0].Profiles = nil }},
		{"observed project redirect", func(_ *Candidate, b *Binding) { b.Project = "other" }},
		{"observed server redirect", func(_ *Candidate, b *Binding) { b.ServerID = "other-server" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := candidateFixture()
			observed := prior.Environment.Binding
			tc.edit(&next, &observed)
			if _, err := CompareCandidates(prior.Environment, observed, prior, next); err == nil {
				t.Fatal("accepted mismatched environment, project or binding")
			}
		})
	}
}

func TestCompareCandidatesSelectionIsNotDeletionDirective(t *testing.T) {
	prior := candidateFixture()
	next := candidateFixture()
	next.SelectedWorkloads = []string{"alpha"}
	next.Workloads = []acceptedstate.WorkloadSpec{next.Workloads[1]}
	report, err := CompareCandidates(prior.Environment, prior.Environment.Binding, prior, next)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Changed || len(report.WorkloadChanges.Unselected) != 1 || report.WorkloadChanges.Unselected[0] != "beta" {
		t.Fatalf("missing unselected workload evidence: %+v", report)
	}
	if len(report.WorkloadChanges.NewlySelected) != 0 {
		t.Fatal("invented new selections")
	}
}
