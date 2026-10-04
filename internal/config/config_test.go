package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	userPath := filepath.Join(dir, "user.json")
	commandPath := filepath.Join(dir, "command.json")

	writeFile(t, systemPath, `{"z":{"b":{"c":"system"}}}`)
	writeFile(t, userPath, `{"z":{"b":{"c":"user"}}}`)
	writeFile(t, commandPath, `{"z":{"b":{"c":"command-file"}}}`)

	loader := &Loader{
		SystemPath: systemPath,
		UserPath:   userPath,
		Environment: []string{
			"AGINCTUS_Z_B_C=environment",
		},
	}

	cfg, err := loader.Load(Options{
		ConfigurationFile: commandPath,
		Overrides:         []string{"z.b.c=command-line"},
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got, ok := cfg.Get("z.b.c")
	if !ok || got != "command-line" {
		t.Fatalf("Get(z.b.c) = %#v, %v; want command-line", got, ok)
	}
}

func TestLoadFallsBackThroughSources(t *testing.T) {
	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.json")
	userPath := filepath.Join(dir, "user.json")

	writeFile(t, systemPath, `{"z":{"b":{"c":"system"}}}`)
	writeFile(t, userPath, `{"z":{"b":{"c":"user"}}}`)

	loader := &Loader{
		SystemPath:  systemPath,
		UserPath:    userPath,
		Environment: nil,
	}

	cfg, err := loader.Load(Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, _ := cfg.Get("z.b.c")
	if got != "user" {
		t.Fatalf("Get(z.b.c) = %#v, want user", got)
	}

	loader.UserPath = filepath.Join(dir, "missing.json")
	cfg, err = loader.Load(Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, _ = cfg.Get("z.b.c")
	if got != "system" {
		t.Fatalf("Get(z.b.c) = %#v, want system", got)
	}
}

func TestDefaultManagementNetwork(t *testing.T) {
	loader := &Loader{
		SystemPath:  filepath.Join(t.TempDir(), "missing-system.json"),
		UserPath:    filepath.Join(t.TempDir(), "missing-user.json"),
		Environment: nil,
	}

	cfg, err := loader.Load(Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := cfg.Get("incus.management.network.name")
	if !ok || got != "aginctus-mgmt" {
		t.Fatalf("management network name = %#v, %v", got, ok)
	}
}

func TestDefaultHerdrClientImageAlias(t *testing.T) {
	loader := &Loader{
		SystemPath:  filepath.Join(t.TempDir(), "missing-system.json"),
		UserPath:    filepath.Join(t.TempDir(), "missing-user.json"),
		Environment: nil,
	}

	cfg, err := loader.Load(Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, ok := cfg.Get("infrastructure.herdr.image.alias")
	if !ok || got != "aginctus-herdr-client" {
		t.Fatalf("Herdr client image alias = %#v, %v", got, ok)
	}
}

func TestEnvironmentParsesJSONValues(t *testing.T) {
	loader := &Loader{
		Environment: []string{
			"AGINCTUS_FEATURE_ENABLED=true",
			"AGINCTUS_LIMIT=12",
			"AGINCTUS_LABEL=hello",
		},
	}

	cfg, err := loader.Load(Options{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, _ := cfg.Get("feature.enabled"); got != true {
		t.Fatalf("feature.enabled = %#v, want true", got)
	}
	if got, _ := cfg.Get("limit"); got != float64(12) {
		t.Fatalf("limit = %#v, want 12", got)
	}
	if got, _ := cfg.Get("label"); got != "hello" {
		t.Fatalf("label = %#v, want hello", got)
	}
}

func TestInvalidOverride(t *testing.T) {
	loader := &Loader{}
	if _, err := loader.Load(Options{Overrides: []string{"missing-equals"}}); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
