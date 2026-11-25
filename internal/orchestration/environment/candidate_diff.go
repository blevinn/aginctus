package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

// CandidateDiff is a redacted, non-authorizing comparison between two
// configured environment declarations. Neither argument is an authenticated
// accepted revision. WorkloadChanges contains field names, not values.
type CandidateDiff struct {
	EnvironmentID      string                       `json:"environmentID"`
	IncusProject        string                       `json:"incusProject"`
	PreviousDigest      string                       `json:"previousDigest"`
	ProposedDigest      string                       `json:"proposedDigest"`
	DisplayNameChanged  bool                         `json:"displayNameChanged"`
	GatewayChanged      bool                         `json:"gatewayChanged"`
	Changed             bool                         `json:"changed"`
	WorkloadChanges     acceptedstate.DeploymentDiff `json:"workloadChanges"`
}

// CompareCandidates compares two complete, typed declarations for one
// explicitly enrolled environment. The observed binding MUST already have
// come from an independently authenticated Incus connection. This helper
// cannot verify that evidence or authorize state promotion or execution.
//
// A display-name change is allowed: lifecycle identity is the UUID and
// immutable server/project binding. Different environment UUIDs in the same
// project cannot be compared, merged or adopted through this API.
func CompareCandidates(enrolled Descriptor, observed Binding, previous, proposed Candidate) (CandidateDiff, error) {
	if err := enrolled.Validate(); err != nil {
		return CandidateDiff{}, fmt.Errorf("invalid enrolled environment: %w", err)
	}
	if err := enrolled.VerifyBinding(observed); err != nil {
		return CandidateDiff{}, err
	}
	for _, candidate := range []Candidate{previous, proposed} {
		if err := candidate.Validate(); err != nil {
			return CandidateDiff{}, errors.New("invalid typed environment candidate")
		}
		if !candidate.Environment.SameEnvironment(enrolled) {
			return CandidateDiff{}, errors.New("candidate environment or project binding mismatch")
		}
	}
	prior, err := previous.Canonical()
	if err != nil {
		return CandidateDiff{}, err
	}
	next, err := proposed.Canonical()
	if err != nil {
		return CandidateDiff{}, err
	}
	changes, err := acceptedstate.CompareDeployments(candidateDeployment(previous), candidateDeployment(proposed))
	if err != nil {
		return CandidateDiff{}, err
	}
	previousSHA := sha256.Sum256(prior)
	proposedSHA := sha256.Sum256(next)
	return CandidateDiff{
		EnvironmentID: enrolled.ID,
		IncusProject: enrolled.Binding.Project,
		PreviousDigest: hex.EncodeToString(previousSHA[:]),
		ProposedDigest: hex.EncodeToString(proposedSHA[:]),
		DisplayNameChanged: previous.Environment.Name != proposed.Environment.Name,
		GatewayChanged:     !sameGatewaySpec(previous.Gateway, proposed.Gateway),
		Changed: previousSHA != proposedSHA,
		WorkloadChanges: changes,
	}, nil
}

// candidateDeployment reuses workload-only field-diff semantics. This is a
// transient comparison view, not a legacy acceptance record or permission to
// translate stored DeploymentSpec revisions without an explicit migration.
func candidateDeployment(c Candidate) acceptedstate.DeploymentSpec {
	return acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind: acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion: acceptedstate.SelectionAPIVersion,
			Kind: acceptedstate.SelectionKind,
			DeploymentID: c.Environment.ID,
			IncusProject: c.Environment.Binding.Project,
			SelectedWorkloads: c.SelectedWorkloads,
		},
		Workloads: c.Workloads,
	}
}
