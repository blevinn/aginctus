package environment

import (
	"errors"
	"fmt"
	"slices"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

// LegacyWorkloadMapping is an explicit, reviewed mapping from a legacy
// workload identity to a proposed environment workload identity. An instance
// name is retained byte-for-byte; changes in physical identity require a
// separate migration plan and real Incus ownership verification.
type LegacyWorkloadMapping struct {
	LegacyID     string `json:"legacyId"`
	TargetID     string `json:"targetId"`
	InstanceName string `json:"instanceName"`
}

// LegacyCandidatePreview is a NON-AUTHORIZING transformed declaration.
// Neither source integrity nor a matching instance name proves resource
// ownership. It must not be written to accepted state or used for adoption
// without backup, independent Incus identity and explicit operator approval.
type LegacyCandidatePreview struct {
	LegacyDeploymentID string                  `json:"legacyDeploymentId"`
	LegacyDigest       string                  `json:"legacyDigest"`
	EnvironmentID      string                  `json:"environmentId"`
	Mappings           []LegacyWorkloadMapping `json:"mappings"`
	Candidate          Candidate               `json:"candidate"`
}

// PreviewLegacyCandidate refuses implicit scope or workload mappings. The
// caller must already have selected the legacy spec from a trustworthy source
// and pinned its expected digest. This function performs no I/O or mutation.
// Profiles and immutable fingerprints are copied unmodified; secret, volume,
// host and project-wide resource migration is explicitly out of scope.
func PreviewLegacyCandidate(
	legacy acceptedstate.DeploymentSpec, expectedLegacyDigest string,
	target Descriptor, mappings []LegacyWorkloadMapping,
) (LegacyCandidatePreview, error) {
	if err := legacy.Validate(); err != nil {
		return LegacyCandidatePreview{}, errors.New("invalid legacy workload deployment")
	}
	if err := target.Validate(); err != nil {
		return LegacyCandidatePreview{}, errors.New("invalid target environment descriptor")
	}
	digest, err := legacy.Digest()
	if err != nil {
		return LegacyCandidatePreview{}, err
	}
	if expectedLegacyDigest == "" || expectedLegacyDigest != digest {
		return LegacyCandidatePreview{}, errors.New("legacy source digest is not explicitly pinned")
	}
	if legacy.Selection.IncusProject != target.Binding.Project {
		return LegacyCandidatePreview{}, errors.New("legacy project relocation requires separate migration")
	}
	if len(mappings) != len(legacy.Workloads) {
		return LegacyCandidatePreview{}, errors.New("all legacy workloads require explicit mapping")
	}
	byID := make(map[string]acceptedstate.WorkloadSpec, len(legacy.Workloads))
	for _, w := range legacy.Workloads {
		byID[w.ID] = w
	}
	seenOld := make(map[string]bool, len(mappings))
	seenNew := make(map[string]bool, len(mappings))
	candidate := Candidate{
		APIVersion: CandidateAPIVersion, Kind: CandidateKind,
		Environment: target, SelectedWorkloads: make([]string, 0, len(mappings)),
		Workloads: make([]acceptedstate.WorkloadSpec, 0, len(mappings)),
	}
	copiedMappings := slices.Clone(mappings)
	for _, mapping := range copiedMappings {
		old, ok := byID[mapping.LegacyID]
		if !ok || mapping.TargetID == "" || mapping.InstanceName != old.InstanceName ||
			seenOld[mapping.LegacyID] || seenNew[mapping.TargetID] {
			return LegacyCandidatePreview{}, errors.New("ambiguous, missing or renamed legacy workload mapping")
		}
		seenOld[mapping.LegacyID] = true
		seenNew[mapping.TargetID] = true
		old.ID = mapping.TargetID
		old.Profiles = slices.Clone(old.Profiles)
		candidate.Workloads = append(candidate.Workloads, old)
		candidate.SelectedWorkloads = append(candidate.SelectedWorkloads, mapping.TargetID)
	}
	if err := candidate.Validate(); err != nil {
		return LegacyCandidatePreview{}, fmt.Errorf("mapped candidate is invalid: %w", err)
	}
	// Make ordering canonical without changing profile order or source data.
	slices.Sort(candidate.SelectedWorkloads)
	slices.SortFunc(candidate.Workloads, func(a, b acceptedstate.WorkloadSpec) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	slices.SortFunc(copiedMappings, func(a, b LegacyWorkloadMapping) int {
		if a.LegacyID < b.LegacyID {
			return -1
		}
		if a.LegacyID > b.LegacyID {
			return 1
		}
		return 0
	})
	return LegacyCandidatePreview{
		LegacyDeploymentID: legacy.Selection.DeploymentID,
		LegacyDigest:       digest, EnvironmentID: target.ID,
		Mappings: copiedMappings, Candidate: candidate,
	}, nil
}
