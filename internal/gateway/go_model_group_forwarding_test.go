package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// The built-in option chooses model groups; it does not restrict header names.
// Assert that this opt-in fixture cannot silently become wildcard/global.
func TestGoModelGroupForwardingConfigIsStrictlyScoped(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "litellm-go", "model-group-forwarding.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := yaml.YAMLToJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(normalized, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"model_list": []any{},
		"litellm_settings": map[string]any{
			"model_group_settings": map[string]any{
				"forward_client_headers_to_llm_api": []any{"aginctus-go-pilot"},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opt-in Go forwarding config changed scope or enabled other behavior: got %#v", got)
	}
}

func TestGoModelGroupForwardingDoesNotActivateStockGateway(t *testing.T) {
	spec := Spec{
		ID: "local", Project: "aginctus-gateway", Network: "aginctus-mgmt",
		LiteLLMImage:  "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage: "docker.io/library/postgres:17-alpine",
	}
	rendered, err := spec.RenderCompose()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"model-group-forwarding.yaml", "CONFIG_FILE_PATH",
		"WORKER_CONFIG", "forward_client_headers_to_llm_api",
		"litellm_opencode_go_hook",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("stock gateway unexpectedly activates model-group forwarding via %q", forbidden)
		}
	}
}
