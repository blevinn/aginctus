package acceptedstate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
)

// ReadCandidateFile loads a bounded, explicit, nonsecret typed candidate.
// It never resolves ambient XDG, environment, image aliases or Incus state.
// Callers supply both identity constraints outside the candidate file.
func ReadCandidateFile(path, deploymentID, project string) (DeploymentSpec, error) {
	if path == "" || deploymentID == "" || project == "" {
		return DeploymentSpec{}, errors.New("candidate path, deployment and project must be explicit")
	}
	f, err := os.Open(path)
	if err != nil {
		return DeploymentSpec{}, fmt.Errorf("open explicit candidate: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxSelectionBytes {
		return DeploymentSpec{}, errors.New("candidate must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSelectionBytes+1))
	if err != nil {
		return DeploymentSpec{}, errors.New("candidate file could not be read")
	}
	spec, err := ParseDeploymentSpec(data)
	if err != nil {
		return DeploymentSpec{}, errors.New("invalid typed candidate")
	}
	if spec.Selection.DeploymentID != deploymentID || spec.Selection.IncusProject != project {
		return DeploymentSpec{}, errors.New("candidate deployment/project identity mismatch")
	}
	return spec, nil
}

// CandidateComparison is a redacted, read-only report. It is not evidence of
// principal authorization and never grants permission to promote or execute.
type CandidateComparison struct {
	DeploymentID       string         `json:"deploymentID"`
	IncusProject       string         `json:"incusProject"`
	CandidateDigest    string         `json:"candidateDigest"`
	AcceptedRevisionID string         `json:"acceptedRevisionID"`
	AcceptedGeneration uint64         `json:"acceptedGeneration"`
	HasAccepted        bool           `json:"hasAccepted"`
	Changed            bool           `json:"changed"`
	Changes            DeploymentDiff `json:"changes"`
}

// CompareCandidate never writes to the local store or reconciles Incus.
// A missing accepted revision is distinct from an accepted empty selection.
func CompareCandidate(store *LocalStore, candidate DeploymentSpec) (CandidateComparison, error) {
	if store == nil {
		return CandidateComparison{}, errors.New("local store is required")
	}
	digest, err := candidate.Digest()
	if err != nil {
		return CandidateComparison{}, err
	}
	result := CandidateComparison{
		DeploymentID: candidate.Selection.DeploymentID,
		IncusProject: candidate.Selection.IncusProject,
		CandidateDigest: digest,
		Changes: DeploymentDiff{
			NewlySelected: []string{},
			Unselected: []string{},
			Modified: []WorkloadChange{},
		},
	}
	active, accepted, err := store.ReadActive()
	if err != nil {
		return CandidateComparison{}, err
	}
	if active == nil {
		result.Changes.NewlySelected = slices.Clone(candidate.Selection.SelectedWorkloads)
		slices.Sort(result.Changes.NewlySelected)
		result.Changed = true
		return result, nil
	}
	changes, err := CompareDeployments(*accepted, candidate)
	if err != nil {
		return CandidateComparison{}, err
	}
	result.HasAccepted = true
	result.AcceptedRevisionID = active.ID
	result.AcceptedGeneration = active.Generation
	result.Changed = active.SpecDigest != digest
	result.Changes = changes
	return result, nil
}
