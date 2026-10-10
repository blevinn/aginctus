package gateway

import (
	"os"
	"path/filepath"
	"reflect"
	"sigs.k8s.io/yaml"
	"strings"
	"sync"
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

func TestInitializeRuntimeEnvironmentConcurrentFirstUse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, name := range requiredRuntimeEnvironment {
		t.Setenv(name, "")
	}
	const callers = 24
	var wg sync.WaitGroup
	begin := make(chan struct{})
	results := make([]map[string]string, callers)
	errs := make([]error, callers)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-begin
			results[i], errs[i] = (Spec{ID: "concurrent"}).InitializeRuntimeEnvironment()
		}(i)
	}
	close(begin)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d failed: %v", i, err)
		}
		for _, key := range requiredRuntimeEnvironment {
			if results[i][key] == "" || results[i][key] != results[0][key] {
				t.Fatalf("caller %d got divergent value for %s", i, key)
			}
		}
	}
	persisted, err := (Spec{ID: "concurrent"}).InitializeRuntimeEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range requiredRuntimeEnvironment {
		if persisted[key] != results[0][key] {
			t.Fatalf("persisted %s differs from concurrent result", key)
		}
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
