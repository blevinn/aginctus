package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	maxAcceptanceBytes = 16 * 1024
	maxSelectedBytes   = 64 * 1024
)

var (
	acceptanceID = regexp.MustCompile("^[a-zA-Z0-9]{8}$")
	preflightTarget = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$")
)

// uniqueJSON checks nested objects as well as the top-level envelope; the
// Python v1 acceptance reader rejects duplicate fields at every level.
func uniqueJSON(payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON nesting exceeded")
		}
		value, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := value.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate or invalid JSON key")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("extra JSON data")
	}
	return nil
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid evidence object")
	}
	return fields, nil
}

func requiredString(fields map[string]json.RawMessage, name, value string) bool {
	got, err := getString(fields, name)
	return err == nil && got == value
}

func statusValue(value string) bool {
	return value == "pass" || value == "fail" || value == "not_run"
}

func utcValue(source map[string]json.RawMessage) (string, error) {
	created, err := getString(source, "created_utc")
	if err != nil {
		return "", err
	}
	date, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return "", errors.New("invalid UTC timestamp")
	}
	_, offset := date.Zone()
	if offset != 0 {
		return "", errors.New("non-UTC timestamp")
	}
	// Validate the source string, but never invent a timestamp for the run.
	return created, nil
}

// NormalizeAcceptance is a pure, allowlisted adapter for the existing local
// acceptance v1 receipt. It NEVER turns partial host checks into live proof.
func NormalizeAcceptance(payload []byte, runID string) (Report, error) {
	if !strings.HasPrefix(runID, "aginctus-acceptance.") ||
		!acceptanceID.MatchString(strings.TrimPrefix(runID, "aginctus-acceptance.")) ||
		len(payload) > maxAcceptanceBytes {
		return nil, errors.New("invalid acceptance selector")
	}
	if err := uniqueJSON(payload); err != nil {
		return nil, err
	}
	fields, err := object(payload)
	if err != nil {
		return nil, err
	}
	version, vErr := getInt(fields, "schema_version")
	code, cErr := getInt(fields, "harness_exit_code")
	mode, mErr := getString(fields, "mode")
	status, sErr := getString(fields, "status")
	created, tErr := utcValue(fields)
	if vErr != nil || version != 1 || cErr != nil || code < 0 || code > 255 ||
		mErr != nil || (mode != "mock" && mode != "live") ||
		sErr != nil || (status != "incomplete" && status != "fail" && status != "error") ||
		tErr != nil || !requiredString(fields, "profile", "local_acceptance") ||
		!requiredString(fields, "run_id", runID) ||
		!requiredString(fields, "scenario", "opencode") ||
		!requiredString(fields, "verification_tier", map[string]string{"mock": "mock_orchestration", "live": "local_live_partial"}[mode]) ||
		!requiredString(fields, "upstream_inference", unverified) ||
		!requiredString(fields, "credential_egress", unverified) ||
		!requiredString(fields, "cleanup", unverified) ||
		string(fields["complete_acceptance"]) != "false" {
		return nil, errors.New("invalid acceptance envelope")
	}

	var entries []json.RawMessage
	if json.Unmarshal(fields["results"], &entries) != nil || len(entries) != 6 {
		return nil, errors.New("invalid acceptance results")
	}
	ids := []string{"fixture_change", "workspace_tests", "agent_tools", "upstream_inference", "credential_egress", "cleanup"}
	sources := []string{"host_fixture_check", "host_go_test", "not_independently_observed", "not_independently_observed", "not_independently_observed", "not_independently_observed"}
	values := make([]string, 0, len(ids))
	for i, raw := range entries {
		item, err := object(raw)
		if err != nil || !requiredString(item, "id", ids[i]) ||
			!requiredString(item, "source", sources[i]) {
			return nil, errors.New("invalid acceptance result source")
		}
		value, err := getString(item, "status")
		if err != nil || !statusValue(value) || (i >= 2 && value != "not_run") {
			return nil, errors.New("invalid acceptance result status")
		}
		values = append(values, value)
	}
	var commands map[string]json.RawMessage
	if json.Unmarshal(fields["cleanup_commands"], &commands) != nil || len(commands) != 2 {
		return nil, errors.New("invalid cleanup commands")
	}
	stop, se := getString(commands, "stop")
	closeState, ce := getString(commands, "close")
	if se != nil || ce != nil || !statusValue(stop) || !statusValue(closeState) {
		return nil, errors.New("invalid cleanup status")
	}
	removed := "not_run"
	if _, ok := fields["local_workspace_removed"]; ok {
		removed, err = getString(fields, "local_workspace_removed")
		if err != nil || !statusValue(removed) {
			return nil, errors.New("invalid local removal status")
		}
	}
	expectedStatus := "fail"
	if code == 0 {
		expectedStatus = "error"
		if values[0] == "pass" && values[1] == "pass" {
			expectedStatus = "incomplete"
		}
	}
	if status != expectedStatus || (code == 0 && (stop != "pass" || closeState != "pass" || removed == "fail")) {
		return nil, errors.New("contradictory acceptance results")
	}
	observed := []any{}
	for i := 0; i < 2; i++ {
		observed = append(observed, map[string]any{"id": "acceptance/" + ids[i], "status": values[i]})
	}
	observed = append(observed,
		map[string]any{"id": "acceptance/stop_command", "status": stop},
		map[string]any{"id": "acceptance/close_command", "status": closeState},
		map[string]any{"id": "acceptance/local_workspace_removed", "status": removed})
	summary := map[string]int{"passed": 0, "failed": 0, "errors": 0, "not_run": 0}
	for _, state := range []string{values[0], values[1], stop, closeState, removed} {
		summary[map[string]string{"pass": "passed", "fail": "failed", "not_run": "not_run"}[state]]++
	}
	return Report{
		"schema_version": 1, "profile": "local_acceptance_view", "status": status,
		"summary": summary, "results": observed,
		"source": map[string]any{"run_id": runID, "mode": mode, "created_utc": created},
		"verification_tier": map[string]string{"mock": "mock_orchestration", "live": "local_live_partial"}[mode],
		"agent_tools": unverified, "upstream_inference": unverified,
		"credential_egress": unverified, "cleanup": unverified, "complete_acceptance": false,
	}, nil
}

