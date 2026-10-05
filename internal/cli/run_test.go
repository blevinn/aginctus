package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
)

type fakeIncusClient struct {
	version string
	err     error
}


func (c *fakeIncusClient) ServerVersion(context.Context) (string, error) {
	return c.version, c.err
}

type fakeConfigLoader struct {
	cfg     *config.Config
	err     error
	options config.Options
}

func (l *fakeConfigLoader) Load(options config.Options) (*config.Config, error) {
	l.options = options
	if l.err != nil {
		return nil, l.err
	}
	if l.cfg != nil {
		return l.cfg, nil
	}
	return (&config.Loader{}).Load(config.Options{})
}

func stubManagementNetwork(t *testing.T, fn func(context.Context, managementnetwork.Spec, managementnetwork.Options) error) {
	t.Helper()
	previous := executeManagementNetwork
	executeManagementNetwork = fn
	t.Cleanup(func() { executeManagementNetwork = previous })
}

func stubHerdrClient(t *testing.T, fn func(context.Context, herdrclient.Spec, herdrclient.Options) error) {
	t.Helper()
	previous := executeHerdrClient
	executeHerdrClient = fn
	t.Cleanup(func() { executeHerdrClient = previous })
}

func TestDoctorSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{}
	client := &fakeIncusClient{version: "7.0.1"}
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, client, loader)

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "incus daemon: reachable (7.0.1)") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestConfigAlias(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{}
	client := &fakeIncusClient{version: "7.0.1"}
	code := Run(
		context.Background(),
		[]string{"--config=z.b.c=xyz", "doctor"},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if len(loader.options.Overrides) != 1 || loader.options.Overrides[0] != "z.b.c=xyz" {
		t.Fatalf("Overrides = %#v", loader.options.Overrides)
	}
}

func TestConfigurationOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{}
	client := &fakeIncusClient{version: "7.0.1"}
	code := Run(
		context.Background(),
		[]string{
			"--configuration-file=custom.json",
			"--configure=z.b.c=xyz",
			"--config", "feature.enabled=true",
			"doctor",
		},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if loader.options.ConfigurationFile != "custom.json" {
		t.Fatalf("ConfigurationFile = %q", loader.options.ConfigurationFile)
	}
	if len(loader.options.Overrides) != 2 {
		t.Fatalf("Overrides = %#v", loader.options.Overrides)
	}
}

func TestNetworkEnsureUsesEffectiveConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	var gotSpec managementnetwork.Spec
	var gotOptions managementnetwork.Options
	stubManagementNetwork(t, func(_ context.Context, spec managementnetwork.Spec, options managementnetwork.Options) error {
		gotSpec = spec
		gotOptions = options
		return nil
	})

	code := Run(
		context.Background(),
		[]string{
			"--config=incus.management.network.name=lab-mgmt",
			"--config=incus.management.network.ipv4.address=10.42.0.1/24",
			"--config=incus.management.network.ipv4.nat=true",
			"network", "ensure", "--dry-run", "--force",
		},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if gotSpec.Name != "lab-mgmt" || gotSpec.IPv4Address != "10.42.0.1/24" {
		t.Fatalf("network spec = %#v", gotSpec)
	}
	if !gotSpec.IPv4NAT || gotSpec.IPv4Routing {
		t.Fatalf("network policy = %#v", gotSpec)
	}
	if !gotOptions.DryRun || !gotOptions.Force || gotOptions.Operation != "upsert" {
		t.Fatalf("network options = %#v", gotOptions)
	}
	if !strings.Contains(stdout.String(), "would reconcile") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNetworkEnsureRejectsWrongConfigurationType(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	code := Run(
		context.Background(),
		[]string{"--config=incus.management.network.ipv4.nat=5", "network", "ensure"},
		&stdout, &stderr, client, loader,
	)

	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "must be a boolean") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConfigurationLoadFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{err: errors.New("bad json")}
	client := &fakeIncusClient{}
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, client, loader)

	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bad json") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{}
	client := &fakeIncusClient{}
	code := Run(context.Background(), []string{"nope"}, &stdout, &stderr, client, loader)

	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestNetworkEnsureDryRunOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}
	stubManagementNetwork(t, func(_ context.Context, _ managementnetwork.Spec, options managementnetwork.Options) error {
		if !options.DryRun {
			t.Fatal("DryRun = false, want true")
		}
		return nil
	})

	code := Run(context.Background(), []string{"network", "ensure", "--dry-run"}, &stdout, &stderr, client, loader)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would reconcile") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNetworkTeardownUsesConfiguredNameAndOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	var gotSpec managementnetwork.Spec
	var gotOptions managementnetwork.Options
	stubManagementNetwork(t, func(_ context.Context, spec managementnetwork.Spec, options managementnetwork.Options) error {
		gotSpec = spec
		gotOptions = options
		return nil
	})

	code := Run(
		context.Background(),
		[]string{
			"--config=incus.management.network.name=lab-mgmt",
			"network", "teardown", "--dry-run", "--force",
		},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if gotSpec.Name != "lab-mgmt" {
		t.Fatalf("teardown spec = %#v", gotSpec)
	}
	if !gotOptions.DryRun || !gotOptions.Force || gotOptions.Operation != "delete" {
		t.Fatalf("teardown options = %#v", gotOptions)
	}
	if !strings.Contains(stdout.String(), "would delete") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNetworkTeardownAbsent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}
	stubManagementNetwork(t, func(context.Context, managementnetwork.Spec, managementnetwork.Options) error { return nil })

	code := Run(context.Background(), []string{"network", "teardown"}, &stdout, &stderr, client, loader)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "absent") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNetworkRejectsUnknownOption(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	code := Run(context.Background(), []string{"network", "ensure", "--wat"}, &stdout, &stderr, client, loader)

	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown option") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHerdrClientEnsureUsesEffectiveConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	var gotSpec herdrclient.Spec
	var gotOptions herdrclient.Options
	stubHerdrClient(t, func(_ context.Context, spec herdrclient.Spec, options herdrclient.Options) error {
		gotSpec = spec
		gotOptions = options
		return nil
	})

	code := Run(
		context.Background(),
		[]string{
			"--config=infrastructure.herdr.name=lab-herdr",
			"--config=infrastructure.herdr.image.alias=lab-herdr-image",
			"--config=infrastructure.herdr.storage.pool=fast",
			"--config=incus.management.network.name=lab-mgmt",
			"herdr", "client", "ensure", "--dry-run",
		},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if gotSpec.Name != "lab-herdr" || gotSpec.ImageAlias != "lab-herdr-image" || gotSpec.StoragePool != "fast" || gotSpec.ManagementNetwork != "lab-mgmt" {
		t.Fatalf("Herdr spec = %#v", gotSpec)
	}
	if !gotOptions.DryRun || gotOptions.Force || gotOptions.Operation != "upsert" {
		t.Fatalf("Herdr options = %#v", gotOptions)
	}
	if !strings.Contains(stdout.String(), "would reconcile") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHerdrClientTeardownUsesConfiguredNameAndForce(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	var gotSpec herdrclient.Spec
	var gotOptions herdrclient.Options
	stubHerdrClient(t, func(_ context.Context, spec herdrclient.Spec, options herdrclient.Options) error {
		gotSpec = spec
		gotOptions = options
		return nil
	})

	code := Run(
		context.Background(),
		[]string{"--config=infrastructure.herdr.name=lab-herdr", "herdr", "client", "teardown", "--force"},
		&stdout, &stderr, client, loader,
	)
	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if gotSpec.Name != "lab-herdr" || !gotOptions.Force || gotOptions.Operation != "delete" {
		t.Fatalf("spec = %#v options = %#v", gotSpec, gotOptions)
	}
	if !strings.Contains(stdout.String(), "absent") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHerdrClientStopsWhenOrchestrationFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}
	stubHerdrClient(t, func(context.Context, herdrclient.Spec, herdrclient.Options) error {
		return errors.New("herdr orchestration failed")
	})

	code := Run(context.Background(), []string{"herdr", "client", "ensure"}, &stdout, &stderr, client, loader)
	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "herdr orchestration failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestGatewayRenderUsesEffectiveConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	code := Run(
		context.Background(),
		[]string{
			"--config=gateway.id=lab",
			"--config=gateway.compose.project=lab-gateway",
			"--config=incus.management.network.name=lab-mgmt",
			"gateway", "render",
		},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"name: lab-gateway", "name: lab-mgmt"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %q", want, stdout.String())
		}
	}
}

func TestGatewayUpStopsBeforeNetworkMutationWhenSecretInitializationFails(t *testing.T) {
	stateRoot := t.TempDir()
	blocked := filepath.Join(stateRoot, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write blocked state path: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", blocked)

	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{}

	code := Run(context.Background(), []string{"gateway", "up"}, &stdout, &stderr, client, loader)
	if code != 1 {
		t.Fatalf("Run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "gateway initialization") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
