package acceptedstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// DeploymentSpec is a deliberately workload-only accepted-intent fragment.
// It cannot carry host policy, arbitrary Incus config, runtime credentials,
// user-controlled source overlays or permission/ownership grants. Future
// resource domains require their own reviewed schema version.
type DeploymentSpec struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Selection  SelectionManifest `json:"selection"`
	Workloads  []WorkloadSpec    `json:"workloads"`
}

const (
	DeploymentAPIVersion = "aginctus.dev/accepted/v1alpha1"
	DeploymentKind       = "WorkloadDeployment"
)

// WorkloadSpec is an immutable, typed, nonsecret declaration. Image references
// must already be resolved to immutable SHA256 fingerprints before acceptance.
// Profiles are pinned by name, not evidence that a profile is safe or owned.
// This type is NOT a native reconcile write authorization.
type WorkloadSpec struct {
	ID               string   `json:"id"`
	InstanceName     string   `json:"instanceName"`
	InstanceType     string   `json:"instanceType"`
	ImageFingerprint string   `json:"imageFingerprint"`
	Profiles         []string `json:"profiles"`
	EnsureRunning    bool     `json:"ensureRunning"`
}

func (s DeploymentSpec) Validate() error {
	if s.APIVersion != DeploymentAPIVersion || s.Kind != DeploymentKind {
		return errors.New("unsupported deployment spec version or kind")
	}
	if err := s.Selection.Validate(); err != nil {
		return fmt.Errorf("invalid selection: %w", err)
	}
	if s.Workloads == nil || len(s.Workloads) != len(s.Selection.SelectedWorkloads) {
		return errors.New("workloads must exactly match the explicit selection")
	}
	selected := make(map[string]bool, len(s.Selection.SelectedWorkloads))
	for _, id := range s.Selection.SelectedWorkloads {
		selected[id] = true
	}
	seen := make(map[string]bool, len(s.Workloads))
	for _, item := range s.Workloads {
		if !selected[item.ID] || seen[item.ID] {
			return errors.New("unexpected or duplicate workload declaration")
		}
		seen[item.ID] = true
		if !localID.MatchString(item.InstanceName) {
			return errors.New("invalid workload instance name")
		}
		if item.InstanceType != "container" && item.InstanceType != "virtual-machine" {
			return errors.New("unsupported workload instance type")
		}
		if len(item.ImageFingerprint) != 64 || !fingerprintHex(item.ImageFingerprint) {
			return errors.New("workload image must be pinned by lowercase SHA256 fingerprint")
		}
		if item.Profiles == nil || len(item.Profiles) > 32 {
			return errors.New("workload profiles must be explicit and bounded")
		}
		profiles := map[string]bool{}
		for _, name := range item.Profiles {
			if !localID.MatchString(name) || profiles[name] {
				return errors.New("invalid or duplicate profile")
			}
			profiles[name] = true
		}
	}
	return nil
}

func fingerprintHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// Canonical returns stable, independent bytes. Workloads are treated as a set,
// while the order of profiles is retained because Incus profile order matters.
func (s DeploymentSpec) Canonical() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	s.Selection.SelectedWorkloads = slices.Clone(s.Selection.SelectedWorkloads)
	slices.Sort(s.Selection.SelectedWorkloads)
	s.Workloads = slices.Clone(s.Workloads)
	slices.SortFunc(s.Workloads, func(a, b WorkloadSpec) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return json.Marshal(s)
}

func (s DeploymentSpec) Digest() (string, error) {
	data, err := s.Canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ParseDeploymentSpec rejects duplicate JSON keys, case aliases, unrecognized
// fields, truncated or trailing documents and unsupported schema versions.
// The input is not a generic JSON configuration to be merged across principals.
func ParseDeploymentSpec(data []byte) (DeploymentSpec, error) {
	if len(data) == 0 || len(data) > maxSelectionBytes {
		return DeploymentSpec{}, errors.New("invalid deployment spec size")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return DeploymentSpec{}, errors.New("invalid or duplicate deployment JSON keys")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return DeploymentSpec{}, errors.New("deployment spec must be a JSON object")
	}
	if err := strictKeySet(root, "apiVersion", "kind", "selection", "workloads"); err != nil {
		return DeploymentSpec{}, err
	}
	if _, err := ParseSelection(root["selection"]); err != nil {
		return DeploymentSpec{}, errors.New("invalid selection in deployment spec")
	}
	var workloadRaw []json.RawMessage
	if err := json.Unmarshal(root["workloads"], &workloadRaw); err != nil || workloadRaw == nil {
		return DeploymentSpec{}, errors.New("workloads must be an explicit array")
	}
	for _, raw := range workloadRaw {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return DeploymentSpec{}, errors.New("workload entry must be an object")
		}
		if err := strictKeySet(fields, "id", "instanceName", "instanceType", "imageFingerprint", "profiles", "ensureRunning"); err != nil {
			return DeploymentSpec{}, err
		}
	}
	var spec DeploymentSpec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return DeploymentSpec{}, errors.New("invalid deployment spec JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return DeploymentSpec{}, errors.New("unexpected trailing deployment JSON")
	}
	if err := spec.Validate(); err != nil {
		return DeploymentSpec{}, err
	}
	return spec, nil
}

func strictKeySet(fields map[string]json.RawMessage, allowed ...string) error {
	names := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		names[name] = true
	}
	for name := range fields {
		if !names[name] {
			return errors.New("unknown or incorrectly cased deployment field")
		}
	}
	return nil
}
