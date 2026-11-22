package client

import (
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// validateLogicalWorkload checks a domain reader's public projection.
// It is not ownership evidence: the domain reader must authenticate the
// exact Incus project and environment before constructing this document.
func validateLogicalWorkload(workload api.Workload, environmentID string) error {
	if workload.EnvironmentID != environmentID || workload.Name == "" ||
		workload.Implementation == "" || workload.Components == nil {
		return errors.New("logical workload reader returned incomplete resource")
	}
	switch workload.Class {
	case api.WorkloadClassAgent, api.WorkloadClassFrontend, api.WorkloadClassGateway:
	default:
		return errors.New("logical workload reader returned unsupported class")
	}
	seen := make(map[string]bool, len(workload.Components))
	for _, component := range workload.Components {
		if component.Name == "" || seen[component.Name] {
			return errors.New("logical workload reader returned invalid or duplicate component")
		}
		seen[component.Name] = true
	}
	return nil
}

// validateLogicalObservations prevents a status renderer from accepting
// contradictory or unnamed component observations. AcceptedRevision is
// untrusted metadata unless the domain reader independently supplied it
// from an authenticated accepted-state authority.
func validateLogicalObservations(status api.WorkloadStatus) error {
	if status.Observed == nil {
		return errors.New("logical workload reader returned nil observation collection")
	}
	seen := make(map[string]bool, len(status.Observed))
	for _, observation := range status.Observed {
		if observation.Name == "" || observation.State == "" || seen[observation.Name] {
			return errors.New("logical workload reader returned contradictory component observation")
		}
		seen[observation.Name] = true
	}
	return nil
}
