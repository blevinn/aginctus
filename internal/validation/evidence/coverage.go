package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/blevinn/aginctus/internal/validation/catalog"
)

var coverageCaseID = regexp.MustCompile("^[A-Za-z0-9_./:-]{1,192}$")

// CoverageSelection contains only explicitly named existing evidence.
// Missing selections are reported as not_run, never discovered or executed.
type CoverageSelection struct {
	HermeticJSON        string
	VMRunID             string
	VMRoot              string
	PilotTopologyJSON   string
	OpenCodeRuntimeJSON string
	AcceptanceRunID     string
	AcceptanceRoot      string
}

func validateFailureCases(fields map[string]json.RawMessage, status string) error {
	cases, exists := fields["failed_cases"]
	truncated, hasTruncated := fields["cases_truncated"]
	if !exists {
		if hasTruncated {
			return errors.New("truncation without cases")
		}
		return nil
	}
	if status != "fail" {
		return errors.New("failure cases on non-failed suite")
	}
	var entries []json.RawMessage
	if json.Unmarshal(cases, &entries) != nil || len(entries) == 0 || len(entries) > 8 {
		return errors.New("invalid failure case count")
	}
	if hasTruncated && (string(truncated) != "true" || len(entries) != 8) {
		return errors.New("invalid case truncation")
	}
	seen := map[string]bool{}
	for _, raw := range entries {
		item, err := object(raw)
		if err != nil || len(item) != 2 {
			return errors.New("invalid failure case envelope")
		}
		id, e1 := getString(item, "id")
		kind, e2 := getString(item, "kind")
		if e1 != nil || e2 != nil || !coverageCaseID.MatchString(id) ||
			(kind != "fail" && kind != "error") || seen[id] {
			return errors.New("invalid failure case identifier")
		}
		seen[id] = true
	}
	return nil
}

// NormalizeHermetic validates the complete v1 schema the independent Python
// CI summary validator accepts. It only returns a status, not child output.
func NormalizeHermetic(payload []byte) (string, error) {
	if len(payload) > maxSelectedBytes {
		return "", errors.New("oversized hermetic evidence")
	}
	if err := uniqueJSON(payload); err != nil {
		return "", err
	}
	fields, err := object(payload)
	if err != nil {
		return "", err
	}
	version, err := getInt(fields, "schema_version")
	status, se := getString(fields, "status")
	if err != nil || version != 1 || se != nil ||
		(status != "pass" && status != "fail") ||
		!requiredString(fields, "profile", "hermetic") {
		return "", errors.New("invalid hermetic envelope")
	}
	var results []json.RawMessage
	suites := catalog.HermeticSuites()
	if json.Unmarshal(fields["results"], &results) != nil || len(results) != len(suites) {
		return "", errors.New("invalid suite count")
	}
	summary := map[string]int{"passed": 0, "failed": 0, "errors": 0}
	for i, raw := range results {
		row, err := object(raw)
		if err != nil || !requiredString(row, "id", suites[i].ID) {
			return "", errors.New("invalid suite identifier")
		}
		value, err := getString(row, "status")
		if err != nil || (value != "pass" && value != "fail" && value != "error") {
			return "", errors.New("invalid suite status")
		}
		duration, err := getInt(row, "duration_ms")
		if err != nil || duration < 0 {
			return "", errors.New("invalid suite duration")
		}
		exit, exists := row["exit_code"]
		if !exists {
			return "", errors.New("missing suite exit")
		}
		if value == "error" {
			if string(exit) != "null" {
				return "", errors.New("error suite has exit code")
			}
		} else {
			code, err := getInt(row, "exit_code")
			if err != nil || (value == "pass") != (code == 0) {
				return "", errors.New("inconsistent suite exit code")
			}
		}
		if err := validateFailureCases(row, value); err != nil {
			return "", err
		}
		summary[map[string]string{"pass": "passed", "fail": "failed", "error": "errors"}[value]]++
	}
	counts, err := object(fields["summary"])
	if err != nil || len(counts) != 3 {
		return "", errors.New("invalid hermetic counts")
	}
	for key, want := range summary {
		observed, err := getInt(counts, key)
		if err != nil || observed != want {
			return "", errors.New("inconsistent hermetic counts")
		}
	}
	if (status == "pass") != (summary["passed"] == len(suites)) {
		return "", errors.New("inconsistent hermetic outcome")
	}
	return status, nil
}

func coverageObserved(name, status, detail string) map[string]any {
	row := map[string]any{"id": name, "status": status, "evidence": "local_report"}
	if detail != "" {
		row["detail"] = detail
	}
	return row
}

