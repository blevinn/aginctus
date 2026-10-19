package gateway

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func egressTestSpec() Spec {
	return Spec{
		ID: "local", Project: "aginctus-gateway",
		Network: "aginctus-mgmt", EgressNetwork: "aginctus-egress",
		NetworkIPv4Address: "10.42.0.1/24", NetworkIPv6Address: "none",
		LiteLLMImage: "example/litellm:stable", PostgresImage: "example/postgres:stable",
	}
}

func TestEgressGatewayTopologyAndRenderParity(t *testing.T) {
	spec := egressTestSpec()
	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("expected management, egress and compose: %d steps", len(plan.Steps))
	}
	if plan.Steps[0].ID != "management-network" ||
		plan.Steps[1].ID != "gateway-egress-network" ||
		plan.Steps[2].ID != "gateway" {
		t.Fatalf("unexpected deployment order: %#v", plan.Steps)
	}
	egress := string(plan.Steps[1].Configuration)
	for _, marker := range []string{"aginctus-egress", "gateway-egress-network", "user.aginctus.managed", "ipv4.nat", "true", "ipv4.routing", "ipv6.address", "none"} {
		if !strings.Contains(egress, marker) {
			t.Fatalf("egress plan lacks %q: %s", marker, egress)
		}
	}
	management := string(plan.Steps[0].Configuration)
	if !strings.Contains(management, "aginctus-mgmt") || !strings.Contains(management, "ipv4.nat") {
		t.Fatalf("management-network plan missing: %s", management)
	}
	rendered, err := spec.RenderCompose()
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := yaml.YAMLToJSON([]byte(rendered))
	if err != nil {
		t.Fatal(err)
	}
	var renderModel map[string]any
	if err := json.Unmarshal(normalized, &renderModel); err != nil {
		t.Fatal(err)
	}
	var deployment struct {
		Compose map[string]any `json:"compose"`
	}
	if err := json.Unmarshal(plan.Steps[2].Configuration, &deployment); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(renderModel, deployment.Compose) {
		t.Fatalf("render / plan differ: render=%s\ndeploy=%s", normalized, plan.Steps[2].Configuration)
	}
	services := deployment.Compose["services"].(map[string]any)
	postgres := services["postgres"].(map[string]any)
	litellm := services["litellm"].(map[string]any)
	if got := postgres["networks"]; !reflect.DeepEqual(got, []any{"management"}) {
		t.Fatalf("Postgres exposed to egress: %#v", got)
	}
	wantLiteLLMNetworks := map[string]any{
		"management": map[string]any{"x-incus-compose": map[string]any{"internal": true}},
		"egress":     map[string]any{},
	}
	if got := litellm["networks"]; !reflect.DeepEqual(got, wantLiteLLMNetworks) {
		t.Fatalf("LiteLLM management gateway suppression mismatch: %#v", got)
	}
}

func TestEgressNameIsDistinctAndSafe(t *testing.T) {
	for _, name := range []string{"aginctus-mgmt", "../oops", "hello/world", "_wrong", "name with spaces"} {
		spec := egressTestSpec()
		spec.EgressNetwork = name
		if err := spec.Validate(); err == nil {
			t.Fatalf("unsafe egress network accepted: %q", name)
		}
	}
}

func TestEgressStaticIPv4CIDRRenderParity(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressIPv4Address = "10.42.40.5/24"
	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := spec.RenderCompose()
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := yaml.YAMLToJSON([]byte(rendered))
	if err != nil {
		t.Fatal(err)
	}
	var renderModel map[string]any
	if err := json.Unmarshal(normalized, &renderModel); err != nil {
		t.Fatal(err)
	}
	var deployment struct {
		Compose map[string]any `json:"compose"`
	}
	if err := json.Unmarshal(plan.Steps[2].Configuration, &deployment); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(renderModel, deployment.Compose) {
		t.Fatal("render and orchestration differ")
	}
	services := deployment.Compose["services"].(map[string]any)
	litellm := services["litellm"].(map[string]any)
	networks := litellm["networks"].(map[string]any)
	if got := networks["egress"].(map[string]any)["ipv4_address"]; got != "10.42.40.5/24" {
		t.Fatalf("unexpected CIDR: %v", got)
	}
}

func TestEgressIPv4RequiresValidHostCIDR(t *testing.T) {
	for _, addr := range []string{"bogus", "10.42.40.1", "10.42.40.0/24", "2001:db8::2/64", "10.42.40.2/32"} {
		spec := egressTestSpec()
		spec.EgressIPv4Address = addr
		if err := spec.Validate(); err == nil {
			t.Fatalf("accepted invalid CIDR %q", addr)
		}
	}
}

