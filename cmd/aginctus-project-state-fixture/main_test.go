package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

const (
	testID = "11111111-1111-4111-8111-111111111111"
	peerID = "22222222-2222-4222-8222-222222222222"
)

func TestFixtureCLIRecoversScopedNativeJournal(t *testing.T) {
	ctx := t.Context()
	c := fixtureCandidate(testID)
	root := filepath.Join(t.TempDir(), "project-state")
	store, err := environment.InitializeLocalProjectStateStore(root, c.Environment.Binding)
	if err != nil {
		t.Fatal(err)
	}
	r, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := store.PinLocalRevision(ctx, c.Environment, r)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	j, err := pin.NewLocalNativeJournal()
	if err != nil {
		t.Fatal(err)
	}
	if err := j.BeginEffect(ctx, "worker", "worker", "start"); err != nil {
		t.Fatal(err)
	}
	call := func(action, id string, flags ...string) (string, error) {
		var out bytes.Buffer
		args := []string{action, "--state-dir", root, "--server-id", c.Environment.Binding.ServerID,
			"--project", c.Environment.Binding.Project, "--environment", id}
		err := run(ctx, append(args, flags...), &out)
		return out.String(), err
	}
	if out, err := call("operations", testID); err != nil || !strings.Contains(out, j.ID()) {
		t.Fatalf("list: %q %v", out, err)
	}
	if out, err := call("operation", testID, "--operation-id", j.ID()); err != nil || !strings.Contains(out, `"outcome":"uncertain"`) {
		t.Fatalf("inspect: %q %v", out, err)
	}
	if _, err := call("operation", peerID, "--operation-id", j.ID()); err == nil {
		t.Fatal("read another environment's operation")
	}
	if _, err := call("operation", testID, "--operation-id", j.ID(), "--confirm"); err == nil {
		t.Fatal("recovery accepted a write flag")
	}
}

func fixtureCandidate(id string) environment.Candidate {
	return environment.Candidate{
		APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind,
		Environment: environment.Descriptor{
			ID: id, Name: "guest-state",
			Binding: environment.Binding{ServerID: "fixture-server", Project: "owned-project"},
		},
		SelectedWorkloads: []string{"worker"},
		Workloads: []acceptedstate.WorkloadSpec{{
			ID: "worker", InstanceName: "test-worker", InstanceType: "container",
			ImageFingerprint: strings.Repeat("a", 64), Profiles: []string{"default"},
			EnsureRunning: true,
		}},
	}
}

func TestProjectVolumeFixtureHasIndependentAcceptedHistories(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "private-volume")
	args := []string{"--state-dir", root, "--server-id", "fixture-server", "--project", "owned-project"}
	call := func(action string, flags ...string) (acceptedStatus, error) {
		t.Helper()
		var out bytes.Buffer
		opts := append([]string{action}, args...)
		opts = append(opts, flags...)
		err := run(ctx, opts, &out)
		if err != nil {
			return acceptedStatus{}, err
		}
		var state acceptedStatus
		if action != "init" {
			if err := json.Unmarshal(out.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
		}
		return state, nil
	}
	if _, err := call("status", "--environment", testID); err == nil {
		t.Fatal("opened uninitialized volume")
	}
	if _, err := call("init"); err != nil {
		t.Fatal(err)
	}
	candidates := map[string]string{}
	for _, id := range []string{testID, peerID} {
		p := filepath.Join(t.TempDir(), id+".json")
		data, err := fixtureCandidate(id).Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		candidates[id] = p
		first, err := call("status", "--environment", id)
		if err != nil || first.HasAccepted {
			t.Fatalf("absent local revision: %+v %v", first, err)
		}
	}
	a, err := call("accept", "--environment", testID, "--candidate", candidates[testID],
		"--expected-revision", "none", "--confirm")
	if err != nil || a.Revision == nil || a.Revision.Generation != 1 {
		t.Fatalf("first accepted fixture: %+v %v", a, err)
	}
	b, err := call("accept", "--environment", peerID, "--candidate", candidates[peerID],
		"--expected-revision", "none", "--confirm")
	if err != nil || b.Revision == nil || b.Revision.Generation != 1 || b.Revision.ID == a.Revision.ID {
		t.Fatalf("independent environment revisions: %+v %+v %v", a, b, err)
	}
	if _, err := call("accept", "--environment", testID, "--candidate", candidates[testID],
		"--expected-revision", "none", "--confirm"); !errors.Is(err, environment.ErrAcceptedConflict) {
		t.Fatalf("stale expected pointer accepted: %v", err)
	}
	found, err := call("status", "--environment", testID)
	if err != nil || found.Revision == nil || found.Revision.ID != a.Revision.ID {
		t.Fatalf("lost A after B acceptance: %+v %v", found, err)
	}
	if _, err := call("status", "--environment", peerID, "--confirm"); err == nil {
		t.Fatal("read accepted mutation flag")
	}
}

