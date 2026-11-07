package managementnetwork

import (
	"encoding/json"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	nativedriver "github.com/blevinn/aginctus/internal/orchestration/drivers/native"
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
		Backend:     BackendLegacy,
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
	spec := Spec{Name: "aginctus-mgmt", IPv6Address: "none", Backend: BackendLegacy}
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
	plan, err := (Spec{Name: "aginctus-mgmt", Backend: BackendLegacy}).Plan("delete", false)
	if err != nil {
		t.Fatal(err)
	}
	guards := decodeApplyStepConfig(t, plan.Steps[0].Configuration).RequireExistingConfig
	if len(guards) != 2 || guards["user.aginctus.managed"] != "true" || guards["user.aginctus.resource"] != "management-network" {
		t.Fatalf("delete ownership guards = %#v", guards)
	}
}

func TestConfiguredBackendSelection(t *testing.T) {
	loader := &config.Loader{}
	effective, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := FromConfig(effective)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Backend != BackendLegacy {
		t.Fatalf("default backend = %q, want %q", spec.Backend, BackendLegacy)
	}

	effective, err = loader.Load(config.Options{Overrides: []string{
		"incus.management.network.backend=native",
	}})
	if err != nil {
		t.Fatal(err)
	}
	spec, err = FromConfig(effective)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Backend != BackendNative {
		t.Fatalf("selected backend = %q, want %q", spec.Backend, BackendNative)
	}

	effective, err = loader.Load(config.Options{Overrides: []string{
		"incus.management.network.backend=compose",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromConfig(effective); err == nil {
		t.Fatal("unsupported backend was accepted")
	}
}

func TestNativeBackendSelectsTheNativeDriver(t *testing.T) {
	spec := Spec{
		Name: "aginctus-mgmt", IPv4Address: "10.42.0.1/24", IPv4NAT: false,
		IPv4Routing: false, IPv6Address: "none", Backend: BackendNative,
	}
	plan, err := spec.Plan("upsert", false)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Driver != nativedriver.DriverName {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	var cfg nativedriver.Config
	if err := json.Unmarshal(plan.Steps[0].Configuration, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Operation != "upsert" || cfg.Resource != "management-network" ||
		cfg.Name != "aginctus-mgmt" || cfg.Type != "bridge" || cfg.Adopt {
		t.Fatalf("native step configuration = %#v", cfg)
	}
	want := map[string]string{
		"ipv4.address":      "10.42.0.1/24",
		"ipv4.dhcp.gateway": "none",
		"ipv4.nat":          "false",
		"ipv4.routing":      "false",
		"ipv6.address":      "none",
	}
	for key, value := range want {
		if cfg.Config[key] != value {
			t.Fatalf("managed key %q = %q, want %q", key, cfg.Config[key], value)
		}
	}
	if len(cfg.Config) != len(want) {
		t.Fatalf("native step manages unexpected keys: %#v", cfg.Config)
	}
}

func TestNativeBackendCarriesExplicitAdoption(t *testing.T) {
	spec := Spec{
		Name: "aginctus-mgmt", IPv4Address: "auto", IPv6Address: "none",
		Backend: BackendNative,
	}
	plan, err := spec.Plan("delete", true)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	var cfg nativedriver.Config
	if err := json.Unmarshal(plan.Steps[0].Configuration, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Operation != "delete" || !cfg.Adopt {
		t.Fatalf("native step configuration = %#v", cfg)
	}
}

func TestPlanRejectsUnknownBackend(t *testing.T) {
	if _, err := (Spec{Name: "aginctus-mgmt", Backend: Backend("compose")}).Plan("upsert", false); err == nil {
		t.Fatal("plan accepted an unknown backend")
	}
}
