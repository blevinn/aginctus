package environment

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

const (
	CandidateAPIVersion = "aginctus.dev/environment/v1alpha1"
	CandidateKind       = "EnvironmentCandidate"
	maxCandidateBytes   = 1 << 20
)

// Candidate is a source-neutral, nonsecret declaration. Environment identifies
// the intended lifecycle; the server binding is a declaration until independently
// verified against an authenticated Incus connection. No field grants authority.
type Candidate struct {
	APIVersion        string                       `json:"apiVersion"`
	Kind              string                       `json:"kind"`
	Environment       Descriptor                   `json:"environment"`
	SelectedWorkloads []string                     `json:"selectedWorkloads"`
	Workloads         []acceptedstate.WorkloadSpec `json:"workloads"`
	Gateway           *GatewaySpec                 `json:"gateway,omitempty"`
	Herdr             *HerdrSpec                   `json:"herdr,omitempty"`
}

func (c Candidate) Validate() error {
	if c.APIVersion != CandidateAPIVersion || c.Kind != CandidateKind {
		return errors.New("unsupported environment candidate version or kind")
	}
	if err := c.Environment.Validate(); err != nil {
		return fmt.Errorf("invalid environment identity: %w", err)
	}
	// The existing workload-only schema enforces immutable fingerprints, exact
	// selection, unique instances, profile order and no privileged fields.
	// This is validation reuse, NOT translation into an accepted revision.
	spec := acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind:       acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion:        acceptedstate.SelectionAPIVersion,
			Kind:              acceptedstate.SelectionKind,
			DeploymentID:      c.Environment.ID,
			IncusProject:      c.Environment.Binding.Project,
			SelectedWorkloads: c.SelectedWorkloads,
		},
		Workloads: c.Workloads,
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	if c.Gateway != nil {
		if err := c.Gateway.validate(c.Environment.Binding); err != nil {
			return err
		}
	}
	if c.Herdr != nil {
		if err := c.Herdr.validate(c.Environment); err != nil {
			return err
		}
		for _, w := range c.Workloads {
			if w.InstanceName == c.Herdr.InstanceName {
				return errors.New("accepted Herdr instance overlaps a workload")
			}
		}
	}
	return nil
}

// Canonical returns deterministic candidate bytes. These are not accepted
// revision bytes, a cryptographic authorization token or an Incus resource ID.
func (c Candidate) Canonical() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	c.SelectedWorkloads = slices.Clone(c.SelectedWorkloads)
	slices.Sort(c.SelectedWorkloads)
	c.Workloads = slices.Clone(c.Workloads)
	slices.SortFunc(c.Workloads, func(a, b acceptedstate.WorkloadSpec) int {
		return strings.Compare(a.ID, b.ID)
	})
	return json.Marshal(c)
}

// ParseCandidate is deliberately strict: no duplicate keys, case aliases,
// unknown/privileged keys, trailing documents or implicit source precedence.
// Never pass a generic config-loader's recursively merged map to this parser.
func ParseCandidate(data []byte) (Candidate, error) {
	if len(data) == 0 || len(data) > maxCandidateBytes {
		return Candidate{}, errors.New("invalid environment candidate size")
	}
	if err := rejectCandidateDuplicateKeys(data); err != nil {
		return Candidate{}, errors.New("invalid or duplicate environment candidate JSON keys")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil || keys == nil {
		return Candidate{}, errors.New("environment candidate must be a JSON object")
	}
	rootKeys := []string{"apiVersion", "kind", "environment", "selectedWorkloads", "workloads"}
	if raw, present := keys["gateway"]; present {
		rootKeys = append(rootKeys, "gateway")
		if err := gatewayCandidateKeys(raw); err != nil {
			return Candidate{}, err
		}
	}
	if raw, present := keys["herdr"]; present {
		rootKeys = append(rootKeys, "herdr")
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || candidateKeys(fields, "instanceName", "imageFingerprint", "storagePool", "managementNetwork", "allowStateBootstrap") != nil {
			return Candidate{}, errors.New("invalid Herdr candidate fields")
		}
	}
	if err := candidateKeys(keys, rootKeys...); err != nil {
		return Candidate{}, err
	}
	var descriptor map[string]json.RawMessage
	if err := json.Unmarshal(keys["environment"], &descriptor); err != nil || descriptor == nil {
		return Candidate{}, errors.New("environment descriptor must be an object")
	}
	if err := candidateKeys(descriptor, "id", "name", "binding"); err != nil {
		return Candidate{}, err
	}
	var binding map[string]json.RawMessage
	if err := json.Unmarshal(descriptor["binding"], &binding); err != nil || binding == nil {
		return Candidate{}, errors.New("incus binding must be an object")
	}
	if err := candidateKeys(binding, "serverId", "project"); err != nil {
		return Candidate{}, err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(keys["workloads"], &entries); err != nil || entries == nil {
		return Candidate{}, errors.New("workloads must be an explicit array")
	}
	for _, entry := range entries {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry, &fields); err != nil || fields == nil {
			return Candidate{}, errors.New("workload entry must be an object")
		}
		if err := acceptedstate.ValidateWorkloadFields(fields); err != nil {
			return Candidate{}, err
		}
	}
	var c Candidate
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Candidate{}, errors.New("invalid environment candidate JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Candidate{}, errors.New("trailing environment candidate content")
	}
	if err := c.Validate(); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func candidateKeys(fields map[string]json.RawMessage, allowed ...string) error {
	if len(fields) != len(allowed) {
		return errors.New("missing or unknown environment candidate field")
	}
	for _, name := range allowed {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return errors.New("missing or null environment candidate field")
		}
	}
	return nil
}

func rejectCandidateDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[strings.ToLower(name)] {
					return errors.New("duplicate JSON key")
				}
				seen[strings.ToLower(name)] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = dec.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	_, err := dec.Token()
	if err != io.EOF {
		return errors.New("trailing JSON content")
	}
	return nil
}

// ReadCandidateFile reads exactly the caller-selected file, never /etc, XDG,
// AGINCTUS_* variables or other candidates. Path selection is not authorization.
// It is intentionally separate from config.Loader.Load's legacy preference merge.
func ReadCandidateFile(path string) (Candidate, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Candidate{}, errors.New("explicit absolute candidate path required")
	}
	// Opening a FIFO in ordinary blocking mode can hang before Stat can
	// reject it. Check type and size on the SAME opened descriptor used to
	// read the candidate; a Nix-store symlink to a regular file is allowed.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Candidate{}, fmt.Errorf("open selected candidate: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCandidateBytes {
		return Candidate{}, errors.New("candidate must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCandidateBytes+1))
	if err != nil {
		return Candidate{}, errors.New("cannot read candidate")
	}
	return ParseCandidate(data)
}

// SelectCandidate ensures an explicit candidate cannot silently redirect to
// another enrolled project/environment. Observed is supplied by a separately
// authenticated Incus connection; this method does not authenticate it.
func SelectCandidate(c Candidate, enrolled Descriptor, observed Binding) (Candidate, error) {
	if err := c.Validate(); err != nil {
		return Candidate{}, err
	}
	if err := enrolled.Validate(); err != nil {
		return Candidate{}, err
	}
	if !c.Environment.SameEnvironment(enrolled) {
		return Candidate{}, errors.New("candidate targets another enrolled environment")
	}
	if err := enrolled.VerifyBinding(observed); err != nil {
		return Candidate{}, err
	}
	return c, nil
}
