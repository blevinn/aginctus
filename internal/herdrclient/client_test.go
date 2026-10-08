package herdrclient

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/managementnetwork"
)

type applyConfig struct {
	Operation                string            `json:"operation"`
	RejectUnsupportedChanges bool              `json:"rejectUnsupportedChanges"`
	EnsureRunning            bool              `json:"ensureRunning"`
	RequireExistingConfig    map[string]string `json:"requireExistingConfig"`
}

func decodeApplyConfig(t *testing.T, raw []byte) applyConfig {
	t.Helper()
	var cfg applyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode apply config: %v\n%s", err, raw)
	}
	return cfg
}

func testSpec() Spec {
	return Spec{
		Name:              "aginctus-herdr",
		ImageAlias:        "aginctus-herdr-client",
		StoragePool:       "default",
		ManagementNetwork: "aginctus-mgmt",
		Network: managementnetwork.Spec{
			Name:        "aginctus-mgmt",
			IPv4Address: "10.42.0.1/24",
			IPv4NAT:     false,
			IPv4Routing: false,
			IPv6Address: "none",
		},
	}
}

func TestPlanEnsureOrdersNetworkBeforeRunningHerdrClient(t *testing.T) {
	plan, err := testSpec().Plan("upsert", false)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	if plan.Steps[0].ID != "management-network" || plan.Steps[0].Driver != "apply" {
		t.Fatalf("first step = %#v", plan.Steps[0])
	}
	if plan.Steps[1].ID != "herdr-client" || plan.Steps[1].Driver != "apply" {
		t.Fatalf("second step = %#v", plan.Steps[1])
	}

	cfg := decodeApplyConfig(t, plan.Steps[1].Configuration)
	if cfg.Operation != "upsert" || !cfg.EnsureRunning || !cfg.RejectUnsupportedChanges {
		t.Fatalf("Herdr apply config = %#v", cfg)
	}
	for key, want := range map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": "infrastructure",
		"user.aginctus.role":     "herdr-client",
	} {
		if cfg.RequireExistingConfig[key] != want {
			t.Fatalf("ownership guard = %#v", cfg.RequireExistingConfig)
		}
	}

	rendered := string(plan.Steps[1].Configuration)
	for _, want := range []string{
		"aginctus-herdr-client",
		"aginctus-mgmt",
		"default",
		"herdr-client",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("configuration missing %q: %s", want, rendered)
		}
	}
}

func TestPlanTeardownDeletesOnlyHerdrClient(t *testing.T) {
	plan, err := testSpec().Plan("delete", false)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "herdr-client" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	cfg := decodeApplyConfig(t, plan.Steps[0].Configuration)
	if cfg.Operation != "delete" {
		t.Fatalf("operation = %q, want delete", cfg.Operation)
	}
	if cfg.EnsureRunning {
		t.Fatal("ensureRunning = true for delete")
	}
}

func TestPlanForceOmitsHerdrOwnershipGuard(t *testing.T) {
	plan, err := testSpec().Plan("delete", true)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	cfg := decodeApplyConfig(t, plan.Steps[0].Configuration)
	if len(cfg.RequireExistingConfig) != 0 {
		t.Fatalf("force retained guard = %#v", cfg.RequireExistingConfig)
	}
	if !cfg.RejectUnsupportedChanges {
		t.Fatal("force plan disabled immutable-drift rejection")
	}
}