func TestFixtureCLIRequiresExplicitScopeAndConfirmation(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "store")
	tests := [][]string{
		nil, {"init"}, {"init", "--state-dir", root}, {"init", "--state-dir", root, "--server-id", "s", "--project", "default"},
		{"status", "--state-dir", root, "--server-id", "s", "--project", "owned-project"},
		{"accept", "--state-dir", root, "--server-id", "s", "--project", "owned-project",
			"--environment", testID, "--expected-revision", "none"},
		{"unknown", "--state-dir", root, "--server-id", "s", "--project", "owned-project"},
	}
	for _, args := range tests {
		if err := run(ctx, args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid flags: %+v", args)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := run(cancelled, []string{"init"}, &bytes.Buffer{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fixture operation proceeded: %v", err)
	}
}

func TestFixtureAuditCommandIsReadOnlyAndScoped(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "project-state")
	common := []string{"--state-dir", root, "--server-id", "fixture-server", "--project", "owned-project"}
	call := func(action string, flags ...string) ([]byte, error) {
		args := append([]string{action}, common...)
		args = append(args, flags...)
		var out bytes.Buffer
		err := run(ctx, args, &out)
		return out.Bytes(), err
	}
	if _, err := call("init"); err != nil {
		t.Fatal(err)
	}
	raw, err := call("audit", "--environment", testID)
	if err != nil {
		t.Fatal(err)
	}
	var report environment.LocalHistoryAudit
	if err := json.Unmarshal(raw, &report); err != nil || report.HasAccepted || report.Reachable != 0 {
		t.Fatalf("empty audit: %+v %v", report, err)
	}
	candidate := fixtureCandidate(testID)
	data, err := candidate.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := call("accept", "--environment", testID, "--candidate", path,
		"--expected-revision", "none", "--confirm"); err != nil {
		t.Fatal(err)
	}
	raw, err = call("audit", "--environment", testID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &report); err != nil || !report.HasAccepted ||
		report.Generation != 1 || report.Reachable != 1 || report.HeadID == "" {
		t.Fatalf("published audit: %+v %v", report, err)
	}
	for _, flags := range [][]string{
		{"--environment", testID, "--confirm"},
		{"--environment", testID, "--candidate", path},
		{"--environment", "not-a-uuid"},
		{"--environment", testID, "--expected-revision", "none"},
	} {
		if _, err := call("audit", flags...); err == nil {
			t.Fatalf("audit accepted write or invalid selection flags: %v", flags)
		}
	}
	raw, err = call("status", "--environment", testID)
	if err != nil {
		t.Fatal(err)
	}
	var status acceptedStatus
	if err := json.Unmarshal(raw, &status); err != nil || !status.HasAccepted ||
		status.Revision == nil || status.Revision.ID != report.HeadID {
		t.Fatalf("audit changed published pointer: %+v %v", status, err)
	}
}
