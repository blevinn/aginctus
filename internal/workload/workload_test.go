package workload

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
		ID:                "dev",
		Name:              "aginctus-dev",
		Runtime:           "opencode",
		Isolation:         "container",
		ImageAlias:        "aginctus-opencode-workload",
		StoragePool:       "default",
		ManagementNetwork: "aginctus-mgmt",
		Network: managementnetwork.Spec{
			Name:        "aginctus-mgmt",
			IPv4Address: "10.42.0.1/24",
			IPv6Address: "none",
		},
	}
}

func TestPlanEnsureOrdersNetworkBeforeRunningWorkload(t *testing.T) {
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
	if plan.Steps[1].ID != "workload-dev" || plan.Steps[1].Driver != "apply" {
		t.Fatalf("second step = %#v", plan.Steps[1])
	}

	cfg := decodeApplyConfig(t, plan.Steps[1].Configuration)
	if cfg.Operation != "upsert" || !cfg.EnsureRunning || !cfg.RejectUnsupportedChanges {
		t.Fatalf("workload apply config = %#v", cfg)
	}
	for key, want := range map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": "workload",
		"user.aginctus.workload": "dev",
	} {
		if cfg.RequireExistingConfig[key] != want {
			t.Fatalf("ownership guard = %#v", cfg.RequireExistingConfig)
		}
	}

	rendered := string(plan.Steps[1].Configuration)
	for _, want := range []string{
		"aginctus-opencode-workload",
		"agent-workload",
		"opencode",
		"aginctus-mgmt",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("configuration missing %q: %s", want, rendered)
		}
	}
}

func TestPlanTeardownDeletesOnlyWorkload(t *testing.T) {
	plan, err := testSpec().Plan("delete", false)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].ID != "workload-dev" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	cfg := decodeApplyConfig(t, plan.Steps[0].Configuration)
	if cfg.Operation != "delete" || cfg.EnsureRunning {
		t.Fatalf("delete config = %#v", cfg)
	}
}

func TestPlanForceOmitsWorkloadOwnershipGuard(t *testing.T) {
	plan, err := testSpec().Plan("delete", true)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	cfg := decodeApplyConfig(t, plan.Steps[0].Configuration)
	if len(cfg.RequireExistingConfig) != 0 {
		t.Fatalf("force retained ownership guard = %#v", cfg.RequireExistingConfig)
	}
	if !cfg.RejectUnsupportedChanges {
		t.Fatal("force plan disabled immutable-drift rejection")
	}
}

func TestValidateRejectsUnknownIsolation(t *testing.T) {
	spec := testSpec()
	spec.Isolation = "process"
	if err := spec.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want isolation error")
	}
}
