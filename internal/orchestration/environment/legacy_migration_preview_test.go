package environment

import (
	"reflect"
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func legacyMigrationFixture(t *testing.T) (acceptedstate.DeploymentSpec, string, Descriptor, []LegacyWorkloadMapping) {
	t.Helper()
	c := candidateFixture()
	legacy := acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind:       acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion:        acceptedstate.SelectionAPIVersion,
			Kind:              acceptedstate.SelectionKind,
			DeploymentID:      "old-deployment",
			IncusProject:      c.Environment.Binding.Project,
			SelectedWorkloads: []string{"beta", "alpha"},
		},
		Workloads: append([]acceptedstate.WorkloadSpec(nil), c.Workloads...),
	}
	// Exercise profile-slice isolation with ordered profile data on both
	// workloads; the shared candidate fixture intentionally has an empty list.
	legacy.Workloads[1].Profiles = []string{"base", "alpha-extra"}
	digest, err := legacy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	mappings := []LegacyWorkloadMapping{
		{LegacyID: "beta", TargetID: "new-beta", InstanceName: "beta-instance"},
		{LegacyID: "alpha", TargetID: "new-alpha", InstanceName: "alpha-instance"},
	}
	return legacy, digest, c.Environment, mappings
}

func TestPreviewLegacyCandidateExactMappingNoWrites(t *testing.T) {
	legacy, digest, target, mappings := legacyMigrationFixture(t)
	originalWorkloads := append([]acceptedstate.WorkloadSpec(nil), legacy.Workloads...)
	preview, err := PreviewLegacyCandidate(legacy, digest, target, mappings)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EnvironmentID != target.ID || preview.LegacyDigest != digest ||
		preview.LegacyDeploymentID != "old-deployment" ||
		!reflect.DeepEqual(preview.Candidate.SelectedWorkloads, []string{"new-alpha", "new-beta"}) {
		t.Fatalf("unexpected migration preview: %+v", preview)
	}
	if preview.Candidate.Workloads[0].InstanceName != "alpha-instance" ||
		preview.Candidate.Workloads[0].ImageFingerprint != strings.Repeat("a", 64) ||
		preview.Candidate.Workloads[1].InstanceName != "beta-instance" {
		t.Fatal("preview silently renamed or lost pinned workload")
	}
	if !reflect.DeepEqual(legacy.Workloads, originalWorkloads) || legacy.Workloads[0].ID != "beta" {
		t.Fatal("mutated legacy source")
	}
	preview.Candidate.Workloads[0].Profiles[0] = "changed"
	if legacy.Workloads[1].Profiles[0] == "changed" {
		t.Fatal("preview aliased legacy profile slices")
	}
}

func TestPreviewLegacyCandidateRejectsUnsafeConversions(t *testing.T) {
	legacy, digest, target, mappings := legacyMigrationFixture(t)
	cases := []struct {
		name   string
		modify func(*acceptedstate.DeploymentSpec, *string, *Descriptor, *[]LegacyWorkloadMapping)
	}{
		{"unverified digest", func(_ *acceptedstate.DeploymentSpec, d *string, _ *Descriptor, _ *[]LegacyWorkloadMapping) { *d = "" }},
		{"wrong digest", func(_ *acceptedstate.DeploymentSpec, d *string, _ *Descriptor, _ *[]LegacyWorkloadMapping) {
			*d = strings.Repeat("f", 64)
		}},
		{"other project", func(_ *acceptedstate.DeploymentSpec, _ *string, d *Descriptor, _ *[]LegacyWorkloadMapping) {
			d.Binding.Project = "another"
		}},
		{"default project", func(_ *acceptedstate.DeploymentSpec, _ *string, d *Descriptor, _ *[]LegacyWorkloadMapping) {
			d.Binding.Project = "default"
		}},
		{"missing mapping", func(_ *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, m *[]LegacyWorkloadMapping) {
			*m = (*m)[:1]
		}},
		{"duplicate target", func(_ *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, m *[]LegacyWorkloadMapping) {
			(*m)[1].TargetID = (*m)[0].TargetID
		}},
		{"duplicate source", func(_ *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, m *[]LegacyWorkloadMapping) {
			(*m)[1].LegacyID = (*m)[0].LegacyID
		}},
		{"unlisted source", func(_ *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, m *[]LegacyWorkloadMapping) {
			(*m)[1].LegacyID = "other"
		}},
		{"implicit rename", func(_ *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, m *[]LegacyWorkloadMapping) {
			(*m)[1].InstanceName = "new-instance"
		}},
		{"invalid target ID", func(_ *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, m *[]LegacyWorkloadMapping) {
			(*m)[1].TargetID = "../escape"
		}},
		{"invalid legacy spec", func(l *acceptedstate.DeploymentSpec, _ *string, _ *Descriptor, _ *[]LegacyWorkloadMapping) {
			l.Workloads[0].ImageFingerprint = "mutable"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := legacy
			l.Workloads = append([]acceptedstate.WorkloadSpec(nil), legacy.Workloads...)
			d := digest
			targetCopy := target
			m := append([]LegacyWorkloadMapping(nil), mappings...)
			tc.modify(&l, &d, &targetCopy, &m)
			if _, err := PreviewLegacyCandidate(l, d, targetCopy, m); err == nil {
				t.Fatal("unsafe or unpinned migration accepted")
			}
		})
	}
}
