package orchestration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

const (
	NativeAPIVersion = "aginctus.dev/native-plan/v1alpha1"
	NativePlanKind   = "NativeResourcePlan"
	maxNativePlan    = 1 << 20
	maxResources     = 256
)

type NativeResourceKind string

const (
	NativeWorkload          NativeResourceKind = "workload"
	NativeManagementNetwork NativeResourceKind = "management-network"
	NativeHerdrVolume       NativeResourceKind = "herdr-volume"
	NativeHerdrInstance     NativeResourceKind = "herdr-instance"
	NativeGatewayImage      NativeResourceKind = "gateway-image"
	NativeGatewayVolume     NativeResourceKind = "gateway-volume"
	NativeGatewayInstance   NativeResourceKind = "gateway-instance"
	NativeGatewayRuntime    NativeResourceKind = "gateway-runtime"
)

// NativeResource selects one typed operation target. ID is a logical selection
// key in the domain, not a caller-supplied Incus name. Dependencies cannot
// select additional resources or confer authority over them.
type NativeResource struct {
	ID        string             `json:"id"`
	Kind      NativeResourceKind `json:"kind"`
	DependsOn []string           `json:"dependsOn,omitempty"`
}

// NativePlan selects resources from independently verified intent. It does not
// contain config, credentials, resource ownership, pins or an authority grant.
type NativePlan struct {
	APIVersion string                       `json:"apiVersion"`
	Kind       string                       `json:"kind"`
	Revision   environment.AcceptedRevision `json:"revision"`
	Resources  []NativeResource             `json:"resources"`
}

// ParseNativePlan rejects ambiguous JSON before decoding the typed selection.
// It shares revision identity with accepted state; it does not publish state.
func ParseNativePlan(data []byte) (NativePlan, error) {
	var p NativePlan
	if len(data) > maxNativePlan || !utf8.Valid(data) {
		return p, errors.New("invalid native plan size or encoding")
	}
	// The bounded, closed schema contains no arbitrary maps. Require canonical
	// key spelling and reject duplicate keys (encoding/json v1 alone allows both).
	if err := nativePlanKeys(data); err != nil {
		return p, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return NativePlan{}, errors.New("invalid typed native plan JSON")
	}
	if err := p.Validate(); err != nil {
		return NativePlan{}, err
	}
	return p, nil
}

func nativePlanKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 5 {
			return false
		}
		t, err := d.Token()
		if err != nil || t == nil {
			return false
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				switch name {
				case "apiVersion", "kind", "revision", "resources", "id", "dependsOn",
					"environmentId", "serverId", "project", "generation", "parentId", "candidateDigest":
				default:
					return false
				}
				seen[name] = true
				if !walk(depth + 1) {
					return false
				}
			}
			if depth == 1 && !seen["parentId"] {
				return false // The revision envelope requires an explicit parentId (empty for genesis).
			}
		case json.Delim('['):
			for d.More() {
				if !walk(depth + 1) {
					return false
				}
			}
		default:
			return true
		}
		_, err = d.Token()
		return err == nil
	}
	if walk(0) {
		if _, err := d.Token(); err == io.EOF {
			return nil
		}
	}
	return errors.New("invalid or ambiguous native plan JSON")
}

func (p NativePlan) Validate() error {
	_, err := p.ordered()
	return err
}

func (p NativePlan) ordered() ([]NativeResource, error) {
	if p.APIVersion != NativeAPIVersion || p.Kind != NativePlanKind {
		return nil, errors.New("unsupported native plan version or kind")
	}
	if err := p.Revision.ValidateIdentity(); err != nil {
		return nil, err
	}
	if len(p.Resources) == 0 || len(p.Resources) > maxResources {
		return nil, errors.New("native plan requires a bounded nonempty resource selection")
	}
	resources := slices.Clone(p.Resources)
	known := make(map[string]bool, len(resources))
	for i := range resources {
		r := &resources[i]
		if !scopeIdentifier.MatchString(r.ID) || known[r.ID] {
			return nil, errors.New("invalid or duplicate native resource ID")
		}
		known[r.ID] = true
		switch r.Kind {
		case NativeWorkload, NativeManagementNetwork, NativeHerdrVolume, NativeHerdrInstance, NativeGatewayImage, NativeGatewayVolume, NativeGatewayInstance, NativeGatewayRuntime:
		default:
			return nil, fmt.Errorf("resource %q has unsupported native kind", r.ID)
		}
		if len(r.DependsOn) >= len(resources) {
			return nil, fmt.Errorf("resource %q has too many dependencies", r.ID)
		}
		r.DependsOn = slices.Clone(r.DependsOn)
		slices.Sort(r.DependsOn)
	}
	for _, r := range resources {
		for i, dependency := range r.DependsOn {
			if dependency == r.ID || !known[dependency] || i > 0 && r.DependsOn[i-1] == dependency {
				return nil, fmt.Errorf("resource %q has invalid, unselected or duplicate dependency", r.ID)
			}
		}
	}
	slices.SortFunc(resources, func(a, b NativeResource) int { return strings.Compare(a.ID, b.ID) })
	ordered := make([]NativeResource, 0, len(resources))
	done := make(map[string]bool, len(resources))
	for len(ordered) < len(resources) {
		before := len(ordered)
		for _, r := range resources {
			if done[r.ID] || slices.ContainsFunc(r.DependsOn, func(dep string) bool { return !done[dep] }) {
				continue
			}
			ordered = append(ordered, r)
			done[r.ID] = true
			break
		}
		if len(ordered) == before {
			return nil, errors.New("native resource dependency cycle")
		}
	}
	return ordered, nil
}
