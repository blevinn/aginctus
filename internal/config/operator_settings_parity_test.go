package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The same inputs drive both module evaluations and the real Go loader. Nix
// supplies freshly built outputs; plain Go tests retain the loader assertions.
func TestOperatorSettingsParity(t *testing.T) {
	var cases struct {
		Valid   map[string]json.RawMessage `json:"valid"`
		Invalid map[string]json.RawMessage `json:"invalid"`
	}
	readSettingsJSON(t, "testdata/operator-settings.json", &cases)
	var rendered map[string]map[string]string
	if path := os.Getenv("AGINCTUS_OPERATOR_SETTINGS_MANIFEST"); path != "" {
		readSettingsJSON(t, path, &rendered)
		if len(rendered) != len(cases.Valid) {
			t.Fatal("rendered fixture set differs from shared inputs")
		}
	} else {
		t.Log("rendered NixOS/HM comparison requires the operator-settings-parity flake check")
	}
	for name, input := range cases.Valid {
		t.Run(name, func(t *testing.T) {
			inputPath := writeSettingsFixture(t, input)
			baseline, err := (&Loader{}).Load(Options{ConfigurationFile: inputPath})
			if err != nil {
				t.Fatal(err)
			}
			if rendered != nil {
				var system, user map[string]any
				readSettingsJSON(t, rendered[name]["nixos"], &system)
				readSettingsJSON(t, rendered[name]["home"], &user)
				if !reflect.DeepEqual(system, user) {
					t.Fatal("complete NixOS and Home Manager JSON differ")
				}
				combined := &Loader{SystemPath: rendered[name]["nixos"], UserPath: rendered[name]["home"]}
				cfg, err := combined.Load(Options{})
				if err != nil || !reflect.DeepEqual(cfg.Values(), baseline.Values()) {
					t.Fatalf("combined system/user settings differ from Go input: %v", err)
				}
				combined.UserPath = writeSettingsFixture(t, []byte(`{"gateway":{"id":"user"}}`))
				cfg, err = combined.Load(Options{})
				if err != nil {
					t.Fatal(err)
				}
				if id, _ := cfg.String("gateway.id"); id != "user" {
					t.Fatalf("user file did not override generated system file: %q", id)
				}
			}
			for _, surface := range []string{"nixos", "home"} {
				path := inputPath
				if rendered != nil {
					path = rendered[name][surface]
					if path == "" {
						t.Fatalf("missing %s/%s output", name, surface)
					}
				}
				loader := &Loader{}
				if surface == "nixos" {
					loader.SystemPath = path
				} else {
					loader.UserPath = path
				}
				got, err := loader.Load(Options{})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.Values(), baseline.Values()) {
					t.Errorf("%s effective settings differ from Go input: got %#v, want %#v", surface, got.Values(), baseline.Values())
				}
				// Exercise all precedence layers above either generated file.
				loader.Environment = []string{"AGINCTUS_GATEWAY_ID=environment"}
				for _, layer := range []struct {
					options Options
					want    string
				}{
					{Options{}, "environment"},
					{Options{ConfigurationFile: writeSettingsFixture(t, []byte(`{"gateway":{"id":"explicit"}}`))}, "explicit"},
					{Options{ConfigurationFile: inputPath, Overrides: []string{"gateway.id=cli"}}, "cli"},
				} {
					cfg, err := loader.Load(layer.options)
					if err != nil {
						t.Fatal(err)
					}
					if id, _ := cfg.String("gateway.id"); id != layer.want {
						t.Errorf("%s precedence id = %q, want %q", surface, id, layer.want)
					}
				}
			}
		})
	}
	for name, input := range cases.Invalid {
		t.Run(name, func(t *testing.T) {
			_, err := (&Loader{}).Load(Options{ConfigurationFile: writeSettingsFixture(t, input)})
			if err == nil {
				t.Fatal("invalid configuration accepted")
			}
			if strings.Contains(err.Error(), "synthetic-secret-sentinel") {
				t.Fatal("error exposes secret value")
			}
		})
	}
}

func readSettingsJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func writeSettingsFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
