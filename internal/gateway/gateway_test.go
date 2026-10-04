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
