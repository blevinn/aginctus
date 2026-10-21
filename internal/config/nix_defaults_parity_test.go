package config

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// cliOnlyDefaults are Go fallbacks with no NixOS module option. The module's
// freeform JSON layer lets an operator set either key, and the CLI supplies the
// fallback when it is absent, so a divergence here changes behavior only for
// hosts that never set the key.
var cliOnlyDefaults = map[string]any{
	"gateway.credentials.storage.pool": "default",
	"gateway.egress.network.name":      "aginctus-egress",
}

// The NixOS module and this defaults map both claim to describe the same
// effective configuration. TestDefaultsMatchNixOSModule fails when they stop
// agreeing, so neither side can be changed alone.
func TestDefaultsMatchNixOSModule(t *testing.T) {
	rendered := flatten(readRenderedModuleDefaults(t))
	for key, want := range rendered {
		got, ok := lookup(defaults, key)
		if !ok {
			t.Errorf("NixOS module default %q has no CLI default", key)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("default %q = %#v in the CLI, %#v in the NixOS module", key, got, want)
		}
	}
	for _, key := range sortedKeys(flatten(defaults)) {
		if _, ok := rendered[key]; ok {
			continue
		}
		want, ok := cliOnlyDefaults[key]
		if !ok {
			t.Errorf("CLI default %q is not rendered by the NixOS module and is not a documented CLI-only fallback", key)
			continue
		}
		if got, _ := lookup(defaults, key); !reflect.DeepEqual(got, want) {
			t.Errorf("CLI-only fallback %q = %#v, want %#v", key, got, want)
		}
	}
	for key := range cliOnlyDefaults {
		if _, ok := lookup(defaults, key); !ok {
			t.Errorf("documented CLI-only fallback %q is no longer a CLI default", key)
		}
	}
}

// readRenderedModuleDefaults prefers the module output supplied by the
// gateway-config-parity flake check and otherwise falls back to the committed
// snapshot, so plain `go test` proves the same contract without Nix.
func readRenderedModuleDefaults(t *testing.T) map[string]any {
	t.Helper()
	path := "testdata/nixos-module-defaults.json"
	if generated := os.Getenv("AGINCTUS_NIXOS_MODULE_DEFAULTS"); generated != "" {
		path = generated
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rendered map[string]any
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return rendered
}

func flatten(values map[string]any) map[string]any {
	flat := map[string]any{}
	var walk func(prefix string, current map[string]any)
	walk = func(prefix string, current map[string]any) {
		for key, value := range current {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if nested, ok := value.(map[string]any); ok {
				walk(path, nested)
				continue
			}
			flat[path] = value
		}
	}
	walk("", values)
	return flat
}

func lookup(values map[string]any, path string) (any, bool) {
	current := values
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		value, ok := current[segment]
		if !ok {
			return nil, false
		}
		if i == len(segments)-1 {
			return value, true
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		current = nested
	}
	return nil, false
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}