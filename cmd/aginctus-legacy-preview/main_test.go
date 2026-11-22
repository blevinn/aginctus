package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func legacyPreviewFixture(t *testing.T) ([]string, string) {
	t.Helper()
	d := t.TempDir()
	legacy := acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind: acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion: acceptedstate.SelectionAPIVersion,
			Kind: acceptedstate.SelectionKind,
			DeploymentID: "old", IncusProject: "project1",
			SelectedWorkloads: []string{"worker"},
		},
		Workloads: []acceptedstate.WorkloadSpec{{
			ID: "worker", InstanceName: "worker-instance",
			InstanceType: "container",
			ImageFingerprint: strings.Repeat("a", 64),
			Profiles: []string{"base"}, EnsureRunning: true,
		}},
	}
	digest, err := legacy.Digest()
	if err != nil { t.Fatal(err) }
	spec, err := legacy.Canonical()
	if err != nil { t.Fatal(err) }
	legacyPath := filepath.Join(d, "legacy.json")
	if err := os.WriteFile(legacyPath, spec, 0o600); err != nil { t.Fatal(err) }
	mappingPath := filepath.Join(d, "mappings.json")
	if err := os.WriteFile(mappingPath, []byte(`[{"legacyId":"worker","targetId":"new-worker","instanceName":"worker-instance"}]`), 0o600); err != nil { t.Fatal(err) }
	args := []string{
		"--legacy-path", legacyPath, "--legacy-deployment", "old",
		"--expected-legacy-digest", digest,
		"--project", "project1", "--server-id", "selected-server",
		"--environment-id", "7c82e6d1-1212-4123-8123-0123456789ab",
		"--environment-name", "development", "--mappings", mappingPath,
	}
	return args, mappingPath
}

func TestLegacyPreviewCommandIsReadOnlyAndExplicit(t *testing.T) {
	args, _ := legacyPreviewFixture(t)
	var output bytes.Buffer
	if err := run(args, &output); err != nil { t.Fatal(err) }
	var report environment.LegacyCandidatePreview
	if err := json.Unmarshal(output.Bytes(), &report); err != nil { t.Fatal(err) }
	if report.Candidate.Environment.Binding.Project != "project1" ||
		report.Candidate.Environment.ID != "7c82e6d1-1212-4123-8123-0123456789ab" ||
		len(report.Candidate.Workloads) != 1 ||
		report.Candidate.Workloads[0].ID != "new-worker" ||
		report.Candidate.Workloads[0].InstanceName != "worker-instance" {
		t.Fatalf("invalid preview: %+v", report)
	}
	if report.Candidate.Workloads[0].ImageFingerprint != strings.Repeat("a", 64) {
		t.Fatal("pinned image not preserved")
	}
	for _, replacement := range []struct{ key, value string }{
		{"--expected-legacy-digest", strings.Repeat("f", 64)},
		{"--project", "other"},
		{"--environment-id", "non-uuid"},
		{"--legacy-deployment", "other"},
	} {
		changed := append([]string(nil), args...)
		for i := range changed {
			if changed[i] == replacement.key {
				changed[i+1] = replacement.value
				break
			}
		}
		if err := run(changed, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid selector %s", replacement.key)
		}
	}
	if err := run(append(args, "--confirm"), &bytes.Buffer{}); err == nil {
		t.Fatal("accepted unsupported mutation confirmation")
	}
}

func TestLegacyPreviewRejectsMalformedMappingsWithoutOutput(t *testing.T) {
	args, mappingPath := legacyPreviewFixture(t)
	for _, invalid := range []string{
		`[{"legacyId":"worker","legacyId":"other","targetId":"new-worker","instanceName":"worker-instance"}]`,
		`[{"legacyId":"worker","LegacyId":"worker","targetId":"new-worker","instanceName":"worker-instance"}]`,
		`[{"legacyId":"worker","targetId":"../bad","instanceName":"worker-instance"}]`,
		`[{"legacyId":"worker","targetId":"new-worker","instanceName":"different"}]`,
		`[{"legacyId":"worker","targetId":"new-worker","instanceName":null}]`,
		`{}`,
	} {
		if err := os.WriteFile(mappingPath, []byte(invalid), 0o600); err != nil { t.Fatal(err) }
		var output bytes.Buffer
		if err := run(args, &output); err == nil || output.Len() != 0 {
			t.Fatalf("accepted invalid mapping %s", invalid)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(t.TempDir(), "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
}
