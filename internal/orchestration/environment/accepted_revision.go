package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const AcceptedRevisionAPIVersion = "aginctus.dev/environment-revision/v1alpha1"

// ErrAcceptedUnchanged means the proposed candidate has the same canonical
// content as the currently accepted candidate, not that Incus converged.
var ErrAcceptedUnchanged = errors.New("environment accepted candidate is unchanged")

// ErrAcceptedConflict indicates a stale or mismatched expected revision.
// Callers must not turn it into last-writer-wins or retry in another environment.
var ErrAcceptedConflict = errors.New("environment accepted revision conflict")

// AcceptedRevision is a content identity for ONE environment inside ONE
// authenticated Incus project. It does not prove a project principal, grant
// state promotion, or authorize a native workload mutation.
type AcceptedRevision struct {
	APIVersion     string `json:"apiVersion"`
	EnvironmentID  string `json:"environmentId"`
	ServerID       string `json:"serverId"`
	Project        string `json:"project"`
	Generation     uint64 `json:"generation"`
	ParentID       string `json:"parentId"`
	CandidateDigest string `json:"candidateDigest"`
	ID             string `json:"id"`
}

type acceptedRevisionIdentity struct {
	APIVersion      string `json:"apiVersion"`
	EnvironmentID   string `json:"environmentId"`
	ServerID        string `json:"serverId"`
	Project         string `json:"project"`
	Generation      uint64 `json:"generation"`
	ParentID        string `json:"parentId"`
	CandidateDigest string `json:"candidateDigest"`
}

func (r AcceptedRevision) identity() acceptedRevisionIdentity {
	return acceptedRevisionIdentity{
		APIVersion: r.APIVersion, EnvironmentID: r.EnvironmentID,
		ServerID: r.ServerID, Project: r.Project, Generation: r.Generation,
		ParentID: r.ParentID, CandidateDigest: r.CandidateDigest,
	}
}

func acceptedRevisionDigest(value acceptedRevisionIdentity) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func validAcceptedHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, ch := range hash {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// ValidateIdentity checks the entire versioned revision ID, not just the
// length of its hash. The caller must separately authenticate the project
// and establish that this record was written by its accepted-state service.
func (r AcceptedRevision) ValidateIdentity() error {
	if r.APIVersion != AcceptedRevisionAPIVersion || r.Generation == 0 ||
		!idPattern.MatchString(r.EnvironmentID) ||
		r.ServerID == "" || r.Project == "" ||
		!validAcceptedHash(r.CandidateDigest) ||
		(r.Generation == 1) != (r.ParentID == "") ||
		(r.ParentID != "" && !validAcceptedHash(r.ParentID)) {
		return errors.New("invalid environment accepted revision envelope")
	}
	// Use the same validation as a normal enrolled environment. A display
	// name is not part of revision identity and can be changed independently.
	if err := (Descriptor{
		ID: r.EnvironmentID, Name: "revision",
		Binding: Binding{ServerID: r.ServerID, Project: r.Project},
	}).Validate(); err != nil {
		return errors.New("invalid accepted revision project binding")
	}
	expected, err := acceptedRevisionDigest(r.identity())
	if err != nil || expected != r.ID {
		return errors.New("environment accepted revision digest mismatch")
	}
	return nil
}

// ValidateAgainst additionally verifies that the immutable candidate body
// actually matches the environment, server/project and digest in the record.
func (r AcceptedRevision) ValidateAgainst(candidate Candidate) error {
	if err := r.ValidateIdentity(); err != nil {
		return err
	}
	canonical, err := candidate.Canonical()
	if err != nil {
		return errors.New("invalid accepted environment candidate")
	}
	if r.EnvironmentID != candidate.Environment.ID ||
		r.ServerID != candidate.Environment.Binding.ServerID ||
		r.Project != candidate.Environment.Binding.Project {
		return errors.New("accepted revision targets a different environment authority")
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != r.CandidateDigest {
		return errors.New("accepted revision candidate content mismatch")
	}
	return nil
}

// BuildAcceptedRevision derives the next immutable candidate revision. The
// caller supplies the fully verified prior accepted candidate when a parent
// exists; a pointer or untrusted declaration alone cannot change a binding.
// The result is only a proposed record until an independently authorized,
// durable per-environment CAS publishes it.
func BuildAcceptedRevision(
	parent *AcceptedRevision, previous *Candidate, candidate Candidate,
) (AcceptedRevision, error) {
	canonical, err := candidate.Canonical()
	if err != nil {
		return AcceptedRevision{}, fmt.Errorf("invalid environment candidate: %w", err)
	}
	if (parent == nil) != (previous == nil) {
		return AcceptedRevision{}, errors.New("accepted revision and prior candidate must be supplied together")
	}
	next := AcceptedRevision{
		APIVersion: AcceptedRevisionAPIVersion,
		EnvironmentID: candidate.Environment.ID,
		ServerID: candidate.Environment.Binding.ServerID,
		Project: candidate.Environment.Binding.Project,
		Generation: 1,
	}
	sum := sha256.Sum256(canonical)
	next.CandidateDigest = hex.EncodeToString(sum[:])
	if parent != nil {
		if err := parent.ValidateAgainst(*previous); err != nil {
			return AcceptedRevision{}, fmt.Errorf("unverified accepted parent: %w", err)
		}
		if !candidate.Environment.SameEnvironment(previous.Environment) {
			return AcceptedRevision{}, errors.New("environment identity or Incus binding changed; explicit migration required")
		}
		if parent.CandidateDigest == next.CandidateDigest {
			return AcceptedRevision{}, ErrAcceptedUnchanged
		}
		if parent.Generation == ^uint64(0) {
			return AcceptedRevision{}, errors.New("accepted revision generation overflow")
		}
		next.Generation = parent.Generation + 1
		next.ParentID = parent.ID
	}
	next.ID, err = acceptedRevisionDigest(next.identity())
	if err != nil {
		return AcceptedRevision{}, err
	}
	return next, nil
}
