package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/testenv"
)

func TestOfflinePlannerInputAndRedaction(t *testing.T) {
	for _, data := range []string{"", "null", `{}`, `{"unknown":"private-input"}`, `{} {}`, strings.Repeat("x", 256*1024+1)} {
		var out, stderr bytes.Buffer
		if code := run(nil, strings.NewReader(data), &out, &stderr); code == 0 {
			t.Fatal("accepted incomplete or malformed input", data)
		}
		if strings.Contains(out.String()+stderr.String(), "private-input") {
			t.Fatal("echoed input")
		}
	}
	var out, stderr bytes.Buffer
	if run([]string{"--fingerprints"}, strings.NewReader(`{"inventory":{"complete":true}}`), &out, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	var result map[string]string
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result["status"] != "inspection_only" || len(result["inventory_fingerprint"]) != 64 {
		t.Fatal(out.String(), err)
	}
	for _, args := range [][]string{{"--help"}, {"--socket", "/tmp/other"}, {"apply"}} {
		out.Reset()
		stderr.Reset()
		code := run(args, strings.NewReader(""), &out, &stderr)
		if args[0] == "--help" && code != 0 || args[0] != "--help" && code != 2 {
			t.Fatal(args, code)
		}
	}
}

func TestOfflinePlannerJSONRoundTrip(t *testing.T) {
	in := input{
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
		if code := run(nil, bytes.NewReader(data), &out, &stderr); code != 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
		var plan testenv.IncusBootstrapPlan
		if err := json.Unmarshal(out.Bytes(), &plan); err != nil || (existing && plan.Status != "offline_match") || (!existing && plan.Status != "plan_only") {
			t.Fatal(plan, err)
		}
	}
}
