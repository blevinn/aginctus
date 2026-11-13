package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/acceptedstate"
	"github.com/blevinn/aginctus/internal/filestate"
)

func candidateFile(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "candidate.json")
	s := acceptedstate.DeploymentSpec{
		APIVersion: acceptedstate.DeploymentAPIVersion,
		Kind:       acceptedstate.DeploymentKind,
		Selection: acceptedstate.SelectionManifest{
			APIVersion:        acceptedstate.SelectionAPIVersion,
			Kind:              acceptedstate.SelectionKind,
			DeploymentID:      "demo",
			IncusProject:      "isolated",
			SelectedWorkloads: []string{"agent"},
		},
		Workloads: []acceptedstate.WorkloadSpec{{
			ID: "agent", InstanceName: "demo-agent", InstanceType: "container",
			ImageFingerprint: strings.Repeat("a", 64), Profiles: []string{}, EnsureRunning: true,
		}},
	}
	data, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOfflineExplicitLocalAcceptanceLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	candidate := candidateFile(t, t.TempDir())
	var out bytes.Buffer
	call := func(args ...string) (map[string]any, error) {
		t.Helper()
		out.Reset()
		err := run(args, &out)
		if err != nil {
			return nil, err
		}
		var value map[string]any
		if err := json.Unmarshal(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value, nil
	}
	base := []string{"--state-dir", root, "--deployment", "demo", "--project", "isolated"}
	if _, err := call(append([]string{"status"}, base...)...); err == nil {
		t.Fatal("implicit initialization accepted")
	}
	if _, err := call("init", "--state-dir", root); err != nil {
		t.Fatal(err)
	}
	status, err := call(append([]string{"status"}, base...)...)
	if err != nil || status["hasAccepted"] != false {
		t.Fatalf("empty status %+v %v", status, err)
	}
	diffArgs := append(append([]string{"diff"}, base...), "--candidate", candidate)
	diff, err := call(diffArgs...)
	if err != nil || diff["hasAccepted"] != false || diff["changed"] != true {
		t.Fatalf("initial diff %+v %v", diff, err)
	}
	if _, err := call(append([]string{"accept"}, append(base, "--candidate", candidate)...)...); err == nil {
		t.Fatal("accepted without confirm+CAS")
	}
	// Nothing mutates until explicit --confirm and a genesis CAS expectation.
	opened, err := acceptedstate.OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if current, _, err := opened.ReadActive(); err != nil || current != nil {
		t.Fatalf("diff/denied accept mutated state: %+v %v", current, err)
	}
	acceptedArgs := append([]string{"accept"}, base...)
	acceptedArgs = append(acceptedArgs, "--candidate", candidate, "--expected-revision", "none", "--confirm")
	rev, err := call(acceptedArgs...)
	if err != nil {
		t.Fatal(err)
	}
	if rev["generation"] != float64(1) {
		t.Fatalf("first generation: %+v", rev)
	}
	status, err = call(append([]string{"status"}, base...)...)
	if err != nil || status["hasAccepted"] != true {
		t.Fatalf("status %+v %v", status, err)
	}
	if _, err := call(acceptedArgs...); !errors.Is(err, acceptedstate.ErrRevisionConflict) {
		t.Fatalf("stale genesis: %v", err)
	}
	diff, err = call(diffArgs...)
	if err != nil || diff["changed"] != false {
		t.Fatalf("unchanged: %+v %v", diff, err)
	}
	// Exercise the non-genesis path with the exact observed revision and an
	// actual workload change, then prove stale updates and no-ops do not write.
	firstID := rev["id"].(string)
	updateArgs := append(append([]string{"accept"}, base...), "--candidate", candidate, "--expected-revision", firstID, "--confirm")
	if _, err := call(updateArgs...); !errors.Is(err, acceptedstate.ErrNoChanges) {
		t.Fatalf("unchanged acceptance: %v", err)
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(strings.Repeat("a", 64)), []byte(strings.Repeat("b", 64)), 1)
	if err := os.WriteFile(candidate, data, 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err = call(diffArgs...)
	if err != nil || diff["changed"] != true || strings.Contains(out.String(), strings.Repeat("b", 64)) {
		t.Fatalf("changed candidate diff: %+v %v", diff, err)
	}
	second, err := call(updateArgs...)
	if err != nil || second["generation"] != float64(2) || second["parentID"] != firstID {
		t.Fatalf("second acceptance: %+v %v", second, err)
	}
	if _, err := call(updateArgs...); !errors.Is(err, acceptedstate.ErrRevisionConflict) {
		t.Fatalf("stale update: %v", err)
	}
	for _, action := range []string{"status", "diff", "accept"} {
		args := []string{action, "--state-dir", root, "--deployment", "foreign", "--project", "isolated"}
		if action != "status" {
			args = append(args, "--candidate", candidate)
		}
		if action == "accept" {
			args = append(args, "--expected-revision", second["id"].(string), "--confirm")
		}
		if _, err := call(args...); err == nil {
			t.Fatalf("%s allowed foreign identity", action)
		}
	}
	// Reopen through a fresh store handle after all refused operations.
	reopened, err := acceptedstate.OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := reopened.ReadActive()
	if err != nil || active.ID != second["id"] || active.Generation != 2 {
		t.Fatalf("denied operation changed revision: %+v %v", active, err)
	}
}

func TestLocalAcceptanceFailsClosedOnIdentityAndUnsafeCandidate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if _, err := acceptedstate.InitializeLocalStore(root); err != nil {
		t.Fatal(err)
	}
	candidate := candidateFile(t, t.TempDir())
	for _, args := range [][]string{
		{"accept", "--state-dir", root, "--project", "isolated", "--candidate", candidate, "--expected-revision", "none", "--confirm"},
		{"accept", "--state-dir", root, "--deployment", "demo", "--project", "foreign", "--candidate", candidate, "--expected-revision", "none", "--confirm"},
		{"accept", "--state-dir", root, "--deployment", "demo", "--project", "isolated", "--candidate", candidate, "--expected-revision", "other", "--confirm"},
		{"init", "--state-dir", root, "--confirm"},
		{"status", "--state-dir", root, "--deployment", "demo", "--project", "default"},
		{"status", "--state-dir", root, "--deployment", "../unsafe", "--project", "isolated"},
		{"status", "--state-dir", root, "--deployment", "demo", "--project", "../unsafe"},
	} {
		var out bytes.Buffer
		if err := run(args, &out); err == nil {
			t.Fatalf("accepted invalid operation: %v", args)
		}
		if out.Len() != 0 {
			t.Fatal("leaked successful output on failure")
		}
	}
	p := filepath.Join(t.TempDir(), "malicious.json")
	if err := os.WriteFile(p, []byte(`{"secret":"canary"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := run([]string{"accept", "--state-dir", root, "--deployment", "demo", "--project", "isolated", "--candidate", p, "--expected-revision", "none", "--confirm"}, &output)
	if err == nil || strings.Contains(err.Error(), "canary") || output.Len() != 0 {
		t.Fatalf("unsafe output: %v %q", err, output.String())
	}
}

func TestConcurrentLocalAcceptanceHasOneWinner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store, err := acceptedstate.InitializeLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateFile(t, t.TempDir())
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			var out bytes.Buffer
			results <- run([]string{"accept", "--state-dir", root, "--deployment", "demo", "--project", "isolated", "--candidate", candidate, "--expected-revision", "none", "--confirm"}, &out)
		}()
	}
	close(start)
	winners, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case errors.Is(err, acceptedstate.ErrRevisionConflict), errors.Is(err, filestate.ErrRegistryBusy):
			conflicts++
		default:
			t.Fatalf("unexpected acceptance result: %v", err)
		}
	}
	active, _, err := store.ReadActive()
	if err != nil || active == nil || active.Generation != 1 || winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent acceptance: winners=%d conflicts=%d active=%+v err=%v", winners, conflicts, active, err)
	}
}
