package native

import (
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func acceptedPreviewFixture(t *testing.T) (acceptedstate.PinnedWorkloadSnapshot, InstanceObservation) {
	t.Helper()
	workload := acceptedstate.WorkloadSpec{
		ID: "dev", InstanceName: "dev-instance", InstanceType: "container",
		ImageFingerprint: strings.Repeat("b", 64), Profiles: []string{"base"},
		EnsureRunning: true,
	}
	spec := acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind:       acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion:   acceptedstate.SelectionAPIVersion,
			Kind:         acceptedstate.SelectionKind,
			DeploymentID: "demo", IncusProject: "aginctus",
			SelectedWorkloads: []string{"dev"},
		},
		Workloads: []acceptedstate.WorkloadSpec{workload},
	}
	revision, err := acceptedstate.BuildRevision(nil, spec)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := acceptedstate.PinnedWorkloadSnapshot{
		Revision: revision, DeploymentID: "demo",
		IncusProject: "aginctus", Workload: workload,
	}
	observed := InstanceObservation{
		Identity: InstanceIdentity{Project: "aginctus", Name: "dev-instance"},
		Type:     "container", State: "Stopped",
		ImageFingerprint: workload.ImageFingerprint,
		Profiles:         []string{"base"},
		Config: map[string]string{
			"user.aginctus.managed":    "true",
			"user.aginctus.resource":   "workload",
			"user.aginctus.role":       "agent-workload",
			"user.aginctus.workload":   "dev",
			"user.aginctus.deployment": "demo",
			"volatile.base_image":      workload.ImageFingerprint,
		},
	}
	return snapshot, observed
}

func TestAcceptedLifecyclePreviewNeverAuthorizesIncusWrites(t *testing.T) {
	snapshot, observed := acceptedPreviewFixture(t)
	preview, err := PreviewAcceptedWorkloadLifecycle(snapshot, &observed)
	if err != nil || preview.Class != AcceptedWouldStart || preview.WriteAuthorized ||
		preview.RevisionID != snapshot.Revision.ID || preview.Reason != "requires-independent-mutation-authorization" {
		t.Fatalf("unsafe preview: %+v %v", preview, err)
	}
	if observed.State != "Stopped" || snapshot.Workload.EnsureRunning != true {
		t.Fatal("pure preview changed inputs")
	}
	observed.State = "Running"
	preview, err = PreviewAcceptedWorkloadLifecycle(snapshot, &observed)
	if err != nil || preview.Class != AcceptedUnchanged || preview.WriteAuthorized {
		t.Fatalf("running instance not idempotent: %+v %v", preview, err)
	}
	snapshot.Workload.EnsureRunning = false
	preview, err = PreviewAcceptedWorkloadLifecycle(snapshot, &observed)
	if err != nil || preview.Class != AcceptedNoStartRequested || preview.WriteAuthorized {
		t.Fatalf("false ensureRunning caused implicit stop: %+v %v", preview, err)
	}
	observed.State = "Stopped"
	preview, err = PreviewAcceptedWorkloadLifecycle(snapshot, &observed)
	if err != nil || preview.Class != AcceptedNoStartRequested {
		t.Fatalf("false ensureRunning caused start: %+v %v", preview, err)
	}
}

func TestAcceptedLifecyclePreviewBlocksUnverifiedOrForeignResources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		alter  func(*acceptedstate.PinnedWorkloadSnapshot, *InstanceObservation)
		reason string
	}{
		{"wrong-instance", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) { o.Identity.Name = "foreign" }, "identity-mismatch"},
		{"wrong-project", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) { o.Identity.Project = "foreign" }, "identity-mismatch"},
		{"missing-owner", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) {
			delete(o.Config, "user.aginctus.managed")
		}, "ownership-unverified"},
		{"wrong-deployment", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) {
			o.Config["user.aginctus.deployment"] = "foreign"
		}, "ownership-unverified"},
		{"legacy-deployment", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) {
			delete(o.Config, "user.aginctus.deployment")
		}, "ownership-unverified"},
		{"environment-owned-same-project", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) {
			o.Config["user.aginctus.environment-id"] = "7c82e6d1-1212-4123-8123-0123456789ab"
		}, "ownership-unverified"},
		{"unknown-state", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) { o.State = "Error" }, "lifecycle-unverified"},
		{"wrong-type", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) { o.Type = "virtual-machine" }, "replacement-or-profile-change-required"},
		{"wrong-profiles", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) { o.Profiles = []string{"other"} }, "replacement-or-profile-change-required"},
		{"wrong-image", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) {
			o.ImageFingerprint = strings.Repeat("a", 64)
		}, "image-provenance-mismatch"},
		{"wrong-base-image", func(_ *acceptedstate.PinnedWorkloadSnapshot, o *InstanceObservation) {
			o.Config["volatile.base_image"] = strings.Repeat("a", 64)
		}, "image-provenance-mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, observed := acceptedPreviewFixture(t)
			tc.alter(&snapshot, &observed)
			preview, err := PreviewAcceptedWorkloadLifecycle(snapshot, &observed)
			if err != nil || preview.Class != AcceptedBlocked || preview.Reason != tc.reason || preview.WriteAuthorized {
				t.Fatalf("untrusted observation yielded action: %+v %v", preview, err)
			}
		})
	}
}

func TestAcceptedLifecyclePreviewRejectsInvalidAcceptedIntentAndNoObservation(t *testing.T) {
	snapshot, _ := acceptedPreviewFixture(t)
	preview, err := PreviewAcceptedWorkloadLifecycle(snapshot, nil)
	if err != nil || preview.Class != AcceptedBlocked || preview.Reason != "resource-unobserved" {
		t.Fatalf("missing observation treated as absent writable instance: %+v %v", preview, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*acceptedstate.PinnedWorkloadSnapshot)
	}{
		{"default", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.IncusProject = "default" }},
		{"unsafe-instance", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Workload.InstanceName = "../foreign" }},
		{"alias-image", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Workload.ImageFingerprint = "latest" }},
		{"implicit-profiles", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Workload.Profiles = nil }},
		{"absent-revision", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Revision.ID = "" }},
		{"missing-digest", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Revision.SpecDigest = "" }},
		{"forged-generation", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Revision.Generation++ }},
		{"forged-digest", func(s *acceptedstate.PinnedWorkloadSnapshot) { s.Revision.SpecDigest = strings.Repeat("f", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := acceptedPreviewFixture(t)
			tc.edit(&s)
			if _, err := PreviewAcceptedWorkloadLifecycle(s, nil); err == nil {
				t.Fatal("invalid pinned workload accepted")
			}
		})
	}
}
