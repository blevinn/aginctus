package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/incus"
)

type fakeIncusClient struct {
	version        string
	err            error
	networkResult   incus.EnsureResult
	networkErr      error
	networkSpec     incus.ManagementNetworkSpec
	networkOptions  incus.MutationOptions
	teardownResult  incus.TeardownResult
	teardownErr     error
	teardownName    string
	teardownOptions incus.MutationOptions
	herdrResult     incus.EnsureResult
	herdrErr        error
	herdrSpec       incus.HerdrClientSpec
	herdrOptions    incus.MutationOptions
	herdrTeardown   incus.TeardownResult
	herdrName       string
}

func (c *fakeIncusClient) ServerVersion(context.Context) (string, error) {
	return c.version, c.err
}

func (c *fakeIncusClient) EnsureManagementNetwork(_ context.Context, spec incus.ManagementNetworkSpec, options incus.MutationOptions) (incus.EnsureResult, error) {
	c.networkSpec = spec
	c.networkOptions = options
	return c.networkResult, c.networkErr
}

func (c *fakeIncusClient) TeardownManagementNetwork(_ context.Context, name string, options incus.MutationOptions) (incus.TeardownResult, error) {
	c.teardownName = name
	c.teardownOptions = options
	return c.teardownResult, c.teardownErr
}

func (c *fakeIncusClient) EnsureHerdrClient(_ context.Context, spec incus.HerdrClientSpec, options incus.MutationOptions) (incus.EnsureResult, error) {
	c.herdrSpec = spec
	c.herdrOptions = options
	return c.herdrResult, c.herdrErr
}

func (c *fakeIncusClient) TeardownHerdrClient(_ context.Context, name string, options incus.MutationOptions) (incus.TeardownResult, error) {
	c.herdrName = name
	c.herdrOptions = options
	return c.herdrTeardown, c.herdrErr
}

func (c *fakeIncusClient) InstanceArchitecture(context.Context, string) (string, error) {
	return "x86_64", nil
}

func (c *fakeIncusClient) WriteInstanceFile(context.Context, string, string, []byte, int) error {
	return nil
}

func (c *fakeIncusClient) ExecInstance(context.Context, string, []string) (string, error) {
	return "herdr 0.9.1\n", nil
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
	client := &fakeIncusClient{networkResult: incus.EnsureResult{Created: true}}

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
	if client.networkSpec.Name != "lab-mgmt" || client.networkSpec.IPv4Address != "10.42.0.1/24" {
		t.Fatalf("network spec = %#v", client.networkSpec)
	}
	if !client.networkSpec.IPv4NAT || client.networkSpec.IPv4Routing {
		t.Fatalf("network policy = %#v", client.networkSpec)
	}
	if !client.networkOptions.DryRun || !client.networkOptions.Force {
		t.Fatalf("network options = %#v", client.networkOptions)
	}
	if !strings.Contains(stdout.String(), "created") {
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
	client := &fakeIncusClient{networkResult: incus.EnsureResult{Updated: true, DryRun: true}}

	code := Run(
		context.Background(),
		[]string{"network", "ensure", "--dry-run"},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would update") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestNetworkTeardownUsesConfiguredNameAndOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{teardownResult: incus.TeardownResult{Deleted: true, DryRun: true}}

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
	if client.teardownName != "lab-mgmt" {
		t.Fatalf("teardown name = %q", client.teardownName)
	}
	if !client.teardownOptions.DryRun || !client.teardownOptions.Force {
		t.Fatalf("teardown options = %#v", client.teardownOptions)
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
	client := &fakeIncusClient{herdrResult: incus.EnsureResult{Created: true, DryRun: true}}

	code := Run(
		context.Background(),
		[]string{
			"--config=infrastructure.herdr.name=lab-herdr",
			"--config=infrastructure.herdr.storage.pool=fast",
			"--config=incus.management.network.name=lab-mgmt",
			"herdr", "client", "ensure", "--dry-run",
		},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if client.herdrSpec.Name != "lab-herdr" || client.herdrSpec.StoragePool != "fast" || client.herdrSpec.ManagementNetwork != "lab-mgmt" {
		t.Fatalf("Herdr spec = %#v", client.herdrSpec)
	}
	if !client.herdrOptions.DryRun {
		t.Fatalf("Herdr options = %#v", client.herdrOptions)
	}
	if !strings.Contains(stdout.String(), "would create") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHerdrClientTeardownUsesConfiguredName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	client := &fakeIncusClient{herdrTeardown: incus.TeardownResult{Deleted: true}}

	code := Run(
		context.Background(),
		[]string{"--config=infrastructure.herdr.name=lab-herdr", "herdr", "client", "teardown", "--force"},
		&stdout, &stderr, client, loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if client.herdrName != "lab-herdr" || !client.herdrOptions.Force {
		t.Fatalf("name = %q options = %#v", client.herdrName, client.herdrOptions)
	}
}