func selectedCoverage(name string, selection CoverageSelection) (string, string, error) {
	switch name {
	case "hermetic":
		raw, err := ReadSelectedJSON(selection.HermeticJSON)
		if err != nil {
			return "", "", err
		}
		status, err := NormalizeHermetic(raw)
		return status, "", err
	case "vm":
		report, err := ReadVM(selection.VMRoot, selection.VMRunID)
		if err != nil {
			return "", "", err
		}
		status := report["status"].(string)
		results := report["results"].([]any)
		item := results[0].(map[string]any)
		return status, item["id"].(string), nil
	case "preflight/pilot-topology", "preflight/opencode-runtime":
		path := selection.PilotTopologyJSON
		check := "pilot-topology"
		if name == "preflight/opencode-runtime" {
			path = selection.OpenCodeRuntimeJSON
			check = "opencode-runtime"
		}
		raw, err := ReadSelectedJSON(path)
		if err != nil {
			return "", "", err
		}
		status, err := NormalizePreflight(raw, check)
		return status, "", err
	case "acceptance/local_checks":
		report, err := ReadAcceptance(selection.AcceptanceRoot, selection.AcceptanceRunID)
		if err != nil {
			return "", "", err
		}
		detail := "live_partial"
		if report["source"].(map[string]any)["mode"] == "mock" {
			detail = "mock"
		}
		status := report["status"].(string)
		if status == "incomplete" {
			status = "pass"
		}
		return status, detail, nil
	}
	return "", "", errors.New("unknown coverage source")
}

// NormalizeCoverage is read-only. In particular local acceptance records
// only prove fixture and command observations, NOT a live inference result.
func NormalizeCoverage(selection CoverageSelection) Report {
	sources := []struct {
		id       string
		selected bool
	}{
		{"hermetic", selection.HermeticJSON != ""},
		{"vm", selection.VMRunID != ""},
		{"preflight/pilot-topology", selection.PilotTopologyJSON != ""},
		{"preflight/opencode-runtime", selection.OpenCodeRuntimeJSON != ""},
		{"acceptance/local_checks", selection.AcceptanceRunID != ""},
	}
	rows := make([]any, 0, 9)
	counts := map[string]int{"passed": 0, "failed": 0, "errors": 0, "not_run": 0}
	for _, source := range sources {
		var row map[string]any
		if !source.selected {
			row = map[string]any{"id": source.id, "status": "not_run", "evidence": "none"}
		} else {
			status, detail, err := selectedCoverage(source.id, selection)
			if err != nil {
				row = map[string]any{"id": source.id, "status": "error", "evidence": "invalid"}
			} else {
				row = coverageObserved(source.id, status, detail)
			}
		}
		rows = append(rows, row)
		counts[map[string]string{"pass": "passed", "fail": "failed", "error": "errors", "not_run": "not_run"}[row["status"].(string)]]++
	}
	for _, id := range []string{"live/agent_tools", "live/upstream_inference", "live/credential_egress", "live/cleanup"} {
		rows = append(rows, map[string]any{"id": id, "status": "not_run", "evidence": "none"})
		counts["not_run"]++
	}
	status := "incomplete"
	if counts["errors"] > 0 {
		status = "error"
	} else if counts["failed"] > 0 {
		status = "fail"
	}
	return Report{
		"schema_version": 1, "profile": "coverage", "status": status,
		"summary": counts, "results": rows,
		"upstream_inference": unverified, "gateway_authorization": unverified,
		"egress_policy": unverified, "cleanup": unverified, "complete_acceptance": false,
	}
}

// WriteCoverageText retains the previous fixed human-facing status table,
// with no raw evidence, unknown paths, prompts or sensitive source fields.
func WriteCoverageText(output io.Writer, report Report) error {
	results := report["results"].([]any)
	for _, entry := range results {
		row := entry.(map[string]any)
		detail := ""
		if value, ok := row["detail"].(string); ok {
			detail = "  " + value
		}
		if _, err := fmt.Fprintf(output, "%-10s %s%s\n", strings.ToUpper(row["status"].(string)), row["id"].(string), detail); err != nil {
			return err
		}
	}
	counts := report["summary"].(map[string]int)
	if _, err := fmt.Fprintf(output, "Coverage: %d passed, %d failed, %d errors, %d not run\n",
		counts["passed"], counts["failed"], counts["errors"], counts["not_run"]); err != nil {
		return err
	}
	_, err := fmt.Fprintln(output, "NOT VERIFIED: live acceptance, upstream inference, egress, cleanup")
	return err
}
