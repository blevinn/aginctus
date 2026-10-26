package managementnetwork

import (
	"encoding/json"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

type applyStepConfig struct {
	Operation                string                       `json:"operation"`
	RejectUnsupportedChanges bool                         `json:"rejectUnsupportedChanges"`
	RequireExistingConfig    map[string]string            `json:"requireExistingConfig"`
	Documents                []map[string]json.RawMessage `json:"documents"`
}

func TestConfiguredDHCPGatewayPolicy(t *testing.T) {
	for _, gateway := range []string{"none", "10.88.0.1"} {
		t.Run(gateway, func(t *testing.T) {
			loader := &config.Loader{}
			effective, err := loader.Load(config.Options{Overrides: []string{
				"incus.management.network.name=custom-mgmt",
				"incus.management.network.ipv4.address=10.88.0.1/24",
				"incus.management.network.ipv4.dhcp.gateway=" + gateway,
			}})
			if err != nil {
				t.Fatal(err)
			}
			spec, err := FromConfig(effective)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := spec.Plan("upsert", false)
			if err != nil {
				t.Fatal(err)
			}
			step := decodeApplyStepConfig(t, plan.Steps[0].Configuration)
			var settings map[string]string
			if err := json.Unmarshal(step.Documents[0]["config"], &settings); err != nil {
				t.Fatal(err)
			}
			if settings["ipv4.dhcp.gateway"] != gateway || settings["ipv4.address"] != "10.88.0.1/24" {
				t.Fatalf("management settings = %#v", settings)
			}
		})
	}
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
	if !cfg.RejectUnsupportedChanges {
		t.Fatal("rejectUnsupportedChanges = false, want true")
	}
	if cfg.RequireExistingConfig["user.aginctus.managed"] != "true" {
		t.Fatalf("managed guard = %#v", cfg.RequireExistingConfig)
	}
	if cfg.RequireExistingConfig["user.aginctus.resource"] != "management-network" {
		t.Fatalf("resource guard = %#v", cfg.RequireExistingConfig)
	}
	if cfg.RequireExistingConfig["ipv4.dhcp.gateway"] != "none" {
		t.Fatalf("DHCP policy guard = %#v", cfg.RequireExistingConfig)
	}
	var settings map[string]string
	if err := json.Unmarshal(cfg.Documents[0]["config"], &settings); err != nil {
		t.Fatal(err)
	}
	if settings["ipv4.dhcp.gateway"] != "none" {
		t.Fatalf("default DHCP gateway = %q", settings["ipv4.dhcp.gateway"])
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
	if !cfg.RejectUnsupportedChanges {
		t.Fatal("force plan disabled immutable-drift rejection")
	}
}

func TestDeleteRetainsOwnershipWithoutDHCPMigration(t *testing.T) {
	plan, err := (Spec{Name: "aginctus-mgmt"}).Plan("delete", false)
	if err != nil {
		t.Fatal(err)
	}
	guards := decodeApplyStepConfig(t, plan.Steps[0].Configuration).RequireExistingConfig
	if len(guards) != 2 || guards["user.aginctus.managed"] != "true" || guards["user.aginctus.resource"] != "management-network" {
		t.Fatalf("delete ownership guards = %#v", guards)
	}
}
