package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverGatewayIPv4(t *testing.T) {
	project := "aginctus-gateway"
	good := `[{"name":"litellm-1","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"state":{"network":{"eth0":{"addresses":[{"family":"inet","address":"10.42.40.12","scope":"global"},{"family":"inet6","address":"fe80::2"}]}}}}]`
	ip, err := discoverGatewayIPv4([]byte(good), project)
	if err != nil || ip != "10.42.40.12" {
		t.Fatalf("got %q %v", ip, err)
	}
	for _, bad := range []string{
		strings.Replace(good, `"Running"`, `"Stopped"`, 1),
		strings.Replace(good, `"user.aginctus.managed":"true"`, `"user.aginctus.managed":"false"`, 1),
		strings.Replace(good, `"10.42.40.12"`, `"8.8.8.8"`, 1),
		strings.Replace(good, `"10.42.40.12"`, `"127.0.0.1"`, 1),
		strings.Replace(good, `"10.42.40.12"`, `"10.42.40.12"},{"family":"inet","address":"10.42.40.13"`, 1),
		`[]`,
	} {
		if _, err := discoverGatewayIPv4([]byte(bad), project); err == nil {
			t.Errorf("expected fail closed for %s", bad)
		}
	}
}

func TestReadWorkloadKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("fake-test-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := readWorkloadKey(path)
	if err != nil || value != "fake-test-key" {
		t.Fatalf("got %q %v", value, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorkloadKey(path); err == nil {
		t.Fatal("accepted world readable key")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorkloadKey(path); err == nil {
		t.Fatal("accepted symlink")
	}
}
