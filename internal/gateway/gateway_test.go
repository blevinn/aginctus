package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sigs.k8s.io/yaml"
	"strings"
	"testing"
)

func TestRenderCompose(t *testing.T) {
	spec := Spec{
		ID:                 "local",
		Project:            "aginctus-gateway",
		Network:            "aginctus-mgmt",
		NetworkIPv4Address: "10.42.0.1/24",
		NetworkIPv4NAT:     false,
		NetworkIPv4Routing: false,
		NetworkIPv6Address: "none",
		LiteLLMImage:       "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage:      "docker.io/library/postgres:17-alpine",
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

func TestOrchestrationPlanOrdersNetworkBeforeComposeAndKeepsSecretReferences(t *testing.T) {
	spec := Spec{
		ID:                 "local",
		Project:            "aginctus-gateway",
		Network:            "aginctus-mgmt",
		NetworkIPv4Address: "10.42.0.1/24",
		NetworkIPv4NAT:     false,
		NetworkIPv4Routing: false,
		NetworkIPv6Address: "none",
		LiteLLMImage:       "ghcr.io/berriai/litellm:v1.103.0-stable",
		PostgresImage:      "docker.io/library/postgres:17-alpine",
	}

	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatalf("OrchestrationPlan() error = %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("plan steps = %#v", plan.Steps)
	}
	if plan.Steps[0].ID != "management-network" || plan.Steps[0].Driver != "apply" {
		t.Fatalf("first plan step = %#v", plan.Steps[0])
	}
	if plan.Steps[1].ID != "gateway" || plan.Steps[1].Driver != "compose" {
		t.Fatalf("second plan step = %#v", plan.Steps[1])
	}
	networkConfiguration := string(plan.Steps[0].Configuration)
	for _, want := range []string{"aginctus-mgmt", "10.42.0.1/24", "user.aginctus.managed", "management-network"} {
		if !strings.Contains(networkConfiguration, want) {
			t.Fatalf("network configuration missing %q: %s", want, networkConfiguration)
		}
	}
	configuration := string(plan.Steps[1].Configuration)
	for _, want := range []string{
		"aginctus-gateway",
		`${AGINCTUS_GATEWAY_POSTGRES_PASSWORD}`,
		`${AGINCTUS_GATEWAY_MASTER_KEY}`,
		`${AGINCTUS_GATEWAY_SALT_KEY}`,
	} {
		if !strings.Contains(configuration, want) {
			t.Fatalf("configuration missing %q: %s", want, configuration)
		}
	}
}

func TestInitializeRuntimeEnvironmentGeneratesAndPersistsMissingSecrets(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	for _, name := range requiredRuntimeEnvironment {
		t.Setenv(name, "")
	}

	spec := Spec{ID: "local"}
	first, err := spec.InitializeRuntimeEnvironment()
	if err != nil {
		t.Fatalf("InitializeRuntimeEnvironment() error = %v", err)
	}
	second, err := spec.InitializeRuntimeEnvironment()
	if err != nil {
		t.Fatalf("InitializeRuntimeEnvironment() second error = %v", err)
	}

	for _, name := range requiredRuntimeEnvironment {
		if first[name] == "" {
			t.Fatalf("%s was not generated", name)
		}
		if second[name] != first[name] {
			t.Fatalf("%s changed between runs", name)
		}
	}

	info, err := os.Stat(filepath.Join(state, "aginctus", "gateway", "local", "secrets.json"))
	if err != nil {
		t.Fatalf("stat secret state: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret state mode = %o, want 600", info.Mode().Perm())
	}
}

func TestInitializeRuntimeEnvironmentUsesProvidedSeedOnlyWhenMissing(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	for _, name := range requiredRuntimeEnvironment {
		t.Setenv(name, "")
	}
	t.Setenv("AGINCTUS_GATEWAY_MASTER_KEY", "seeded-value")

	spec := Spec{ID: "local"}
	first, err := spec.InitializeRuntimeEnvironment()
	if err != nil {
		t.Fatalf("InitializeRuntimeEnvironment() error = %v", err)
	}
	if first["AGINCTUS_GATEWAY_MASTER_KEY"] != "seeded-value" {
		t.Fatalf("seeded master key = %q", first["AGINCTUS_GATEWAY_MASTER_KEY"])
	}

	t.Setenv("AGINCTUS_GATEWAY_MASTER_KEY", "replacement")
	second, err := spec.InitializeRuntimeEnvironment()
	if err != nil {
		t.Fatalf("InitializeRuntimeEnvironment() second error = %v", err)
	}
	if second["AGINCTUS_GATEWAY_MASTER_KEY"] != "seeded-value" {
		t.Fatalf("persisted master key = %q, want original seed", second["AGINCTUS_GATEWAY_MASTER_KEY"])
	}
}

func TestGatewayIDRejectsTraversalAndSeparators(t *testing.T) {
	for _, id := range []string{"../outside", ".", "..", "nested/path", "nested\\\\path", "/absolute", ""} {
		t.Run(id, func(t *testing.T) {
			spec := Spec{ID: id, Project: "gateway", Network: "mgmt", LiteLLMImage: "litellm", PostgresImage: "postgres"}
			if err := spec.Validate(); err == nil {
				t.Fatalf("accepted unsafe gateway ID %q", id)
			}
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			if _, err := spec.InitializeRuntimeEnvironment(); err == nil {
				t.Fatalf("initialized unsafe gateway ID %q", id)
			}
		})
	}
	for _, id := range []string{"local", "gateway-01", "gateway_01"} {
		spec := Spec{ID: id, Project: "gateway", Network: "mgmt", LiteLLMImage: "litellm", PostgresImage: "postgres"}
		if err := spec.Validate(); err != nil {
			t.Fatalf("valid gateway ID %q rejected: %v", id, err)
		}
	}
}

func TestGatewaySecretStateRejectsSymlinkAndLooseMode(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Join(state, "aginctus", "gateway", "local")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secrets.json")
	outside := filepath.Join(state, "outside")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := (Spec{ID: "local"}).InitializeRuntimeEnvironment(); err == nil {
		t.Fatal("accepted symlink secret state")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Spec{ID: "local"}).InitializeRuntimeEnvironment(); err == nil {
		t.Fatal("accepted loosely permissioned secret state")
	}
}

func TestRenderedGatewayComposeMatchesDeploymentModel(t *testing.T) {
	spec := Spec{
		ID: "parity", Project: "gateway-parity", Network: "management-parity",
		NetworkIPv4Address: "10.42.0.1/24", NetworkIPv6Address: "none",
		LiteLLMImage: "example/litellm:custom", PostgresImage: "example/postgres:custom",
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

	plan, err := spec.OrchestrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("plan steps = %d", len(plan.Steps))
	}
	var config struct {
		Compose map[string]any `json:"compose"`
	}
	if err := json.Unmarshal(plan.Steps[1].Configuration, &config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(renderModel, config.Compose) {
		renderJSON, _ := json.MarshalIndent(renderModel, "", "  ")
		deployJSON, _ := json.MarshalIndent(config.Compose, "", "  ")
		t.Fatalf("render and deploy Compose models differ:\\nrender=%s\\ndeploy=%s", renderJSON, deployJSON)
	}
}

func TestGatewayRejectsUnsafeSeedValues(t *testing.T) {
	for _, seed := range []string{"a\nb", "a$b", "a#b", "a=b", "a:b", "a@b", "a/b", "a\\\\b", "a'b", "a\"b", "a b"} {
		t.Run(fmt.Sprintf("%q", seed), func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			for _, key := range requiredRuntimeEnvironment {
				t.Setenv(key, "")
			}
			t.Setenv("AGINCTUS_GATEWAY_POSTGRES_PASSWORD", seed)
			if _, err := (Spec{ID: "local"}).InitializeRuntimeEnvironment(); err == nil {
				t.Fatal("accepted unsafe credential seed")
			} else if strings.Contains(err.Error(), seed) {
				t.Fatal("error exposed secret material")
			}
		})
	}
}

func TestGatewayAcceptsSafeCredentialSeed(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, key := range requiredRuntimeEnvironment {
		t.Setenv(key, "")
	}
	t.Setenv("AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AZaz09_-safe")
	values, err := (Spec{ID: "local"}).InitializeRuntimeEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if values["AGINCTUS_GATEWAY_POSTGRES_PASSWORD"] != "AZaz09_-safe" {
		t.Fatal("valid seed changed unexpectedly")
	}
}
