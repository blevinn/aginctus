package acceptedstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const RevisionAPIVersion = "aginctus.dev/revision/v1alpha1"

var ErrNoChanges = errors.New("accepted deployment intent is unchanged")

// Revision is a cryptographic CONTENT IDENTIFIER for a proposed immutable
// accepted revision, not a signature, credential, ownership claim or permission
// to mutate infrastructure. Promotion authority belongs to a separate service.
type Revision struct {
	APIVersion string `json:"apiVersion"`
	Generation uint64 `json:"generation"`
	ParentID   string `json:"parentID"`
	SpecDigest string `json:"specDigest"`
	ID         string `json:"id"`
}

type revisionIdentity struct {
	APIVersion string `json:"apiVersion"`
	Generation uint64 `json:"generation"`
	ParentID   string `json:"parentID"`
	SpecDigest string `json:"specDigest"`
}

func (r Revision) validateDigest(spec DeploymentSpec) error {
	digest, err := spec.Digest()
	if err != nil {
		return err
	}
	if digest != r.SpecDigest {
		return errors.New("accepted revision spec digest mismatch")
	}
	return r.validateIdentity()
}

func (r Revision) validateIdentity() error {
	if r.APIVersion != RevisionAPIVersion || r.Generation == 0 ||
		(r.Generation == 1) != (r.ParentID == "") ||
		!fingerprintHex(r.SpecDigest) || len(r.SpecDigest) != 64 ||
		(r.ParentID != "" && (len(r.ParentID) != 64 || !fingerprintHex(r.ParentID))) {
		return errors.New("invalid accepted revision envelope")
	}
	identity := revisionIdentity{r.APIVersion, r.Generation, r.ParentID, r.SpecDigest}
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if r.ID != hex.EncodeToString(sum[:]) {
		return errors.New("accepted revision ID mismatch")
	}
	return nil
}

// BuildRevision is pure. It does not establish who can accept a revision.
// A store must independently enforce CAS under lock before publishing.
func BuildRevision(parent *Revision, candidate DeploymentSpec) (Revision, error) {
	digest, err := candidate.Digest()
	if err != nil {
		return Revision{}, err
	}
	r := Revision{APIVersion: RevisionAPIVersion, Generation: 1, SpecDigest: digest}
	if parent != nil {
		if parent.validateIdentity() != nil {
			return Revision{}, errors.New("invalid parent revision")
		}
		if parent.SpecDigest == digest {
			return Revision{}, ErrNoChanges
		}
		r.Generation = parent.Generation + 1
		if r.Generation == 0 {
			return Revision{}, errors.New("revision generation overflow")
		}
		r.ParentID = parent.ID
	}
	data, err := json.Marshal(revisionIdentity{r.APIVersion, r.Generation, r.ParentID, r.SpecDigest})
	if err != nil {
		return Revision{}, err
	}
	sum := sha256.Sum256(data)
	r.ID = hex.EncodeToString(sum[:])
	return r, nil
}

type WorkloadChange struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}

// DeploymentDiff contains FIELD NAMES, never values or secret-derived digests.
// Unselected resource IDs are not requests to delete those resources.
type DeploymentDiff struct {
	NewlySelected []string         `json:"newlySelected"`
	Unselected    []string         `json:"unselected"`
	Modified      []WorkloadChange `json:"modified"`
}

func CompareDeployments(active, candidate DeploymentSpec) (DeploymentDiff, error) {
	if err := active.Validate(); err != nil {
		return DeploymentDiff{}, err
	}
	if err := candidate.Validate(); err != nil {
		return DeploymentDiff{}, err
	}
	if active.Selection.DeploymentID != candidate.Selection.DeploymentID ||
		active.Selection.IncusProject != candidate.Selection.IncusProject {
		return DeploymentDiff{}, errors.New("deployment or project identity change requires separate migration")
	}
	sel, err := CompareSelections(active.Selection, candidate.Selection)
	if err != nil {
		return DeploymentDiff{}, fmt.Errorf("selection diff: %w", err)
	}
	result := DeploymentDiff{NewlySelected: sel.NewlySelected, Unselected: sel.Unselected, Modified: []WorkloadChange{}}
	old := map[string]WorkloadSpec{}
	for _, w := range active.Workloads {
		old[w.ID] = w
	}
	for _, w := range candidate.Workloads {
		was, ok := old[w.ID]
		if !ok {
			continue
		}
		fields := []string{}
		if w.InstanceName != was.InstanceName {
			fields = append(fields, "instanceName")
		}
		if w.InstanceType != was.InstanceType {
			fields = append(fields, "instanceType")
		}
		if w.ImageFingerprint != was.ImageFingerprint {
			fields = append(fields, "imageFingerprint")
		}
		if !slices.Equal(w.Profiles, was.Profiles) {
			fields = append(fields, "profiles")
		}
		if w.EnsureRunning != was.EnsureRunning {
			fields = append(fields, "ensureRunning")
		}
		if len(fields) > 0 {
			result.Modified = append(result.Modified, WorkloadChange{w.ID, fields})
		}
	}
	slices.SortFunc(result.Modified, func(a, b WorkloadChange) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result, nil
}
