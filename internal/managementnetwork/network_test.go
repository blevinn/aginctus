package managementnetwork

import (
	"strings"
	"testing"
)

func TestPlanRequiresOwnershipByDefault(t *testing.T) {
	spec := Spec{
		Name:        "aginctus-mgmt",
		IPv4Address: "10.42.0.1/24",
		IPv4NAT:     false,
		IPv4Routing: false,
		IPv6Address: "none",
	}
	plan, err := spec.Plan("upsert", false)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Driver != "apply" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	cfg := string(plan.Steps[0].Configuration)
	for _, want := range []string{
		`"operation":"upsert"`,
		"user.aginctus.managed",
		"user.aginctus.resource",
		"management-network",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("configuration missing %q: %s", want, cfg)
		}
	}
}

func TestPlanForceOmitsExistingResourceGuard(t *testing.T) {
	spec := Spec{Name: "aginctus-mgmt", IPv6Address: "none"}
	plan, err := spec.Plan("delete", true)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	cfg := string(plan.Steps[0].Configuration)
	if !strings.Contains(cfg, `"operation":"delete"`) {
		t.Fatalf("configuration = %s", cfg)
	}
	if strings.Contains(cfg, "requireExistingConfig") && !strings.Contains(cfg, `"requireExistingConfig":{}`) {
		t.Fatalf("force plan retained ownership guard: %s", cfg)
	}
}