func TestExplicitEgressBridgeCIDRInPlan(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	spec.EgressIPv4Address = "10.42.40.25/24"
	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	var networkStep struct {
		Documents []struct {
			Config map[string]string `json:"config"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(plan.Steps[1].Configuration, &networkStep); err != nil {
		t.Fatal(err)
	}
	if len(networkStep.Documents) != 1 || networkStep.Documents[0].Config["ipv4.address"] != "10.42.40.1/24" {
		t.Fatalf("unexpected egress bridge IPv4 address in network plan")
	}
	var composeStep struct {
		Compose struct {
			Networks map[string]struct {
				External bool              `json:"external"`
				Name     string            `json:"name"`
				XIncus   map[string]string `json:"x-incus"`
			} `json:"networks"`
		} `json:"compose"`
	}
	if err := json.Unmarshal(plan.Steps[2].Configuration, &composeStep); err != nil {
		t.Fatal(err)
	}
	egressNetwork := composeStep.Compose.Networks["egress"]
	if !egressNetwork.External || egressNetwork.Name != spec.EgressNetwork ||
		egressNetwork.XIncus["ipv4.address"] != spec.EgressBridgeIPv4CIDR {
		t.Fatalf("external egress Compose network missing explicit bridge CIDR: %+v", egressNetwork)
	}
	for _, tc := range []struct {
		bridge string
		host   string
	}{
		{"not-a-prefix", ""},
		{"2001:db8::1/64", ""},
		{"10.42.40.0/24", ""},
		{"10.42.40.1/31", ""},
		{"10.42.40.1/24", "10.42.41.5/24"},
		{"10.42.40.1/24", "10.42.40.1/24"},
		{"10.42.40.1/24", "10.42.40.5/25"},
	} {
		s := egressTestSpec()
		s.EgressBridgeIPv4CIDR = tc.bridge
		s.EgressIPv4Address = tc.host
		if err := s.Validate(); err == nil {
			t.Fatalf("accepted bridge %q and host %q", tc.bridge, tc.host)
		}
	}
}

func TestEgressDHCPDisabledRequiresExplicitAddressing(t *testing.T) {
	for _, tc := range []struct {
		name, bridge, host string
		disabled           bool
		valid              bool
	}{
		{"default", "", "", false, true},
		{"missing both", "", "", true, false},
		{"missing host", "10.42.40.1/24", "", true, false},
		{"missing bridge", "", "10.42.40.25/24", true, false},
		{"complete", "10.42.40.1/24", "10.42.40.25/24", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := egressTestSpec()
			spec.EgressBridgeIPv4CIDR = tc.bridge
			spec.EgressIPv4Address = tc.host
			spec.EgressDHCPDisabled = tc.disabled
			if err := spec.Validate(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, validation error: %v", tc.valid, err)
			}
		})
	}
	spec := egressTestSpec()
	spec.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	spec.EgressIPv4Address = "10.42.40.25/24"
	spec.EgressDHCPDisabled = true
	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Documents []struct {
			Config map[string]string `json:"config"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(plan.Steps[1].Configuration, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Documents) != 1 || data.Documents[0].Config["ipv4.dhcp"] != "false" {
		t.Fatalf("expected disabled DHCP network configuration: %s", plan.Steps[1].Configuration)
	}
	spec.EgressDHCPDisabled = false
	plan, err = spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	data.Documents = nil // Avoid retaining entries from the prior JSON map.
	if err := json.Unmarshal(plan.Steps[1].Configuration, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Documents) != 1 {
		t.Fatalf("unexpected network plan documents: %d", len(data.Documents))
	}
	if _, set := data.Documents[0].Config["ipv4.dhcp"]; set {
		t.Fatalf("default should leave DHCP policy absent")
	}
}

func TestEgressDHCPRangesRenderedAndValidated(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressBridgeIPv4CIDR = "10.42.40.1/24"
	spec.EgressIPv4Address = "10.42.40.25/24"
	spec.EgressDHCPRanges = "10.42.40.100-10.42.40.199"
	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Documents []struct {
			Config map[string]string `json:"config"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(plan.Steps[1].Configuration, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Documents[0].Config["ipv4.dhcp.ranges"]; got != spec.EgressDHCPRanges {
		t.Fatalf("DHCP ranges absent from plan: %q", got)
	}
	for _, tc := range []struct {
		name, ranges string
		disabled     bool
	}{
		{"overlapping NIC", "10.42.40.20-10.42.40.30", false},
		{"overlapping gateway", "10.42.40.1-10.42.40.10", false},
		{"malformed", "nope", false},
		{"disabled", "10.42.40.100-10.42.40.199", true},
	} {
		s := spec
		s.EgressDHCPRanges = tc.ranges
		s.EgressDHCPDisabled = tc.disabled
		if err := s.Validate(); err == nil {
			t.Errorf("%s unexpectedly valid", tc.name)
		}
	}
}
