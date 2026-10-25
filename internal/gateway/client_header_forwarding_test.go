package gateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	gatewayassets "github.com/blevinn/aginctus/scripts/gateway"
	composeproject "github.com/lxc/incus-compose/project"
	"sigs.k8s.io/yaml"
)

func TestSelectiveGoRenderAndPinnedLoader(t *testing.T) {
	for _, stock := range []string{"false", "[]"} {
		effective, err := (&config.Loader{}).Load(config.Options{Overrides: []string{
			"gateway.litellm.selectiveGoForwarding=true",
			"gateway.litellm.forwardClientHeaders=" + stock,
			"gateway.images.litellm=ghcr.io/berriai/litellm:v1.103.0@sha256:" + strings.Repeat("a", 64),
		}})
		if err != nil {
			t.Fatal(err)
		}
		spec, err := FromConfig(effective)
		if err != nil {
			t.Fatal(err)
		}
		_, compose := assertRenderedGatewayComposeMatchesDeploymentModel(t, spec)
		services := compose["services"].(map[string]any)
		if _, exists := services["postgres"].(map[string]any)["configs"]; exists {
			t.Fatal("bundle attached to PostgreSQL")
		}
		env := services["litellm"].(map[string]any)["environment"].(map[string]any)
		if env["PYTHONPATH"] != gatewayassets.PythonPath || env["CONFIG_FILE_PATH"] != gatewayassets.ConfigPath {
			t.Fatal("incorrect callback environment")
		}
		policy := parseForwardingPolicy(t, gatewayassets.Files()["config.yaml"])
		wantPolicy := map[string]any{"litellm_settings": map[string]any{"callbacks": []any{"litellm_opencode_go_hook.proxy_handler_instance"}}}
		if !reflect.DeepEqual(policy, wantPolicy) {
			t.Fatalf("unexpected callback policy: %#v", policy)
		}
		rendered, err := spec.RenderCompose()
		if err != nil {
			t.Fatal(err)
		}
		jsonnet, err := json.Marshal(compose)
		if err != nil {
			t.Fatal(err)
		}
		for _, document := range []string{rendered, string(jsonnet)} {
			file := filepath.Join(t.TempDir(), "compose.yaml")
			if err := os.WriteFile(file, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY", "AGINCTUS_GATEWAY_POSTGRES_PASSWORD"} {
				t.Setenv(key, "synthetic-only")
			}
			project, err := composeproject.New().Load(context.Background(), composeproject.LoadFiles([]string{file}), composeproject.LoadName(spec.Project), composeproject.LoadOsEnv())
			if err != nil {
				t.Fatal(err)
			}
			if len(project.Configs) != 3 || len(project.Services["litellm"].Configs) != 3 || len(project.Services["postgres"].Configs) != 0 {
				t.Fatal("incorrect loaded bundle attachments")
			}
			for name, raw := range gatewayassets.Files() {
				if got := project.Configs["aginctus-go-"+name].Content; got != raw {
					t.Fatalf("loader changed %s source bytes", name)
				}
				found := false
				for _, attachment := range project.Services["litellm"].Configs {
					if attachment.Source == "aginctus-go-"+name && attachment.Target == gatewayassets.PythonPath+"/"+name {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing attachment for %s", name)
				}
			}
		}
	}
}

func TestSelectiveGoDollarInterpolation(t *testing.T) {
	// Exercise future Python regex anchors and literal interpolation syntax even
	// when the current embedded sources happen not to contain dollar characters.
	const raw = "pattern = r'^session$'\nvalue = '$SESSION ${SESSION} $$ ${SESSION:-fallback}'\n"
	t.Setenv("SESSION", "must-not-enter-source")
	document, err := yaml.Marshal(map[string]any{
		"services": map[string]any{"litellm": map[string]any{
			"image":   "ghcr.io/berriai/litellm:v1.103.0",
			"configs": []map[string]string{{"source": "source", "target": "/app/example.py"}},
		}},
		"configs": map[string]any{"source": map[string]string{"content": composeLiteralContent(raw)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(file, document, 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := composeproject.New().Load(context.Background(), composeproject.LoadFiles([]string{file}), composeproject.LoadName("literal-source"), composeproject.LoadOsEnv())
	if err != nil {
		t.Fatal(err)
	}
	if got := project.Configs["source"].Content; got != raw {
		t.Fatalf("loader interpolated literal source: %q", got)
	}
}

func TestSelectiveGoValidation(t *testing.T) {
	base, err := forwardingSpec(t, "false")
	if err != nil {
		t.Fatal(err)
	}
	if base.SelectiveGoForwarding || base.LiteLLMImage != "ghcr.io/berriai/litellm:v1.103.0" {
		t.Fatal("default mode or image changed")
	}
	base.SelectiveGoForwarding = true
	valid := "ghcr.io/berriai/litellm@sha256:" + strings.Repeat("a", 64)
	for _, image := range []string{valid, strings.Replace(valid, "@", ":v1.103.0@", 1)} {
		base.LiteLLMImage = image
		if err := base.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, image := range []string{"ghcr.io/berriai/litellm:v1.103.0", valid + "a", valid[:len(valid)-1], strings.ToUpper(valid), strings.Replace(valid, "berriai", "other", 1), strings.Replace(valid, "litellm@", "litellm-extra@", 1), valid + "\n", strings.Replace(valid, "@", ":@", 1)} {
		base.LiteLLMImage = image
		if _, err := base.RenderCompose(); err == nil {
			t.Fatalf("render accepted image %q", image)
		}
		if _, err := base.OrchestrationPlan(); err == nil {
			t.Fatalf("plan accepted image %q", image)
		}
	}
	base.LiteLLMImage = valid
	for _, stock := range []Spec{{ForwardClientHeaders: true}, {ForwardClientHeaderModels: []string{"other-model"}}, {ForwardClientHeaderModels: []string{"aginctus-go-pilot"}}} {
		base.ForwardClientHeaders, base.ForwardClientHeaderModels = stock.ForwardClientHeaders, stock.ForwardClientHeaderModels
		if _, err := base.RenderCompose(); err == nil {
			t.Fatal("render accepted conflicting stock forwarding")
		}
		if _, err := base.OrchestrationPlan(); err == nil {
			t.Fatal("plan accepted conflicting stock forwarding")
		}
	}
	for _, value := range []string{`"true"`, "1", "null", "[]", "{}"} {
		effective, err := (&config.Loader{}).Load(config.Options{Overrides: []string{"gateway.litellm.selectiveGoForwarding=" + value}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := FromConfig(effective); err == nil {
			t.Fatalf("accepted nonboolean selective mode %s", value)
		}
	}
	for _, stock := range []string{"true", `["other-model"]`, `["aginctus-go-pilot"]`} {
		effective, err := (&config.Loader{}).Load(config.Options{Overrides: []string{
			"gateway.litellm.selectiveGoForwarding=true",
			"gateway.images.litellm=" + valid,
			"gateway.litellm.forwardClientHeaders=" + stock,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := FromConfig(effective); err == nil {
			t.Fatalf("config accepted conflicting forwarding %s", stock)
		}
	}
}

func forwardingSpec(t *testing.T, value string) (Spec, error) {
	t.Helper()
	loader := &config.Loader{}
	var overrides []string
	if value != "" {
		overrides = []string{"gateway.litellm.forwardClientHeaders=" + value}
	}
	effective, err := loader.Load(config.Options{Overrides: overrides})
	if err != nil {
		return Spec{}, err
	}
	return FromConfig(effective)
}

func parseForwardingPolicy(t *testing.T, content string) map[string]any {
	t.Helper()
	var policy map[string]any
	if err := yaml.Unmarshal([]byte(content), &policy); err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestClientHeaderForwardingRenderAndPinnedParser(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  string
		global bool
		models []string
	}{
		{name: "default"},
		{name: "disabled", value: "false"},
		{name: "empty list", value: "[]"},
		{name: "global", value: "true", global: true},
		{name: "pilot", value: `["aginctus-go-pilot"]`, models: []string{"aginctus-go-pilot"}},
		{name: "model patterns", value: `["team/model", "provider/*"]`, models: []string{"team/model", "provider/*"}},
		{name: "quoted YAML scalars", value: `["true", "alias: # literal"]`, models: []string{"true", "alias: # literal"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := forwardingSpec(t, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if spec.ForwardClientHeaders != tc.global || !reflect.DeepEqual(spec.ForwardClientHeaderModels, tc.models) {
				t.Fatalf("incorrect forwarding mode: global=%v models=%v", spec.ForwardClientHeaders, spec.ForwardClientHeaderModels)
			}
			_, compose := assertRenderedGatewayComposeMatchesDeploymentModel(t, spec)
			services := compose["services"].(map[string]any)
			litellm := services["litellm"].(map[string]any)
			postgres := services["postgres"].(map[string]any)
			environment := litellm["environment"].(map[string]any)
			if _, exists := postgres["configs"]; exists {
				t.Fatal("PostgreSQL received a header policy")
			}
			if environment["STORE_MODEL_IN_DB"] != "True" {
				t.Fatal("database model registry was disabled")
			}
			enabled := tc.global || len(tc.models) != 0
			if !enabled {
				if _, exists := compose["configs"]; exists {
					t.Fatal("disabled forwarding declares Compose configs")
				}
				if _, exists := litellm["configs"]; exists {
					t.Fatal("disabled forwarding attaches a config")
				}
				if _, exists := environment["CONFIG_FILE_PATH"]; exists {
					t.Fatal("disabled forwarding overrides LiteLLM config")
				}
				if _, exists := environment["PYTHONPATH"]; exists {
					t.Fatal("disabled forwarding overrides Python path")
				}
			} else {
				if environment["CONFIG_FILE_PATH"] != clientHeadersConfigPath {
					t.Fatal("LiteLLM config path differs from attachment target")
				}
				wantAttachments := []any{map[string]any{"source": clientHeadersConfigName, "target": clientHeadersConfigPath}}
				if !reflect.DeepEqual(litellm["configs"], wantAttachments) {
					t.Fatalf("unexpected LiteLLM config attachments: %#v", litellm["configs"])
				}
				configs := compose["configs"].(map[string]any)
				if len(configs) != 1 {
					t.Fatalf("unexpected Compose configs: %#v", configs)
				}
				content := configs[clientHeadersConfigName].(map[string]any)["content"].(string)
				want := map[string]any{"model_list": []any{}}
				if tc.global {
					want["general_settings"] = map[string]any{"forward_client_headers_to_llm_api": true}
				} else {
					models := make([]any, len(tc.models))
					for i, model := range tc.models {
						models[i] = model
					}
					want["litellm_settings"] = map[string]any{
						"model_group_settings": map[string]any{"forward_client_headers_to_llm_api": models},
					}
				}
				if got := parseForwardingPolicy(t, content); !reflect.DeepEqual(got, want) {
					t.Fatalf("forwarding policy = %#v, want %#v", got, want)
				}
				if tc.name == "pilot" {
					raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "litellm-go", "model-group-forwarding.yaml"))
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(parseForwardingPolicy(t, string(raw)), want) {
						t.Fatal("pilot model-list policy differs from approved offline fixture")
					}
				}
			}

			// Exercise the pinned Compose loader's interpolation and config attachment handling.
			rendered, err := spec.RenderCompose()
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "compose.yaml")
			if err := os.WriteFile(file, []byte(rendered), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY", "AGINCTUS_GATEWAY_POSTGRES_PASSWORD"} {
				t.Setenv(key, "synthetic-only")
			}
			project, err := composeproject.New().Load(context.Background(),
				composeproject.LoadFiles([]string{file}), composeproject.LoadName(spec.Project), composeproject.LoadOsEnv())
			if err != nil {
				t.Fatalf("pinned Compose loader: %v", err)
			}
			if !enabled {
				if len(project.Configs) != 0 || len(project.Services["litellm"].Configs) != 0 {
					t.Fatal("pinned Compose activated a disabled policy")
				}
				return
			}
			definition, ok := project.Configs[clientHeadersConfigName]
			wantContent := compose["configs"].(map[string]any)[clientHeadersConfigName].(map[string]any)["content"]
			if !ok || definition.Content != wantContent {
				t.Fatal("pinned Compose changed the inline policy")
			}
			service := project.Services["litellm"]
			if len(service.Configs) != 1 || service.Configs[0].Target != clientHeadersConfigPath {
				t.Fatal("pinned Compose lost LiteLLM policy attachment")
			}
		})
	}
}

func TestClientHeaderForwardingRejectsInvalidInput(t *testing.T) {
	for _, value := range []string{
		`"true"`, "1", "null", `{}`, `[true]`, `["model", 1]`, `[null]`, `[""]`, `["  "]`,
		`["model\nother"]`, `["${AGINCTUS_GATEWAY_MASTER_KEY}"]`,
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := forwardingSpec(t, value); err == nil {
				t.Fatal("accepted invalid forwarding configuration")
			}
		})
	}
	// Direct Specs must not express a misleading global-with-exclusions policy.
	spec, err := forwardingSpec(t, "true")
	if err != nil {
		t.Fatal(err)
	}
	spec.ForwardClientHeaderModels = []string{"model"}
	if _, err := spec.RenderCompose(); err == nil {
		t.Fatal("accepted both global and model-group modes")
	}
	if _, err := spec.OrchestrationPlan(); err == nil {
		t.Fatal("planned both global and model-group modes")
	}
}

func TestClientHeaderForwardingRejectsPilotSpecificOption(t *testing.T) {
	effective, err := (&config.Loader{}).Load(config.Options{Overrides: []string{"gateway.litellm.opencodeGoForwarding=true"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromConfig(effective); err == nil {
		t.Fatal("silently ignored obsolete pilot-specific forwarding option")
	}
}
