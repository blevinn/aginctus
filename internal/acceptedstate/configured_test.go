package acceptedstate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func loadConfigured(t *testing.T, jsonText string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(path, []byte(jsonText), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := (&config.Loader{Environment: []string{}}).Load(config.Options{ConfigurationFile: path})
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestConfiguredProjectionIsReadOnlyAndExplicit(t *testing.T) {
	cfg := loadConfigured(t, `{"incus":{"project":"tenant-a"},"workloads":{"alpha":{"image":{"alias":"test"}}},"gateway":{"id":"sensitive-nonsecret-but-not-selected"}}`)
	selected := []string{"alpha", "dev"}
	manifest, err := FromConfigured(cfg, "work", selected)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest.SelectedWorkloads, selected) {
		t.Fatalf("selected workloads changed: %+v", manifest)
	}
	if manifest.DeploymentID != "work" || manifest.IncusProject != "tenant-a" {
		t.Fatalf("identity incorrect: %+v", manifest)
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sensitive-nonsecret", `"image"`, `"gateway"`, `"password"`} {
		if strings.Contains(string(canonical), forbidden) {
			t.Fatalf("non-selection value leaked into candidate: %q", forbidden)
		}
	}
	selected[0] = "modified"
	if manifest.SelectedWorkloads[0] != "alpha" {
		t.Fatal("candidate retained caller's mutable slice")
	}
}

func TestConfiguredProjectionFailClosed(t *testing.T) {
	cfg := loadConfigured(t, `{}`)
	for _, tc := range []struct {
		name       string
		deployment string
		ids        []string
	}{
		{"nil selected", "work", nil},
		{"missing workload", "work", []string{"missing"}},
		{"bad workload", "work", []string{"../../escape"}},
		{"invalid deployment", "../host", []string{}},
		{"duplicate", "work", []string{"dev", "dev"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FromConfigured(cfg, tc.deployment, tc.ids); err == nil {
				t.Fatal("accepted invalid candidate")
			}
		})
	}
	if _, err := FromConfigured(nil, "work", []string{}); err == nil {
		t.Fatal("accepted nil config")
	}
	if _, err := FromConfigured(cfg, "work", []string{}); err != nil {
		t.Fatalf("explicitly empty selection rejected: %v", err)
	}
}

func TestConfiguredProjectionRejectsMalformedWorkloadContainer(t *testing.T) {
	cfg := loadConfigured(t, `{"workloads":{"dev":null}}`)
	if _, err := FromConfigured(cfg, "work", []string{"dev"}); err == nil {
		t.Fatal("accepted null workload")
	}
	cfg = loadConfigured(t, `{"workloads":null}`)
	if _, err := FromConfigured(cfg, "work", []string{}); err == nil {
		t.Fatal("accepted null workloads registry")
	}
}
