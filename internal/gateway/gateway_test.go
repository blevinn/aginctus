package gateway

import (
	"strings"
	"testing"
)

func TestRenderCompose(t *testing.T) {
	spec := Spec{
		ID:            "local",
		Project:       "aginctus-gateway",
		Network:       "aginctus-mgmt",
		LiteLLMImage:  "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage: "docker.io/library/postgres:17-alpine",
	}

	rendered, err := spec.RenderCompose()
	if err != nil {
		t.Fatalf("RenderCompose() error = %v", err)
	}

	for _, want := range []string{
		"name: aginctus-gateway",
		"ghcr.io/berriai/litellm:v1.103.0-stable",
		"docker.io/library/postgres:17-alpine",
		"name: aginctus-mgmt",
		"${AGINCTUS_GATEWAY_MASTER_KEY}",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered compose missing %q:\n%s", want, rendered)
		}
	}
}

func TestValidateRejectsEmptyImage(t *testing.T) {
	spec := Spec{ID: "local", Project: "gateway", Network: "mgmt", PostgresImage: "postgres"}
	if err := spec.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}


func TestOrchestrationPlanUsesComposeDriverAndSecretReferences(t *testing.T) {
	spec := Spec{
		ID:            "local",
		Project:       "aginctus-gateway",
		Network:       "aginctus-mgmt",
		LiteLLMImage:  "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage: "docker.io/library/postgres:17-alpine",
	}

	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatalf("OrchestrationPlan() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Driver != "compose" {
		t.Fatalf("plan steps = %#v", plan.Steps)
	}
	configuration := string(plan.Steps[0].Configuration)
	for _, want := range []string{
		`"project":"aginctus-gateway"`,
		`"processEnvironment":true`,
		`${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}`,
		`${AGINCTUS_GATEWAY_MASTER_KEY}`,
		`${AGINCTUS_GATEWAY_SALT_KEY}`,
	} {
		if !strings.Contains(configuration, want) {
			t.Fatalf("configuration missing %q: %s", want, configuration)
		}
	}
}

func TestValidateRuntimeEnvironmentRejectsMissingSecret(t *testing.T) {
	for _, name := range requiredRuntimeEnvironment {
		t.Setenv(name, "set")
	}
	t.Setenv("AGINCTUS_GATEWAY_MASTER_KEY", "")

	if err := ValidateRuntimeEnvironment(); err == nil {
		t.Fatal("ValidateRuntimeEnvironment() error = nil, want error")
	}
}
