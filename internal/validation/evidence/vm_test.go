package evidence

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(runID string) []byte {
	record := map[string]any{
		"schema_version": 1, "run_id": runID, "scenario": "smoke",
		"status": "pass", "exit_code": 0, "failure_stage": nil,
		"created_utc":  "2026-11-08T00:00:00+00:00",
		"git_revision": strings.Repeat("a", 40), "git_dirty": false,
		"flake_lock_sha256": strings.Repeat("b", 64),
		"verification_tier": vmTier, "upstream_inference": unverified,
		"cleanup": unverified, "secret_canary": "NEVER_COPY_SECRETS",
	}
	result, _ := json.Marshal(record)
	return result
}

func TestNormalizeVMKeepsOnlyBoundedSyntheticMetadata(t *testing.T) {
	id := strings.Repeat("a", 24)
	report, err := NormalizeVM(fixture(id), id)
	if err != nil || report["status"] != "pass" {
		t.Fatalf("unexpected VM normalization %v: %#v", err, report)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "NEVER_COPY_SECRETS") ||
		strings.Contains(string(encoded), "live_verified") {
		t.Fatal("untrusted metadata escaped normalizer")
	}
	var source map[string]any = report["source"].(map[string]any)
	if source["run_id"] != id || report["upstream_inference"] != unverified {
		t.Fatal("synthetic provenance was promoted")
	}
}

func TestVMReportWritersEmitNormalizedOutput(t *testing.T) {
	id := strings.Repeat("e", 24)
	report, err := NormalizeVM(fixture(id), id)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := WriteJSON(&encoded, report); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil || decoded["status"] != "pass" {
		t.Fatalf("invalid JSON report %q: %v", encoded.String(), err)
	}
	var text bytes.Buffer
	if err := WriteVMText(&text, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "pass  vm/smoke") || !strings.Contains(text.String(), "VM evidence: pass") {
		t.Fatalf("unexpected text report %q", text.String())
	}
	if strings.Contains(text.String(), "NEVER_COPY_SECRETS") {
		t.Fatal("text report leaked untrusted receipt metadata")
	}
}

func TestNormalizeVMFailsClosedOnInvalidClaims(t *testing.T) {
	id := strings.Repeat("b", 24)
	cases := []map[string]any{
		{"schema_version": 2},
		{"status": "fail"},
		{"exit_code": true},
		{"exit_code": "0"},
		{"schema_version": "1"},
		{"scenario": "native-oci"},
		{"scenario": "../foreign"},
		{"created_utc": "2026-11-08T00:00:00-07:00"},
		{"cleanup": "verified"},
		{"upstream_inference": "verified"},
		{"verification_tier": "live"},
		{"git_dirty": "false"},
		{"git_revision": "not-a-hash"},
		{"failure_stage": "runtime"},
	}
	for _, changes := range cases {
		var source map[string]any
		_ = json.Unmarshal(fixture(id), &source)
		for key, value := range changes {
			source[key] = value
		}
		raw, _ := json.Marshal(source)
		if report, err := NormalizeVM(raw, id); err == nil {
			t.Fatalf("accepted invalid receipt %#v: %#v", changes, report)
		}
	}
	if _, err := NormalizeVM(fixture(id), strings.Repeat("c", 24)); err == nil {
		t.Fatal("accepted mismatched run selector")
	}
}

func TestPrivateVMReadRefusesSymlinksAndPublicFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owned")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("c", 24)
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "result.json")
	if err := os.WriteFile(file, fixture(id), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVM(root, id); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVM(root, id); err == nil {
		t.Fatal("accepted publicly readable evidence")
	}
	_ = os.Chmod(file, 0600)
	alias := filepath.Join(filepath.Dir(root), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVM(alias, id); err == nil {
		t.Fatal("followed symlinked evidence root")
	}
	if _, err := ReadVM(root, "../"+id); err == nil {
		t.Fatal("accepted traversing run selector")
	}
	if err := os.Rename(file, file+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file+".old", file); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVM(root, id); err == nil {
		t.Fatal("followed symlinked result file")
	}
}

func TestFullStackCannotFabricatePass(t *testing.T) {
	id := strings.Repeat("d", 24)
	var source map[string]any
	_ = json.Unmarshal(fixture(id), &source)
	source["scenario"] = "full-stack"
	source["full_stack"] = map[string]any{"schema_version": 1, "source": "guest_observation_not_attestation", "phases": map[string]string{"preflight": "pass"}, "gates": map[string]string{}}
	bad, _ := json.Marshal(source)
	if _, err := NormalizeVM(bad, id); err == nil {
		t.Fatal("accepted incomplete full-stack observations")
	}
	if good := Unavailable(); good["status"] != "error" || good["upstream_inference"] != unverified {
		t.Fatal("unavailable receipt was treated as proof")
	}
}
