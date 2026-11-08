package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/testenv"
)

func TestBootstrapFingerprintsIsInspectionOnly(t *testing.T) {
	var out, stderr bytes.Buffer
	input := `{"contract":{"pool":{"name":"testenv","driver":"lvm","config":{"source":"reviewed"}}},"inventory":{"complete":true}}`
	if code := execute([]string{"worker", "bootstrap", "fingerprints"}, strings.NewReader(input), &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var result map[string]string
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["status"] != "inspection_only" || len(result["inventory_fingerprint"]) != 64 || len(result["pool_config_fingerprint"]) != 64 {
		t.Fatal(out.String(), err)
	}
}

func TestBootstrapPlanJSONRoundTrip(t *testing.T) {
	in := plannerInput{
		Contract: testenv.IncusBootstrapContract{SchemaVersion: 1, ImageFingerprint: strings.Repeat("a", 64),
			Pool: testenv.WorkerPoolInspection{Name: "testenv", Driver: "lvm", Config: map[string]string{"source": "reviewed-source"}}},
		Inventory: testenv.IncusBootstrapInventory{Complete: true, DedicatedLocal: true, Socket: "/var/lib/incus/unix.socket",
			DefaultProjectEmpty: true, MarkerReady: true, IdentityReady: true, CredentialReady: true,
			Profiles: []testenv.IncusBootstrapProfile{{Project: "default", Name: "default"}}},
	}
	in.Contract.PoolConfigFingerprint, _ = testenv.BootstrapPoolConfigFingerprint(in.Contract.Pool)
	for _, existing := range []bool{false, true} {
		if existing {
			project, _ := testenv.IncusProjectPolicy("testenv")
			in.Inventory.Projects = []testenv.IncusProjectPlan{project}
			in.Inventory.Profiles = append(in.Inventory.Profiles, testenv.IncusBootstrapProfile{Project: testenv.DefaultVMProject, Name: "default"})
			in.Inventory.Pools = []testenv.IncusBootstrapPool{{Metadata: in.Contract.Pool, Status: "Created", Total: 72 * 1024 * 1024 * 1024}}
			in.Inventory.Images = []testenv.WorkerImageInspection{{Fingerprint: in.Contract.ImageFingerprint, Project: testenv.DefaultVMProject, Type: "virtual-machine", Architecture: "x86_64"}}
			in.Contract.ExistingInventoryFingerprint, _ = testenv.BootstrapInventoryFingerprint(in.Inventory)
		}
		data, _ := json.Marshal(in)
		var out, stderr bytes.Buffer
		if code := execute([]string{"worker", "bootstrap", "plan"}, bytes.NewReader(data), &out, &stderr); code != 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
		var plan testenv.IncusBootstrapPlan
		if err := json.Unmarshal(out.Bytes(), &plan); err != nil || (existing && plan.Status != "offline_match") || (!existing && plan.Status != "plan_only") {
			t.Fatal(plan, err)
		}
	}
}

func TestCommandContractsAndInputRedaction(t *testing.T) {
	for _, data := range []string{"", "null", `{}`, `{"unknown":"private-input"}`, `{} {}`, strings.Repeat("x", 256*1024+1)} {
		var out, stderr bytes.Buffer
		if code := execute([]string{"worker", "bootstrap", "plan"}, strings.NewReader(data), &out, &stderr); code == 0 {
			t.Fatal("accepted incomplete or malformed input", data)
		}
		if strings.Contains(out.String()+stderr.String(), "private-input") {
			t.Fatal("echoed input")
		}
	}
	for _, args := range [][]string{{"--help"}, {"worker", "bootstrap", "plan", "extra"}, {"worker", "bootstrap", "apply"}} {
		var out, stderr bytes.Buffer
		code := execute(args, strings.NewReader(""), &out, &stderr)
		if args[0] == "--help" && code != 0 || args[0] != "--help" && code != 2 {
			t.Fatal(args, code, stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := execute([]string{"worker", "bootstrap", "plan"}, strings.NewReader(`{"contract":{},"inventory":{}}`), &out, &stderr); code != 1 {
		t.Fatal("unsafe plan must require operator action", code, stderr.String())
	}
}
