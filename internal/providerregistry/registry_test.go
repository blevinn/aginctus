package providerregistry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
)

func TestRegisterIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry", "providers.json")
	p := Provider{Type: "chatgpt", Alias: "personal"}
	first, err := Register(path, p)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	again, err := Register(path, p)
	if err != nil || again {
		t.Fatalf("again=%v err=%v", again, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "\"personal\"") != 1 {
		t.Fatal(string(b))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file perms: %v, %v", info, err)
	}
}
func TestRejectInsecureOrUnsupported(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "providers.json")
	if _, err := Register(path, Provider{Type: "anthropic", Alias: "one"}); err == nil {
		t.Fatal("unsupported provider")
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "../bad"}); err == nil {
		t.Fatal("unsafe alias")
	}
	if err := os.Symlink("other", path); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "chatgpt"}); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestRegistryLockFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	lock, err := filestate.LockRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "chatgpt"}); !errors.Is(err, filestate.ErrRegistryBusy) {
		t.Fatalf("concurrent registration did not fail busy: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if changed, err := Register(path, Provider{Type: "chatgpt", Alias: "chatgpt"}); err != nil || !changed {
		t.Fatalf("registration failed after releasing lock: changed=%t err=%v", changed, err)
	}
}

func TestLoadPrivateRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry", "providers.json")
	reg, err := Load(path)
	if err != nil || len(reg.Providers) != 0 {
		t.Fatalf("missing: %#v %v", reg, err)
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "personal"}); err != nil {
		t.Fatal(err)
	}
	reg, err = Load(path)
	if err != nil || len(reg.Providers) != 1 || reg.Providers[0].Alias != "personal" {
		t.Fatalf("read: %#v %v", reg, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("insecure file accepted")
	}
}
func TestProviderRegistryReadRejectsUnsafeParentAndOversize(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "registry")
	path := filepath.Join(dir, "providers.json")
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "personal"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("load accepted unsafe provider state directory")
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "another"}); err == nil {
		t.Fatal("register accepted unsafe provider state directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A bounded read protects registry parsing even when a private file
	// contains unexpectedly large data.
	if err := os.WriteFile(path, make([]byte, (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("load accepted oversized registry")
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "another"}); err == nil {
		t.Fatal("register accepted oversized registry")
	}
}

func TestLoadRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if err := os.Symlink("missing", path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("symlink accepted")
	}
}
