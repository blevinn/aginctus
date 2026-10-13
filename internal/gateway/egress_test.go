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
	if got := litellm["networks"]; !reflect.DeepEqual(got, []any{"management", "egress"}) {
		t.Fatalf("LiteLLM outbound route mismatch: %#v", got)
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
