// Package acceptedstate defines pure, non-mutating building blocks for
// prospective accepted deployment revisions. It does not grant authority,
// persist revisions or reconcile Incus resources.
package acceptedstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

const (
	SelectionAPIVersion  = "aginctus.dev/accepted/v1alpha1"
	SelectionKind        = "SelectionManifest"
	maxSelectionBytes    = 1 << 20
	maxSelectedWorkloads = 256
)

var localID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// SelectionManifest is a deliberately narrow, secret-free resource-selection
// envelope, NOT a complete accepted deployment specification. It has no
// configuration payload, host policy, ownership delegation or write capability.
// selectedWorkloads is an explicit set: absence is not a deletion directive.
type SelectionManifest struct {
	APIVersion        string   `json:"apiVersion"`
	Kind              string   `json:"kind"`
	DeploymentID      string   `json:"deploymentID"`
	IncusProject      string   `json:"incusProject"`
	SelectedWorkloads []string `json:"selectedWorkloads"`
}

// Validate rejects ambiguous identities and implicit/default project scope.
func (s SelectionManifest) Validate() error {
	if s.APIVersion != SelectionAPIVersion || s.Kind != SelectionKind {
		return errors.New("unsupported selection manifest version or kind")
	}
	if !localID.MatchString(s.DeploymentID) {
		return errors.New("invalid deployment identity")
	}
	if _, err := incusscope.NewProjectScope(s.IncusProject); err != nil {
		return errors.New("invalid or default Incus project")
	}
	if s.SelectedWorkloads == nil || len(s.SelectedWorkloads) > maxSelectedWorkloads {
		return errors.New("selected workloads must be an explicit bounded list")
	}
	seen := make(map[string]bool, len(s.SelectedWorkloads))
	for _, name := range s.SelectedWorkloads {
		if !localID.MatchString(name) {
			return errors.New("invalid selected workload identity")
		}
		if seen[name] {
			return errors.New("duplicate selected workload identity")
		}
		seen[name] = true
	}
	return nil
}

// Canonical returns independent, stable bytes without changing the caller's
// list. This digestible envelope contains identifiers only, never credentials.
func (s SelectionManifest) Canonical() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	s.SelectedWorkloads = append([]string{}, s.SelectedWorkloads...)
	slices.Sort(s.SelectedWorkloads)
	return json.Marshal(s)
}

// Digest identifies a canonical selection, not an authorization decision.
// Never use this hash to claim the whole deployment intent has been accepted.
func (s SelectionManifest) Digest() (string, error) {
	payload, err := s.Canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// SelectionDiff reports identity-only changes. Unselected is deliberately
// distinct from a delete action: an omitted workload remains unmanaged by
// this particular selection, not automatically removable from Incus.
type SelectionDiff struct {
	NewlySelected []string `json:"newlySelected"`
	Unselected    []string `json:"unselected"`
}

func CompareSelections(active, candidate SelectionManifest) (SelectionDiff, error) {
	if err := active.Validate(); err != nil {
		return SelectionDiff{}, err
	}
	if err := candidate.Validate(); err != nil {
		return SelectionDiff{}, err
	}
	if active.DeploymentID != candidate.DeploymentID || active.IncusProject != candidate.IncusProject {
		return SelectionDiff{}, errors.New("deployment or Incus project identity differs")
	}
	a := make(map[string]bool, len(active.SelectedWorkloads))
	b := make(map[string]bool, len(candidate.SelectedWorkloads))
	for _, name := range active.SelectedWorkloads {
		a[name] = true
	}
	for _, name := range candidate.SelectedWorkloads {
		b[name] = true
	}
	diff := SelectionDiff{NewlySelected: []string{}, Unselected: []string{}}
	for name := range b {
		if !a[name] {
			diff.NewlySelected = append(diff.NewlySelected, name)
		}
	}
	for name := range a {
		if !b[name] {
			diff.Unselected = append(diff.Unselected, name)
		}
	}
	slices.Sort(diff.NewlySelected)
	slices.Sort(diff.Unselected)
	return diff, nil
}

// ParseSelection accepts only one strict JSON document. A separate token pass
// rejects duplicate keys that encoding/json's struct decoder would overwrite,
// and rejects ambiguous future schema fields rather than silently ignoring them.
func ParseSelection(payload []byte) (SelectionManifest, error) {
	if len(payload) == 0 || len(payload) > maxSelectionBytes {
		return SelectionManifest{}, errors.New("invalid selection manifest size")
	}
	if err := rejectDuplicateKeys(payload); err != nil {
		return SelectionManifest{}, errors.New("invalid or duplicate selection JSON keys")
	}
	// Accept only exact field names. encoding/json normally matches struct
	// tags case-insensitively, which would admit ambiguous spellings.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(payload, &keys); err != nil || keys == nil {
		return SelectionManifest{}, errors.New("invalid selection manifest JSON")
	}
	for key := range keys {
		switch key {
		case "apiVersion", "kind", "deploymentID", "incusProject", "selectedWorkloads":
		default:
			return SelectionManifest{}, errors.New("unknown selection manifest field")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var manifest SelectionManifest
	if err := decoder.Decode(&manifest); err != nil {
		return SelectionManifest{}, errors.New("invalid selection manifest JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return SelectionManifest{}, errors.New("unexpected trailing selection content")
	}
	if err := manifest.Validate(); err != nil {
		return SelectionManifest{}, fmt.Errorf("validate selection manifest: %w", err)
	}
	return manifest, nil
}

func rejectDuplicateKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[strings.ToLower(name)] {
					return errors.New("duplicate or invalid key")
				}
				seen[strings.ToLower(name)] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		default:
			return errors.New("invalid JSON delimiter")
		}
	}
	return walk()
}
