package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const acceptanceRun = "aginctus-acceptance.ABCD1234"

func acceptanceFixture() []byte {
	rows := []map[string]string{
		{"id": "fixture_change", "source": "host_fixture_check", "status": "pass"},
		{"id": "workspace_tests", "source": "host_go_test", "status": "pass"},
	}
	for _, id := range []string{"agent_tools", "upstream_inference", "credential_egress", "cleanup"} {
		rows = append(rows, map[string]string{"id": id, "source": "not_independently_observed", "status": "not_run"})
	}
	raw, _ := json.Marshal(map[string]any{
		"schema_version": 1, "profile": "local_acceptance", "run_id": acceptanceRun,
		"scenario": "opencode", "mode": "mock", "status": "incomplete",
		"harness_exit_code": 0, "created_utc": "2026-11-08T22:00:00+00:00",
		"verification_tier": "mock_orchestration", "results": rows,
		"cleanup_commands": map[string]string{"stop": "pass", "close": "pass"},
		"upstream_inference": unverified, "credential_egress": unverified,
		"cleanup": unverified, "complete_acceptance": false,
	})
	return raw
}

func TestAcceptanceNormalizationIsConservative(t *testing.T) {
	report, err := NormalizeAcceptance(acceptanceFixture(), acceptanceRun)
	if err != nil || report["status"] != "incomplete" || report["complete_acceptance"] != false {
		t.Fatalf("invalid normalization: %v %#v", err, report)
	}
	counts := report["summary"].(map[string]int)
	if counts["passed"] != 4 || counts["not_run"] != 1 {
		t.Fatalf("missing host-only counts: %#v", counts)
	}
	var record map[string]any
	if err := json.Unmarshal(acceptanceFixture(), &record); err != nil {
		t.Fatal(err)
	}
	for _, change := range []map[string]any{
		{"schema_version": true},
		{"upstream_inference": "verified"},
		{"mode": "live"},
		{"complete_acceptance": true},
		{"harness_exit_code": "0"},
		{"status": "pass"},
		{"cleanup_commands": map[string]string{"stop": "not_run", "close": "pass"}},
		{"created_utc": "2026-11-08T22:00:00-07:00"},
	} {
		changed := map[string]any{}
		for key, value := range record {
			changed[key] = value
		}
		for key, value := range change {
			changed[key] = value
		}
		raw, _ := json.Marshal(changed)
		if report, err := NormalizeAcceptance(raw, acceptanceRun); err == nil {
			t.Fatalf("accepted %v: %#v", change, report)
		}
	}
	if _, err := NormalizeAcceptance([]byte(`{"schema_version":1,"schema_version":1}`), acceptanceRun); err == nil {
		t.Fatal("accepted duplicate evidence keys")
	}
	if _, err := NormalizeAcceptance(append(acceptanceFixture(), []byte(" {}")...), acceptanceRun); err == nil {
		t.Fatal("accepted trailing evidence objects")
	}
}

func TestAcceptancePrivateReaderRejectsAliasesAndPublicFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(root, acceptanceRun)
	if err := os.Mkdir(run, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(run, "result.json")
	if err := os.WriteFile(path, acceptanceFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAcceptance(root, acceptanceRun); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAcceptance(root, acceptanceRun); err == nil {
		t.Fatal("accepted public result")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(root), "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAcceptance(link, acceptanceRun); err == nil {
		t.Fatal("followed symlink root")
	}
	if _, err := ReadAcceptance(root, "../escape"); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".old", path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAcceptance(root, acceptanceRun); err == nil {
		t.Fatal("followed evidence symlink")
	}
}

func TestPreflightValidationCannotPromoteProof(t *testing.T) {
	base := map[string]any{
		"schema_version": 1, "profile": "pilot_preflight", "status": "pass",
		"summary": map[string]int{"passed": 1, "failed": 0, "errors": 0},
		"verification_tier": "read_only_pilot_preflight",
		"upstream_inference": unverified, "gateway_authorization": unverified,
		"egress_policy": unverified, "target": "configured_pilot",
		"results": []map[string]any{{"id": "preflight/pilot-topology", "status": "pass", "exit_code": 0}},
	}
	check := func(fields map[string]any) (string, error) {
		raw, _ := json.Marshal(fields)
		return NormalizePreflight(raw, "pilot-topology")
	}
	if status, err := check(base); err != nil || status != "pass" {
		t.Fatalf("valid preflight rejected: %s %v", status, err)
	}
	for _, change := range []map[string]any{
		{"schema_version": true},
		{"upstream_inference": "verified"},
		{"status": "fail"},
		{"target": "../secret"},
		{"summary": map[string]int{"passed": 0, "failed": 1, "errors": 0}},
	} {
		changed := map[string]any{}
		for key, value := range base {
			changed[key] = value
		}
		for key, value := range change {
			changed[key] = value
		}
		if status, err := check(changed); err == nil {
			t.Fatalf("accepted invalid preflight %s: %v", status, change)
		}
	}
	if strings.Contains(string(acceptanceFixture()), "SENSITIVE_CANARY") {
		t.Fatal("invalid fixture")
	}
}
