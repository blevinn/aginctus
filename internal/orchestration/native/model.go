// Package native defines the future Aginctus-owned Incus reconciliation contract.
// Nothing in this package changes the v0.1 Apply/Compose execution paths.
package native

import (
	"fmt"
	"strings"
)

type Kind string

const (
	Project  Kind = "project"
	Network  Kind = "network"
	Volume   Kind = "volume"
	Instance Kind = "instance"
)

type State string

const (
	Present State = "present"
	Absent  State = "absent"
)

type Resource struct {
	Kind    Kind              `json:"kind"`
	Project string            `json:"project,omitempty"`
	Name    string            `json:"name"`
	State   State             `json:"state,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
}
type ID struct {
	Kind    Kind
	Project string
	Name    string
}

func (r Resource) ID() ID { return ID{Kind: r.Kind, Project: r.Project, Name: r.Name} }
func (r Resource) Validate() error {
	switch r.Kind {
	case Project, Network, Volume, Instance:
	default:
		return fmt.Errorf("unsupported native resource kind %q", r.Kind)
	}
	if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Name) != r.Name {
		return fmt.Errorf("invalid resource name %q", r.Name)
	}
	if r.State != "" && r.State != Present && r.State != Absent {
		return fmt.Errorf("invalid state %q", r.State)
	}
	if r.Kind == Project && r.Project != "" {
		return fmt.Errorf("project resource cannot specify project scope")
	}
	if r.State == Absent && len(r.Config) > 0 {
		return fmt.Errorf("absent resource cannot request config mutations")
	}
	for key := range r.Config {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("config key must not be empty")
		}
	}
	return nil
}

// ValidateSelection checks only expressly selected resources; omitted resources
// are never implicitly deleted or discovered through this contract.
func ValidateSelection(resources []Resource) error {
	seen := map[ID]bool{}
	for i, r := range resources {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("resource %d: %w", i, err)
		}
		if seen[r.ID()] {
			return fmt.Errorf("duplicate resource %v", r.ID())
		}
		seen[r.ID()] = true
	}
	return nil
}
