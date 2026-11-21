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
