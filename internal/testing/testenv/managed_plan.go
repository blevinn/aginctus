package testenv

import (
	"errors"

	"github.com/blevinn/aginctus/internal/testing/validation/catalog"
)

// ManagedScenarioPlan describes the requirements for an existing batch
// fixture running INSIDE a disposable testenv guest. It is never admission
// or authority to execute a caller-provided Nix target or command.
type ManagedScenarioPlan struct {
	SchemaVersion int      `json:"schema_version"`
	Scenario      string   `json:"scenario"`
	Target        string   `json:"target"`
	ManagedTarget string   `json:"managed_target,omitempty"`
	Mode          string   `json:"mode"`
	Network       string   `json:"network"`
	ResourceClass string   `json:"resource_class"`
	Requires      []string `json:"requires"`
	Status        string   `json:"status"`
	Blocking      []string `json:"blocking"`
}

// PlanManagedScenario derives policy from the canonical catalog. Existing
// scenarios retain their NixOS test-driver implementation; a future direct
// guest implementation must be explicitly admitted instead of inferred.
func PlanManagedScenario(name string) (ManagedScenarioPlan, error) {
	if _, err := VMScenario(name); err != nil {
		return ManagedScenarioPlan{}, err
	}
	for _, scenario := range catalog.PublicVMScenarios() {
		if scenario.Name != name || scenario.Disabled {
			continue
		}
		plan := ManagedScenarioPlan{
			SchemaVersion: 1,
			Scenario:      name,
			Target:        scenario.Target,
			ManagedTarget: scenario.ManagedTarget,
			Mode:          "nested_batch",
			Network:       "offline",
			ResourceClass: "nested_batch",
			Requires:      []string{"immutable_test_payload", "nested_kvm_guest_execution", "guest_local_nix", "bounded_storage", "outer_network_denial"},
			Status:        "not_admitted",
			Blocking:      []string{"trusted_worker_admission", "managed_execution_transport", "independent_cleanup"},
		}
		if scenario.ManagedTarget != "" {
			plan.Mode = "direct_guest"
			plan.ResourceClass = "native_instance"
			plan.Requires = []string{"pinned_scenario_image", "guest_local_incus", "bounded_storage", "outer_network_denial"}
			plan.Blocking = []string{"trusted_worker_admission", "managed_runtime_acceptance", "independent_cleanup"}
		}
		if scenario.Networked {
			plan.Network = "staging_required"
			plan.Requires = append(plan.Requires, "immutable_oci_staging")
			plan.Blocking = append(plan.Blocking, "approved_staging_policy")
		}
		return plan, nil
	}
	return ManagedScenarioPlan{}, errors.New("managed scenario is unavailable")
}
