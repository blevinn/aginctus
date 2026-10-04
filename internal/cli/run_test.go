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
	networkCreated bool
	networkErr     error
	networkSpec    incus.ManagementNetworkSpec
}

func (c *fakeIncusClient) ServerVersion(context.Context) (string, error) {
	return c.version, c.err
}

func (c *fakeIncusClient) EnsureManagementNetwork(_ context.Context, spec incus.ManagementNetworkSpec) (bool, error) {
	c.networkSpec = spec
	return c.networkCreated, c.networkErr
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
	client := &fakeIncusClient{networkCreated: true}

	code := Run(
		context.Background(),
		[]string{
			"--config=incus.management.network.name=lab-mgmt",
			"--config=incus.management.network.ipv4.address=10.42.0.1/24",
			"--config=incus.management.network.ipv4.nat=true",
			"network", "ensure",
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
