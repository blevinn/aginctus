package native

import (
	"errors"
	"regexp"
	"slices"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

// AcceptedLifecycleClass is a pure, read-only intent classification. It is
// never a request to execute an Incus operation or a security decision.
type AcceptedLifecycleClass string

const (
	AcceptedBlocked          AcceptedLifecycleClass = "blocked"
	AcceptedUnchanged        AcceptedLifecycleClass = "unchanged"
	AcceptedNoStartRequested AcceptedLifecycleClass = "no-start-requested"
	AcceptedWouldStart       AcceptedLifecycleClass = "would-start-if-authorized"
)

type AcceptedLifecyclePreview struct {
	RevisionID      string                 `json:"revisionID"`
	Generation      uint64                 `json:"generation"`
	DeploymentID    string                 `json:"deploymentID"`
	IncusProject    string                 `json:"incusProject"`
	WorkloadID      string                 `json:"workloadID"`
	InstanceName    string                 `json:"instanceName"`
	Class           AcceptedLifecycleClass `json:"class"`
	Reason          string                 `json:"reason"`
	WriteAuthorized bool                   `json:"writeAuthorized"`
}

var acceptedFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PreviewAcceptedWorkloadLifecycle classifies exactly one pinned selected
// workload against an already obtained observation. It does not resolve
// mutable image aliases, create absent resources, delete unselected objects,
// stop instances when ensureRunning=false, or mutate any Incus resources.
//
// A caller-provided snapshot can be forged: only LocalRevisionPin can
// establish its same-UID provenance, and neither that provenance nor this
// preview grants controller authority. This v1alpha1 workload-only preview
// recognizes legacy deployment markers and intentionally refuses a resource
// with either environment ownership key, even if empty. A project may contain
// multiple independently versioned environments, so migration must establish
// and verify the full project/environment binding first. Even "would-start"
// is contingent on separately validated server-restricted TLS,
// volume/device/host capability policy, exclusive controller fencing and fresh
// mutation-time ETags.
func PreviewAcceptedWorkloadLifecycle(
	snapshot acceptedstate.PinnedWorkloadSnapshot, observed *InstanceObservation,
) (AcceptedLifecyclePreview, error) {
	w := snapshot.Workload
	spec := acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind:       acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion:        acceptedstate.SelectionAPIVersion,
			Kind:              acceptedstate.SelectionKind,
			DeploymentID:      snapshot.DeploymentID,
			IncusProject:      snapshot.IncusProject,
			SelectedWorkloads: []string{w.ID},
		},
		Workloads: []acceptedstate.WorkloadSpec{w},
	}
	if err := spec.Validate(); err != nil {
		return AcceptedLifecyclePreview{}, errors.New("invalid pinned workload preview selection")
	}
	if snapshot.Revision.ValidateIdentity() != nil {
		return AcceptedLifecyclePreview{}, errors.New("missing or invalid accepted revision identity")
	}
	result := AcceptedLifecyclePreview{
		RevisionID: snapshot.Revision.ID, Generation: snapshot.Revision.Generation,
		DeploymentID: snapshot.DeploymentID, IncusProject: snapshot.IncusProject,
		WorkloadID: w.ID, InstanceName: w.InstanceName,
		Class: AcceptedBlocked, Reason: "resource-unobserved",
	}
	// An unknown observation is not proof that an instance is absent and
	// cannot be reinterpreted as permission to create a replacement.
	if observed == nil {
		return result, nil
	}
	if observed.Identity.Project != snapshot.IncusProject ||
		observed.Identity.Name != w.InstanceName {
		result.Reason = "identity-mismatch"
		return result, nil
	}
	expected := map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": "workload",
		"user.aginctus.role":     "agent-workload",
		"user.aginctus.workload": w.ID,
		// Require exact accepted deployment provenance for future delegated
		// writers. Legacy project-only markers need explicit migration.
		"user.aginctus.deployment": snapshot.DeploymentID,
	}
	for key, value := range expected {
		if observed.Config[key] != value {
			result.Reason = "ownership-unverified"
			return result, nil
		}
	}
	_, hasEnvironmentID := observed.Config[environmentOwnershipKey]
	_, hasObsoleteID := observed.Config[obsoleteEnvironmentOwnershipKey]
	if hasEnvironmentID || hasObsoleteID {
		result.Reason = "ownership-unverified"
		return result, nil
	}
	if observed.State != "Running" && observed.State != "Stopped" {
		result.Reason = "lifecycle-unverified"
		return result, nil
	}
	if observed.Type != w.InstanceType || !slices.Equal(observed.Profiles, w.Profiles) {
		result.Reason = "replacement-or-profile-change-required"
		return result, nil
	}
	if !acceptedFingerprint.MatchString(observed.ImageFingerprint) ||
		observed.ImageFingerprint != w.ImageFingerprint ||
		observed.Config["volatile.base_image"] != w.ImageFingerprint {
		result.Reason = "image-provenance-mismatch"
		return result, nil
	}
	if !w.EnsureRunning {
		result.Class = AcceptedNoStartRequested
		result.Reason = "no-implicit-stop"
		return result, nil
	}
	if observed.State == "Running" {
		result.Class = AcceptedUnchanged
		result.Reason = "running"
		return result, nil
	}
	result.Class = AcceptedWouldStart
	result.Reason = "requires-independent-mutation-authorization"
	return result, nil
}