func UnavailableAcceptance() Report {
	return Report{
		"schema_version": 1, "profile": "local_acceptance_view", "status": "error",
		"summary": map[string]int{"passed": 0, "failed": 0, "errors": 1, "not_run": 0},
		"results": []any{map[string]any{"id": "acceptance/evidence", "status": "error"}},
		"agent_tools": unverified, "upstream_inference": unverified,
		"credential_egress": unverified, "cleanup": unverified, "complete_acceptance": false,
	}
}

// ReadAcceptance accepts only an explicitly selected private runner-owned
// receipt. The root must be absolute and outside the current checkout.
func ReadAcceptance(root, runID string) (Report, error) {
	if !filepath.IsAbs(root) || !strings.HasPrefix(runID, "aginctus-acceptance.") ||
		!acceptanceID.MatchString(strings.TrimPrefix(runID, "aginctus-acceptance.")) {
		return nil, errors.New("invalid private acceptance selection")
	}
	repoRoot, err := repositoryRoot()
	if err != nil {
		return nil, err
	}
	effective, err := filepath.EvalSymlinks(root)
	if err != nil || effective != root {
		return nil, errors.New("invalid acceptance root path")
	}
	relative, err := filepath.Rel(repoRoot, effective)
	if err != nil || relative == "." || (!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && relative != "..") {
		return nil, errors.New("evidence root inside checkout")
	}
	parent, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(parent)
	if err := privateFD(parent, true); err != nil {
		return nil, err
	}
	subdir, err := syscall.Openat(parent, runID, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(subdir)
	if err := privateFD(subdir, true); err != nil {
		return nil, err
	}
	fd, err := syscall.Openat(subdir, "result.json", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "result.json")
	defer file.Close()
	if err := privateFD(fd, false); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxAcceptanceBytes+1))
	if err != nil || len(raw) > maxAcceptanceBytes {
		return nil, errors.New("unreadable acceptance evidence")
	}
	return NormalizeAcceptance(raw, runID)
}

func repositoryRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "justfile")); err == nil {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return "", errors.New("repository root unavailable")
		}
	}
}

// ReadSelectedJSON accepts explicit, bounded, ordinary existing JSON files,
// never directory traversal via an automatically discovered receipt.
func ReadSelectedJSON(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "selected.json")
	defer file.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		st.Size > maxSelectedBytes {
		return nil, errors.New("invalid selected JSON file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSelectedBytes+1))
	if err != nil || len(data) > maxSelectedBytes {
		return nil, errors.New("unreadable selected JSON")
	}
	return data, nil
}

// NormalizePreflight checks an existing read-only Python pilot report but
// never runs its pilot-touching check or promotes it into live authorization.
func NormalizePreflight(payload []byte, check string) (string, error) {
	if check != "pilot-topology" && check != "opencode-runtime" {
		return "", errors.New("invalid preflight selection")
	}
	data, err := object(payload)
	if err != nil {
		return "", err
	}
	version, err := getInt(data, "schema_version")
	status, se := getString(data, "status")
	if err != nil || version != 1 || se != nil || (status != "pass" && status != "fail" && status != "error") ||
		!requiredString(data, "profile", "pilot_preflight") ||
		!requiredString(data, "verification_tier", "read_only_pilot_preflight") ||
		!requiredString(data, "upstream_inference", unverified) ||
		!requiredString(data, "gateway_authorization", unverified) ||
		!requiredString(data, "egress_policy", unverified) {
		return "", errors.New("invalid pilot preflight contract")
	}
	if raw, ok := data["target"]; ok && string(raw) != "null" {
		target, err := getString(data, "target")
		if err != nil || (target != "configured_pilot" && !preflightTarget.MatchString(target)) {
			return "", errors.New("invalid preflight target")
		}
	}
	for _, name := range []string{"duration_ms"} {
		if raw, ok := data[name]; ok && string(raw) != "null" {
			duration, err := getInt(data, name)
			if err != nil || duration < 0 {
				return "", errors.New("invalid preflight duration")
			}
		}
	}
	var rows []json.RawMessage
	if json.Unmarshal(data["results"], &rows) != nil || len(rows) != 1 {
		return "", errors.New("invalid preflight results")
	}
	row, err := object(rows[0])
	if err != nil || !requiredString(row, "id", "preflight/"+check) || !requiredString(row, "status", status) {
		return "", errors.New("invalid preflight result")
	}
	exit, exists := row["exit_code"]
	if !exists {
		return "", errors.New("missing preflight exit code")
	}
	if status == "error" {
		if string(exit) != "null" {
			return "", errors.New("unexpected error exit code")
		}
	} else {
		code, err := getInt(row, "exit_code")
		if err != nil || (status == "pass") != (code == 0) {
			return "", errors.New("inconsistent preflight exit code")
		}
	}
	if raw, ok := row["duration_ms"]; ok && string(raw) != "null" {
		duration, err := getInt(row, "duration_ms")
		if err != nil || duration < 0 {
			return "", errors.New("invalid preflight result duration")
		}
	}
	counts, err := object(data["summary"])
	if err != nil || len(counts) != 3 {
		return "", errors.New("invalid preflight counts")
	}
	for key, want := range map[string]int{"passed": 0, "failed": 0, "errors": 0} {
		if (key == "passed" && status == "pass") || (key == "failed" && status == "fail") || (key == "errors" && status == "error") {
			want = 1
		}
		got, err := getInt(counts, key)
		if err != nil || got != want {
			return "", errors.New("inconsistent preflight counts")
		}
	}
	return status, nil
}

// WriteAcceptanceText is intentionally restricted to the five independently
// host-observed local checks; unknown source values are never printed.
func WriteAcceptanceText(output io.Writer, report Report) error {
	results, _ := report["results"].([]any)
	for _, result := range results {
		row, _ := result.(map[string]any)
		if _, err := fmt.Fprintf(output, "%-10s %s\n", strings.ToUpper(row["status"].(string)), row["id"].(string)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output, "NOT VERIFIED: agent tools, real upstream inference, egress, cleanup")
	return err
}
