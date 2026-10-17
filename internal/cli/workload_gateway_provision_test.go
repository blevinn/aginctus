package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverGatewayIPv4(t *testing.T) {
	const project = "aginctus-gateway"
	const management = "aginctus-mgmt"
	// Both networks can have private addresses. The management attachment is
	// identified by its Incus device metadata, not by eth0 or IP heuristics.
	good := `[{"name":"litellm-1","status":"Running","config":{"user.aginctus.managed":"true","user.aginctus.resource":"compose-instance","user.aginctus.deployment":"aginctus-gateway"},"expanded_devices":{"eth0":{"type":"nic","name":"eth0","network":"aginctus-mgmt"},"eth1":{"type":"nic","name":"eth1","network":"aginctus-egress"}},"state":{"network":{"eth0":{"addresses":[{"family":"inet","address":"10.42.40.12","scope":"global"},{"family":"inet6","address":"fe80::2"}]},"eth1":{"addresses":[{"family":"inet","address":"10.77.0.20","scope":"global"}]}}}}]`
	ip, err := discoverGatewayIPv4([]byte(good), project, management)
	if err != nil || ip != "10.42.40.12" {
		t.Fatalf("dual-NIC management address: got %q, %v", ip, err)
	}
	// Interface indices are not a security boundary; the egress attachment may
	// get eth0 instead of the management attachment.
	swapped := strings.NewReplacer(
		`"name":"eth0","network":"aginctus-mgmt"`, `"name":"eth1","network":"aginctus-mgmt"`,
		`"name":"eth1","network":"aginctus-egress"`, `"name":"eth0","network":"aginctus-egress"`,
	).Replace(good)
	ip, err = discoverGatewayIPv4([]byte(swapped), project, management)
	if err != nil || ip != "10.77.0.20" {
		t.Fatalf("management on eth1: got %q, %v", ip, err)
	}

	for _, tt := range []struct {
		name       string
		inventory  string
		network    string
	}{
		{"stopped", strings.Replace(good, `"Running"`, `"Stopped"`, 1), management},
		{"unowned", strings.Replace(good, `"user.aginctus.managed":"true"`, `"user.aginctus.managed":"false"`, 1), management},
		{"wrong project", strings.Replace(good, `"user.aginctus.deployment":"aginctus-gateway"`, `"user.aginctus.deployment":"other"`, 1), management},
		{"public management address", strings.Replace(good, `"10.42.40.12"`, `"8.8.8.8"`, 1), management},
		{"loopback management address", strings.Replace(good, `"10.42.40.12"`, `"127.0.0.1"`, 1), management},
		{"multiple management addresses", strings.Replace(good, `"address":"10.42.40.12","scope":"global"`, `"address":"10.42.40.12","scope":"global"},{"family":"inet","address":"10.42.40.13","scope":"global"`, 1), management},
		{"no matching NIC", strings.Replace(good, `"network":"aginctus-mgmt"`, `"network":"other"`, 1), management},
		{"missing guest interface", strings.Replace(good, `"name":"eth0","network":"aginctus-mgmt"`, `"name":"eth9","network":"aginctus-mgmt"`, 1), management},
		{"unnamed NIC", strings.Replace(good, `"name":"eth0","network":"aginctus-mgmt"`, `"network":"aginctus-mgmt"`, 1), management},
		{"two management NICs", strings.Replace(good, `"network":"aginctus-egress"`, `"network":"aginctus-mgmt"`, 1), management},
		{"no expanded device metadata", strings.Replace(good, `"expanded_devices"`, `"unavailable_devices"`, 1), management},
		{"missing configured management network", good, ""},
		{"empty inventory", `[]`, management},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if ip, err := discoverGatewayIPv4([]byte(tt.inventory), project, tt.network); err == nil {
				t.Fatalf("expected failure, got %q", ip)
			}
		})
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
