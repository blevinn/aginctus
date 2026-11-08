// Package evidence reads bounded, private, operator-selected test receipts.
// Receipt existence and status are not independent proof of live acceptance.
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/blevinn/aginctus/internal/validation/catalog"
)

const (
	maxVMBytes = 16 * 1024
	vmTier     = "disposable_guest_no_live_provider"
	unverified = "not_verified"
)

var (
	runIDPattern  = regexp.MustCompile("^[0-9a-f]{24}$")
	commitPattern = regexp.MustCompile("^(?:[0-9a-f]{40}|[0-9a-f]{64})$")
	hashPattern   = regexp.MustCompile("^[0-9a-f]{64}$")
)

// Report is a fixed allowlisted JSON envelope assembled by NormalizeVM.
// Never copy untrusted raw record fields into a report.
type Report map[string]any

func privateFD(fd int, directory bool) error {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return err
	}
	kind := uint32(syscall.S_IFREG)
	if directory {
		kind = syscall.S_IFDIR
	}
	if st.Mode&syscall.S_IFMT != kind || st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 {
		return errors.New("invalid evidence file ownership or permissions")
	}
	if !directory && (st.Size > maxVMBytes || st.Nlink != 1) {
		return errors.New("invalid evidence file bounds")
	}
	return nil
}

// ReadVM opens only root/<24 lowercase hex>/result.json, descriptor-relative,
// without following symbolic links. The root must be owner-only.
func ReadVM(root, runID string) (Report, error) {
	if !runIDPattern.MatchString(runID) {
		return nil, errors.New("invalid VM evidence selector")
	}
	parent, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(parent)
	if err := privateFD(parent, true); err != nil {
		return nil, err
	}
	runDir, err := syscall.Openat(parent, runID, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(runDir)
	if err := privateFD(runDir, true); err != nil {
		return nil, err
	}
	fd, err := syscall.Openat(runDir, "result.json", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "result.json")
	defer file.Close()
	if err := privateFD(fd, false); err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxVMBytes+1))
	if err != nil || len(payload) > maxVMBytes {
		return nil, errors.New("unreadable or oversized VM evidence")
	}
	return NormalizeVM(payload, runID)
}

func getString(source map[string]json.RawMessage, name string) (string, error) {
	var result string
	raw, ok := source[name]
	if !ok || len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &result) != nil {
		return "", fmt.Errorf("invalid %s", name)
	}
	return result, nil
}

func getInt(source map[string]json.RawMessage, name string) (int, error) {
	var number json.Number
	if json.Unmarshal(source[name], &number) != nil {
		return 0, fmt.Errorf("invalid numeric %s", name)
	}
	value, err := strconv.Atoi(number.String())
	if err != nil {
		return 0, fmt.Errorf("invalid integer %s", name)
	}
	return value, nil
}

func optional(source map[string]json.RawMessage, name string, pattern *regexp.Regexp) (any, error) {
	raw, ok := source[name]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	value, err := getString(source, name)
	if err != nil || !pattern.MatchString(value) {
		return nil, fmt.Errorf("invalid optional %s", name)
	}
	return value, nil
}

func fullStack(raw json.RawMessage, passing bool) (map[string]any, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 4 {
		return nil, errors.New("invalid full-stack object")
	}
	version, err := getInt(fields, "schema_version")
	source, sourceErr := getString(fields, "source")
	if err != nil || version != 1 || sourceErr != nil || source != "guest_observation_not_attestation" {
		return nil, errors.New("invalid full-stack provenance")
	}
	result := map[string]any{"schema_version": 1, "source": source}
	for _, section := range []struct {
		name string
		ids  []string
	}{
		{"phases", []string{"preflight", "open", "start", "prompt", "wait", "credential-check", "gateway-evidence", "stop", "close"}},
		{"gates", []string{"synthetic_setup", "tool_transcript", "fixture_diff", "independent_test", "credential_scope", "gateway_correlation", "workload_network", "failure_cleanup_control", "owned_fixture_removed", "foreign_workspaces_preserved", "scoped_key_revoked", "workload_key_absent", "model_removed", "listener_stopped", "storage_preserved", "external_traffic", "external_deny"}},
	} {
		var entries map[string]json.RawMessage
		if json.Unmarshal(fields[section.name], &entries) != nil || len(entries) != len(section.ids) {
			return nil, errors.New("invalid full-stack section")
		}
		values := make(map[string]string, len(entries))
		for _, name := range section.ids {
			value, err := getString(entries, name)
			if err != nil || (value != "pass" && value != "fail" && value != unverified) {
				return nil, errors.New("invalid full-stack outcome")
			}
			if passing && value != "pass" {
				return nil, errors.New("incomplete passing full-stack evidence")
			}
			values[name] = value
		}
		result[section.name] = values
	}
	return result, nil
}

