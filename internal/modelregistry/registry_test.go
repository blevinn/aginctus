package modelregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/providerregistry"
)

func registeredProvider(t *testing.T, alias string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	providerPath, err := providerregistry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := providerregistry.Register(providerPath, providerregistry.Provider{Type: "chatgpt", Alias: alias}); err != nil {
		t.Fatal(err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRegisterModelPrivateIdempotentAndConflict(t *testing.T) {
	path := registeredProvider(t, "personal")
	wanted := Model{Name: "coding", Provider: "personal", Source: "chatgpt/confirmed-later"}
	changed, err := Register(path, wanted)
	if err != nil || !changed {
		t.Fatalf("first registration changed=%v err=%v", changed, err)
	}
	changed, err = Register(path, wanted)
	if err != nil || changed {
		t.Fatalf("repeat registration changed=%v err=%v", changed, err)
	}
	got, err := Load(path)
	if err != nil || len(got.Models) != 1 || got.Models[0] != wanted {
		t.Fatalf("load: %+v %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("registry file permission: %v %v", info, err)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("registry directory permission: %v %v", info, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), "access_token") || strings.Contains(string(b), "refresh_token") {
		t.Fatalf("model registry contained credentials or was unreadable: %v", err)
	}
	if _, err := Register(path, Model{Name: "coding", Provider: "personal", Source: "chatgpt/another"}); err == nil {
		t.Fatal("accepted conflicting alias")
	}
}

func TestRegisterModelRequiresProviderAndValidSource(t *testing.T) {
	path := registeredProvider(t, "personal")
	for _, m := range []Model{
		{Name: "coding", Provider: "unknown", Source: "chatgpt/gpt"},
		{Name: "../escape", Provider: "personal", Source: "chatgpt/gpt"},
		{Name: "coding", Provider: "personal", Source: "openai/gpt"},
		{Name: "coding", Provider: "personal", Source: "chatgpt/"},
		{Name: "coding", Provider: "personal", Source: "chatgpt/name with spaces"},
	} {
		if _, err := Register(path, m); err == nil {
			t.Fatalf("accepted invalid model %+v", m)
		}
	}
	reg, err := Load(path)
	if err != nil || len(reg.Models) != 0 {
		t.Fatalf("rejected writes changed registry: %+v %v", reg, err)
	}
}

func TestRegistryRejectsInsecureAndConcurrentState(t *testing.T) {
	path := registeredProvider(t, "chatgpt")
	m := Model{Name: "chatgpt/test", Provider: "chatgpt", Source: "chatgpt/test"}
	if err := os.Symlink("target", path); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, m); err == nil {
		t.Fatal("accepted symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, m); err == nil {
		t.Fatal("accepted existing transaction lock")
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, m); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("accepted world-readable registry")
	}
}

func TestRegistryRejectsCorruptedEntries(t *testing.T) {
	path := registeredProvider(t, "chatgpt")
	if err := os.WriteFile(path, []byte("{\"models\":[{\"name\":\"a\",\"provider\":\"chatgpt\",\"source\":\"chatgpt/x\"},{\"name\":\"a\",\"provider\":\"chatgpt\",\"source\":\"chatgpt/y\"}]}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("accepted duplicate persisted model aliases")
	}
}
