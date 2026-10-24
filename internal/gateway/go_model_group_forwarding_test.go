package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/blevinn/aginctus/internal/config"
	composeproject "github.com/lxc/incus-compose/project"
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
		LiteLLMImage: "ghcr.io/berriai/litellm:v1.103.0-stable",
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

type goForwardingCompose struct {
	Services map[string]struct {
		Environment map[string]string `json:"environment"`
		Configs []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"configs"`
	} `json:"services"`
	Configs map[string]struct {
		Content string `json:"content"`
	} `json:"configs"`
}

func TestGoModelGroupForwardingDeclarativeComposeParity(t *testing.T) {
	spec := Spec{
		ID: "local", Project: "aginctus-gateway", Network: "aginctus-mgmt",
		LiteLLMImage: "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage: "docker.io/library/postgres:17-alpine",
		OpenCodeGoForwarding: true,
	}

	rendered, err := spec.RenderCompose()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.YAMLToJSON([]byte(rendered))
	if err != nil {
		t.Fatal(err)
	}
	var standalone goForwardingCompose
	if err := json.Unmarshal(encoded, &standalone); err != nil {
		t.Fatal(err)
	}

	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[1].Driver != "compose" {
		t.Fatalf("unexpected gateway plan: %#v", plan.Steps)
	}
	var planned struct {
		Compose goForwardingCompose `json:"compose"`
	}
	if err := json.Unmarshal(plan.Steps[1].Configuration, &planned); err != nil {
		t.Fatal(err)
	}

	for name, compose := range map[string]goForwardingCompose{
		"render": standalone, "plan": planned.Compose,
	} {
		service, ok := compose.Services["litellm"]
		if !ok {
			t.Fatalf("%s missing LiteLLM service", name)
		}
		if got := service.Environment["CONFIG_FILE_PATH"]; got != openCodeGoConfigPath {
			t.Fatalf("%s CONFIG_FILE_PATH = %q", name, got)
		}
		if len(service.Configs) != 1 ||
			service.Configs[0].Source != "aginctus-opencode-go-forwarding" ||
			service.Configs[0].Target != openCodeGoConfigPath {
			t.Fatalf("%s: incorrect Compose configs target/source: %#v", name, service.Configs)
		}
		if len(compose.Services["postgres"].Configs) != 0 {
			t.Fatalf("%s: PostgreSQL must not receive forwarding config", name)
		}
		if service.Environment["STORE_MODEL_IN_DB"] != "True" {
			t.Fatalf("%s lost database model registry", name)
		}
		file, ok := compose.Configs["aginctus-opencode-go-forwarding"]
		if !ok || file.Content != openCodeGoForwardingYAML {
			t.Fatalf("%s: inline config does not match approved policy", name)
		}
	}

	// Keep one reviewed policy across both renderers and the older offline fixture.
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "litellm-go", "model-group-forwarding.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	normalized, err := yaml.YAMLToJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(normalized, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		standalone.Configs["aginctus-opencode-go-forwarding"].Content,
		planned.Compose.Configs["aginctus-opencode-go-forwarding"].Content,
	} {
		var actual map[string]any
		data, err := yaml.YAMLToJSON([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &actual); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, fixture) {
			t.Fatalf("declarative policy differs from approved exact model scope: %#v", actual)
		}
	}
}

func TestGoModelGroupForwardingDisabledInOrchestrationPlan(t *testing.T) {
	spec := Spec{
		ID: "local", Project: "aginctus-gateway", Network: "aginctus-mgmt",
		LiteLLMImage: "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage: "docker.io/library/postgres:17-alpine",
	}
	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	var planned struct {
		Compose goForwardingCompose `json:"compose"`
	}
	if err := json.Unmarshal(plan.Steps[len(plan.Steps)-1].Configuration, &planned); err != nil {
		t.Fatal(err)
	}
	if planned.Compose.Services["litellm"].Environment["CONFIG_FILE_PATH"] != "" ||
		len(planned.Compose.Services["litellm"].Configs) != 0 ||
		len(planned.Compose.Configs) != 0 {
		t.Fatal("disabled policy unexpectedly activates forwarded client headers")
	}
}

func TestGoModelGroupForwardingConfigRequiresExplicitBooleanOptIn(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	for _, tc := range []struct {
		name string
		value string
		wantEnabled bool
		wantError bool
	}{
		{name: "default", value: "", wantEnabled: false},
		{name: "enabled", value: "true", wantEnabled: true},
		{name: "disabled", value: "false", wantEnabled: false},
		{name: "reject string", value: "enabled", wantError: true},
		{name: "reject number", value: "1", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var overrides []string
			if tc.value != "" {
				overrides = append(overrides, "gateway.litellm.opencodeGoForwarding=" + tc.value)
			}
			effective, err := loader.Load(config.Options{Overrides: overrides})
			if err != nil {
				t.Fatal(err)
			}
			spec, err := FromConfig(effective)
			if tc.wantError {
				if err == nil {
					t.Fatal("accepted non-boolean model-scoped forwarding configuration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if spec.OpenCodeGoForwarding != tc.wantEnabled {
				t.Fatalf("enabled=%v, want %v", spec.OpenCodeGoForwarding, tc.wantEnabled)
			}
		})
	}
}

func TestGoModelGroupForwardingPinnedComposeParser(t *testing.T) {
	spec := Spec{
		ID: "local", Project: "aginctus-gateway", Network: "aginctus-mgmt",
		LiteLLMImage: "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage: "docker.io/library/postgres:17-alpine",
		OpenCodeGoForwarding: true,
	}
	content, err := spec.RenderCompose()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"AGINCTUS_GATEWAY_MASTER_KEY",
		"AGINCTUS_GATEWAY_SALT_KEY",
		"AGINCTUS_GATEWAY_POSTGRES_PASSWORD",
	} {
		t.Setenv(name, "synthetic-only")
	}

	project, err := composeproject.New().Load(context.Background(),
		composeproject.LoadFiles([]string{file}),
		composeproject.LoadName("aginctus-gateway"),
		composeproject.LoadOsEnv(),
	)
	if err != nil {
		t.Fatalf("pinned incus-compose cannot load inline config: %v", err)
	}
	definition, ok := project.Configs["aginctus-opencode-go-forwarding"]
	if !ok || definition.Content != openCodeGoForwardingYAML {
		t.Fatalf("pinned incus-compose did not retain approved config content")
	}
	service, ok := project.Services["litellm"]
	if !ok || len(service.Configs) != 1 ||
		service.Configs[0].Target != openCodeGoConfigPath {
		t.Fatal("pinned incus-compose lost LiteLLM config attachment")
	}
}
