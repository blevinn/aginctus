package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/validation/catalog"
)

func hermeticFixture() []byte {
	var rows []map[string]any
	for _, suite := range catalog.HermeticSuites() {
		rows = append(rows, map[string]any{
			"id": suite.ID, "status": "pass", "duration_ms": 1,
			"exit_code": 0, "rerun": "just check " + suite.Recipe,
		})
	}
	raw, _ := json.Marshal(map[string]any{
		"schema_version": 1, "profile": "hermetic", "status": "pass",
		"summary": map[string]int{"passed": len(rows), "failed": 0, "errors": 0},
		"results": rows,
	})
	return raw
}

func TestHermeticParityAndHostileEvidence(t *testing.T) {
	if status, err := NormalizeHermetic(hermeticFixture()); err != nil || status != "pass" {
		t.Fatalf("valid hermetic report rejected: %v", err)
	}
	var source map[string]any
	if err := json.Unmarshal(hermeticFixture(), &source); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []map[string]any{
		{"schema_version": true},
		{"status": "fail"},
		{"summary": map[string]int{"passed": 7, "failed": 1, "errors": 0}},
		{"results": []any{}},
	} {
		copy := map[string]any{}
		for key, value := range source {
			copy[key] = value
		}
		for key, value := range changed {
			copy[key] = value
		}
		raw, _ := json.Marshal(copy)
		if _, err := NormalizeHermetic(raw); err == nil {
			t.Fatalf("accepted invalid hermetic %v", changed)
		}
	}
}

func TestHermeticRejectsAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(string(hermeticFixture()), `"schema_version":1`, `"schema_version":2,"schema_version":1`, 1),
		strings.Replace(string(hermeticFixture()), `"exit_code":0`, `"exit_code":1,"exit_code":0`, 1),
	} {
		if _, err := NormalizeHermetic([]byte(raw)); err == nil {
			t.Fatal("accepted ambiguous hermetic evidence")
		}
	}
}

func TestCoveragePreservesMissingAndUnverifiedClaims(t *testing.T) {
	empty := NormalizeCoverage(CoverageSelection{})
	if empty["status"] != "incomplete" || empty["complete_acceptance"] != false {
		t.Fatalf("missing coverage falsely complete: %#v", empty)
	}
	rows := empty["results"].([]any)
	if len(rows) != 9 {
		t.Fatalf("missing coverage identities: %#v", rows)
	}
	for _, value := range rows {
		row := value.(map[string]any)
		if row["status"] != "not_run" || row["evidence"] != "none" {
			t.Fatalf("missing evidence promoted: %#v", row)
		}
	}
	path := filepath.Join(t.TempDir(), "hermetic.json")
	if err := os.WriteFile(path, hermeticFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	covered := NormalizeCoverage(CoverageSelection{HermeticJSON: path})
	results := covered["results"].([]any)
	if results[0].(map[string]any)["status"] != "pass" ||
		results[8].(map[string]any)["status"] != "not_run" ||
		covered["complete_acceptance"] != false {
		t.Fatalf("hermetic coverage claimed live proof: %#v", covered)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	missing := NormalizeCoverage(CoverageSelection{HermeticJSON: path})
	if missing["status"] != "error" ||
		missing["results"].([]any)[0].(map[string]any)["evidence"] != "invalid" {
		t.Fatalf("missing selected source was treated as success: %#v", missing)
	}
}

func TestCoverageLocalAcceptanceCannotBecomeIndependentProof(t *testing.T) {
	root := filepath.Join(t.TempDir(), "receipts")
	if err := os.MkdirAll(filepath.Join(root, acceptanceRun), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, acceptanceRun, "result.json"), acceptanceFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	report := NormalizeCoverage(CoverageSelection{AcceptanceRoot: root, AcceptanceRunID: acceptanceRun})
	row := report["results"].([]any)[4].(map[string]any)
	if row["status"] != "pass" || row["detail"] != "mock" ||
		report["complete_acceptance"] != false || report["upstream_inference"] != unverified {
		t.Fatalf("partial acceptance was promoted: %#v", report)
	}
	if strings.Contains(strings.ToLower(row["detail"].(string)), "verified") {
		t.Fatal("fabricated acceptance evidence")
	}
}