// NormalizeVM checks the v1 record contract and copies fixed metadata only.
// Unknown input keys (including untrusted payloads) are never emitted.
func NormalizeVM(payload []byte, runID string) (Report, error) {
	if !runIDPattern.MatchString(runID) || len(payload) > maxVMBytes || !utf8.Valid(payload) {
		return nil, errors.New("invalid VM receipt input")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil || fields == nil {
		return nil, errors.New("invalid VM receipt JSON")
	}
	version, err := getInt(fields, "schema_version")
	scenario, e1 := getString(fields, "scenario")
	recordID, e2 := getString(fields, "run_id")
	status, e3 := getString(fields, "status")
	exitCode, e4 := getInt(fields, "exit_code")
	tier, e5 := getString(fields, "verification_tier")
	upstream, e6 := getString(fields, "upstream_inference")
	cleanup, e7 := getString(fields, "cleanup")
	created, e8 := getString(fields, "created_utc")
	if err != nil || version != 1 || e1 != nil || e2 != nil || e3 != nil || e4 != nil ||
		e5 != nil || e6 != nil || e7 != nil || e8 != nil || recordID != runID ||
		(status != "pass" && status != "fail") || exitCode < -255 || exitCode > 255 ||
		(status == "pass") != (exitCode == 0) || tier != vmTier ||
		upstream != unverified || cleanup != unverified {
		return nil, errors.New("invalid VM receipt contract")
	}
	exposed := false
	for _, item := range catalog.PublicVMScenarios() {
		if item.Name == scenario && item.Evidence {
			exposed = true
			break
		}
	}
	if !exposed {
		return nil, errors.New("unrecognized VM evidence scenario")
	}
	var stage any
	if raw, ok := fields["failure_stage"]; ok && string(raw) != "null" {
		stage, err = getString(fields, "failure_stage")
		if err != nil || (stage != "launch" && stage != "build" && stage != "runtime") {
			return nil, errors.New("invalid VM failure stage")
		}
	}
	if (status == "pass" && stage != nil) || (status == "fail" && stage == nil) {
		return nil, errors.New("inconsistent VM failure stage")
	}
	instant, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, errors.New("invalid VM UTC timestamp")
	}
	_, offset := instant.Zone()
	if offset != 0 {
		return nil, errors.New("VM timestamp is not UTC")
	}
	revision, err := optional(fields, "git_revision", commitPattern)
	if err != nil {
		return nil, err
	}
	lock, err := optional(fields, "flake_lock_sha256", hashPattern)
	if err != nil {
		return nil, err
	}
	var dirty any
	if raw, ok := fields["git_dirty"]; ok && string(raw) != "null" {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("invalid git dirty marker")
		}
		dirty = value
	}
	counts := map[string]int{"passed": 0, "failed": 0, "errors": 0}
	counts[map[string]string{"pass": "passed", "fail": "failed"}[status]] = 1
	report := Report{
		"schema_version": 1,
		"profile": "disposable_vm",
		"status": status,
		"summary": counts,
		"results": []any{map[string]any{"id": "vm/" + scenario, "status": status, "exit_code": exitCode, "rerun": "just test-vm-" + scenario}},
		"source": map[string]any{"kind": "vm_harness", "run_id": runID, "created_utc": instant.Format(time.RFC3339Nano), "git_revision": revision, "git_dirty": dirty, "flake_lock_sha256": lock},
		"failure_stage": stage,
		"verification_tier": vmTier,
		"upstream_inference": unverified,
		"cleanup": unverified,
	}
	if full, ok := fields["full_stack"]; ok {
		if scenario != "full-stack" {
			return nil, errors.New("full-stack observations on wrong scenario")
		}
		normalized, err := fullStack(full, status == "pass")
		if err != nil {
			return nil, err
		}
		report["full_stack"] = normalized
	}
	return report, nil
}

// Unavailable explicitly claims no verification on any malformed or unreadable
// operator-selected receipt. It never includes the failed selector or path.
func Unavailable() Report {
	return Report{
		"schema_version": 1, "profile": "disposable_vm", "status": "error",
		"summary": map[string]int{"passed": 0, "failed": 0, "errors": 1},
		"results": []any{map[string]any{"id": "vm/evidence", "status": "error", "exit_code": nil}},
		"verification_tier": unverified,
		"upstream_inference": unverified, "cleanup": unverified,
	}
}
