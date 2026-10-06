package managementnetwork

import (
	"encoding/json"
	"testing"
)

type applyStepConfig struct {
	Operation             string                       `json:"operation"`
	RequireExistingConfig map[string]string            `json:"requireExistingConfig"`
	Documents             []map[string]json.RawMessage `json:"documents"`
}

func decodeApplyStepConfig(t *testing.T, raw []byte) applyStepConfig {
	t.Helper()
	var cfg applyStepConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode apply step configuration: %v\n%s", err, raw)
	}
	return cfg
}

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

	cfg := decodeApplyStepConfig(t, plan.Steps[0].Configuration)
	if cfg.Operation != "upsert" {
		t.Fatalf("operation = %q, want upsert", cfg.Operation)
	}
	if cfg.RequireExistingConfig["user.aginctus.managed"] != "true" {
		t.Fatalf("managed guard = %#v", cfg.RequireExistingConfig)
	}
	if cfg.RequireExistingConfig["user.aginctus.resource"] != "management-network" {
		t.Fatalf("resource guard = %#v", cfg.RequireExistingConfig)
	}
}

func TestPlanForceOmitsExistingResourceGuard(t *testing.T) {
	spec := Spec{Name: "aginctus-mgmt", IPv6Address: "none"}
	plan, err := spec.Plan("delete", true)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	cfg := decodeApplyStepConfig(t, plan.Steps[0].Configuration)
	if cfg.Operation != "delete" {
		t.Fatalf("operation = %q, want delete", cfg.Operation)
	}
	if len(cfg.RequireExistingConfig) != 0 {
		t.Fatalf("force plan retained ownership guard: %#v", cfg.RequireExistingConfig)
	}
}
