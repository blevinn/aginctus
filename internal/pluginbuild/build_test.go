package pluginbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/plugin"
)

const builtinPath = "github.com/blevinn/aginctus/internal/pluginbuild/testdata/builtin"
const externalPath = "example.com/aginctus-external-plugin"

// Build real application binaries through temporary overlays/modfiles. Never
// edit the checkout, download modules, enable workspace/toolchain discovery or
// call host services. The external module is pinned to a local synthetic fixture.
func TestSelectionBuilds(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	originalMod, originalSum := read(filepath.Join(root, "go.mod")), read(filepath.Join(root, "go.sum"))
	originalGenerated := read(filepath.Join(root, "internal/appplugins/generated.go"))
	asset := bytes.TrimSpace(read(filepath.Join(root, "internal/pluginbuild/testdata/builtin/capability.txt")))
	for _, tc := range []struct {
		name      string
		selection []Selection
		want      []string
		failure   string
	}{
		{name: "default"},
		{name: "minimal", selection: []Selection{}},
		{name: "builtin", selection: []Selection{{builtinPath, "New"}}, want: []string{"a-builtin"}},
		{name: "custom-external", selection: []Selection{{builtinPath, "NewDependent"}, {externalPath, "New"}}, want: []string{"external-example", "a-builtin"}},
		{name: "duplicate", selection: []Selection{{builtinPath, "New"}, {builtinPath, "NewDuplicate"}}, failure: "duplicate plugin identity"},
		{name: "missing", selection: []Selection{{builtinPath, "NewDependent"}}, failure: "missing dependency"},
		{name: "incompatible", selection: []Selection{{builtinPath, "NewIncompatible"}}, failure: "unsupported plugin API version"},
		{name: "cycle", selection: []Selection{{builtinPath, "NewCycleA"}, {builtinPath, "NewCycleB"}}, failure: "cycle"},
		{name: "bad-factory", selection: []Selection{{builtinPath, "NotAFactory"}}, failure: "undefined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name string, raw []byte) string {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				return path
			}
			mod := append([]byte{}, originalMod...)
			mod = append(mod, []byte("\nrequire "+externalPath+" v1.0.0\nreplace "+externalPath+" => "+filepath.ToSlash(filepath.Join(root, "pkg/plugin/testdata/external"))+"\n")...)
			modfile := write("selection.mod", mod)
			sumfile := write("selection.sum", originalSum)
			defer func() {
				if !bytes.Equal(read(modfile), mod) || !bytes.Equal(read(sumfile), originalSum) {
					t.Fatal("build edited pinned module inputs")
				}
			}()
			flags := []string{"-modfile=" + modfile}
			if tc.name != "default" {
				raw, _ := json.Marshal(Manifest{APIVersion: APIVersion, Package: "appplugins", Plugins: tc.selection})
				source, err := Generate(raw)
				if err != nil {
					t.Fatal(err)
				}
				generated := write("generated.go", source)
				overlay, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(root, "internal/appplugins/generated.go"): generated}})
				flags = append(flags, "-overlay="+write("overlay.json", overlay))
			}
			runGo := func(verb string, args ...string) ([]byte, error) {
				t.Helper()
				all := append([]string{verb}, flags...)
				cmd := exec.CommandContext(ctx, "go", append(all, args...)...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
				return cmd.CombinedOutput()
			}
			binary := filepath.Join(dir, "aginctus")
			buildArgs := []string{"-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", binary, "./cmd/aginctus"}
			output, err := runGo("build", buildArgs...)
			if tc.name == "bad-factory" {
				if err == nil || !bytes.Contains(output, []byte(tc.failure)) {
					t.Fatalf("bad factory compiled: %v\n%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("build: %v\n%s", err, output)
			}
			cmd := exec.CommandContext(ctx, binary, "--configuration-file", "/must/not/be/read", "plugins", "inventory")
			output, err = cmd.CombinedOutput()
			if tc.failure != "" {
				if err == nil || !bytes.Contains(output, []byte(tc.failure)) {
					t.Fatalf("invalid composition: %v\n%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("inventory: %v\n%s", err, output)
			}
			var inventory plugins.CompiledInventory
			if err := json.Unmarshal(output, &inventory); err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, entry := range inventory.Plugins {
				if entry.Version != "fixture-1" || entry.APIVersion != plugin.APIVersion || len(entry.Capabilities) == 0 {
					t.Fatalf("compiled identity/capabilities lost: %+v", entry)
				}
				names = append(names, entry.Name)
			}
			if inventory.SchemaVersion != plugins.InventorySchemaVersion || !slices.Equal(names, tc.want) {
				t.Fatalf("inventory: %s", output)
			}
			deps, err := runGo("list", "-deps", "-f", "{{.ImportPath}}", "./cmd/aginctus")
			if err != nil {
				t.Fatalf("dependency closure: %v\n%s", err, deps)
			}
			hasBuiltin, hasExternal := slices.Contains(names, "a-builtin"), slices.Contains(names, "external-example")
			closure := strings.Fields(string(deps))
			if slices.Contains(closure, builtinPath) != hasBuiltin || slices.Contains(closure, externalPath) != hasExternal {
				t.Fatalf("selection/closure mismatch: builtin=%v external=%v", hasBuiltin, hasExternal)
			}
			if slices.Contains(closure, "github.com/blevinn/aginctus/internal/pluginbuild") {
				t.Fatal("generator leaked into application")
			}
			binaryBytes := read(binary)
			if bytes.Contains(binaryBytes, asset) != hasBuiltin {
				t.Fatal("embedded fixture asset inclusion/omission mismatch")
			}
			if tc.name == "custom-external" {
				if len(inventory.Plugins[1].Dependencies) != 1 || inventory.Plugins[1].Dependencies[0] != "external-example" {
					t.Fatal("compiled dependency lost")
				}
				moduleInfo, err := exec.CommandContext(ctx, "go", "version", "-m", binary).CombinedOutput()
				if err != nil || !bytes.Contains(moduleInfo, []byte(externalPath+"\tv1.0.0")) {
					t.Fatalf("missing pinned external identity: %v\n%s", err, moduleInfo)
				}
				if output, err := runGo("build", buildArgs...); err != nil {
					t.Fatalf("repeat build: %v\n%s", err, output)
				}
				if sha256.Sum256(read(binary)) != sha256.Sum256(binaryBytes) {
					t.Fatal("identical pinned inputs produced different executable bytes")
				}
			}
		})
	}
	if !bytes.Equal(read(filepath.Join(root, "go.mod")), originalMod) || !bytes.Equal(read(filepath.Join(root, "go.sum")), originalSum) || !bytes.Equal(read(filepath.Join(root, "internal/appplugins/generated.go")), originalGenerated) {
		t.Fatal("build fixture changed checkout inputs")
	}
}
