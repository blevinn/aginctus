package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

type fakeIncusClient struct {
	version string
	err     error
}

func (c fakeIncusClient) ServerVersion(context.Context) (string, error) {
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

func TestDoctorSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{}
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, fakeIncusClient{version: "7.0.1"}, loader)

	if code != 0 {
		t.Fatalf("Run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "incus daemon: reachable (7.0.1)") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestConfigurationOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{}
	code := Run(
		context.Background(),
		[]string{
			"--configuration-file=custom.json",
			"--configure=z.b.c=xyz",
			"--configure", "feature.enabled=true",
			"doctor",
		},
		&stdout,
		&stderr,
		fakeIncusClient{version: "7.0.1"},
		loader,
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

func TestConfigGetWithOverride(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil

	code := Run(
		context.Background(),
		[]string{"--configure=z.b.c=xyz", "config", "get", "z.b.c"},
		&stdout,
		&stderr,
		fakeIncusClient{},
		loader,
	)

	if code != 0 {
		t.Fatalf("Run() code = %d; stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != `"xyz"` {
		t.Fatalf("stdout = %q, want %q", got, `"xyz"`)
	}
}

func TestConfigurationLoadFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	loader := &fakeConfigLoader{err: errors.New("bad json")}
	code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr, fakeIncusClient{}, loader)

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
	code := Run(context.Background(), []string{"nope"}, &stdout, &stderr, fakeIncusClient{}, loader)

	if code != 2 {
		t.Fatalf("Run() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
