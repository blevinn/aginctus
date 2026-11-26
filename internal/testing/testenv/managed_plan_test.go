package testenv

import (
	"slices"
	"testing"
)

func TestManagedPlanCatalog(t *testing.T) {
	for _, scenario := range VMScenarios() {
		p, err := PlanManagedScenario(scenario.Name)
		if err != nil {
			t.Fatal(err)
		}
		if p.Scenario != scenario.Name || p.Target != scenario.Target || p.Status != "not_admitted" ||
			!slices.Contains(p.Blocking, "independent_cleanup") {
			t.Fatalf("bad plan: %+v", p)
		}
		if scenario.Name == "native-instance" {
			if p.ManagedTarget != "testenv-managed-native-instance" || !slices.Contains(p.Blocking, "managed_runtime_acceptance") || p.Mode != "direct_guest" || !slices.Contains(p.Requires, "pinned_scenario_image") || slices.Contains(p.Requires, "nested_kvm_guest_execution") {
				t.Fatalf("direct guest plan: %+v", p)
			}
		} else if p.ManagedTarget != "" || p.Mode != "nested_batch" || !slices.Contains(p.Requires, "nested_kvm_guest_execution") {
			t.Fatalf("nested plan: %+v", p)
		}
	}
	for _, name := range []string{"", "full-stack", "native-oci", "../smoke", "smoke;id"} {
		if _, err := PlanManagedScenario(name); err == nil {
			t.Errorf("unexpected plan for %q", name)
		}
	}
}

func TestManagedPlanNetworkPolicy(t *testing.T) {
	offline, err := PlanManagedScenario("native-instance")
	if err != nil || offline.Network != "offline" || slices.Contains(offline.Requires, "immutable_oci_staging") {
		t.Fatalf("offline plan: %+v %v", offline, err)
	}
	staged, err := PlanManagedScenario("gateway-topology")
	if err != nil || staged.Network != "staging_required" || !slices.Contains(staged.Blocking, "approved_staging_policy") {
		t.Fatalf("staged plan: %+v %v", staged, err)
	}
	offline.Requires[0] = "changed"
	again, err := PlanManagedScenario("native-instance")
	if err != nil || again.Requires[0] == "changed" {
		t.Fatal("shared mutable plan")
	}
}
