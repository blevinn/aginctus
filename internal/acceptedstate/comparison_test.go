package acceptedstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadCandidateFileRequiresExactIdentityAndValidSpec(t *testing.T) {
	root := t.TempDir()
	spec := validSpec()
	data, err := spec.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "candidate.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCandidateFile(file, "demo", "aginctus"); err != nil || got.Selection.DeploymentID != "demo" {
		t.Fatalf("candidate read: %v, %+v", err, got)
	}
	for _, tc := range []struct{ deployment, project string }{
		{"other", "aginctus"}, {"demo", "foreign"}, {"", "aginctus"}, {"demo", ""},
	} {
		if _, err := ReadCandidateFile(file, tc.deployment, tc.project); err == nil {
			t.Fatalf("accepted invalid scope: %+v", tc)
		}
	}
	if err := os.WriteFile(file, []byte(`{"gateway":{"password":"nonsecret-fixture"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCandidateFile(file, "demo", "aginctus"); err == nil || strings.Contains(err.Error(), "nonsecret-fixture") {
		t.Fatalf("unsafe candidate error: %v", err)
	}
	if err := os.WriteFile(file, make([]byte, maxSelectionBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCandidateFile(file, "demo", "aginctus"); err == nil {
		t.Fatal("accepted oversized input")
	}
}

func TestReadCandidateFileRejectsFIFOWithoutWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadCandidateFile(path, "demo", "aginctus")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted FIFO as a candidate")
		}
	case <-time.After(2 * time.Second):
		f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			f.Close()
		}
		t.Fatal("candidate open blocked waiting for a FIFO writer")
	}
}

func TestCompareCandidateDistinguishesUnacceptedEmptySelection(t *testing.T) {
	store, err := InitializeLocalStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	spec := validSpec()
	spec.Selection.SelectedWorkloads = []string{}
	spec.Workloads = []WorkloadSpec{}
	before, err := CompareCandidate(store, spec)
	if err != nil || before.HasAccepted || !before.Changed || len(before.Changes.NewlySelected) != 0 {
		t.Fatalf("unaccepted empty selection: %+v, %v", before, err)
	}
	if _, err := store.CommitLocal(0, "", spec); err != nil {
		t.Fatal(err)
	}
	after, err := CompareCandidate(store, spec)
	if err != nil || !after.HasAccepted || after.Changed {
		t.Fatalf("accepted empty selection: %+v, %v", after, err)
	}
}

func TestReadOnlyComparisonDoesNotPromote(t *testing.T) {
	store, err := InitializeLocalStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	spec := validSpec()
	initial, err := CompareCandidate(store, spec)
	if err != nil {
		t.Fatal(err)
	}
	if initial.HasAccepted || !initial.Changed || len(initial.Changes.NewlySelected) != 2 {
		t.Fatalf("initial status: %+v", initial)
	}
	if rev, _, err := store.ReadActive(); err != nil || rev != nil {
		t.Fatalf("read-only diff wrote revision: %+v, %v", rev, err)
	}
	accepted, err := store.CommitLocal(0, "", spec)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := CompareCandidate(store, spec)
	if err != nil || unchanged.Changed || unchanged.AcceptedRevisionID != accepted.ID || unchanged.AcceptedGeneration != 1 {
		t.Fatalf("unchanged comparison: %+v, %v", unchanged, err)
	}
	spec.Workloads[0].ImageFingerprint = strings.Repeat("e", 64)
	diff, err := CompareCandidate(store, spec)
	if err != nil || !diff.Changed || len(diff.Changes.Modified) != 1 {
		t.Fatalf("modified comparison: %+v, %v", diff, err)
	}
	encoded, err := json.Marshal(diff)
	if err != nil || strings.Contains(string(encoded), strings.Repeat("e", 64)) {
		t.Fatalf("diff leaked values: %s, %v", encoded, err)
	}
	revision, _, err := store.ReadActive()
	if err != nil || revision.ID != accepted.ID {
		t.Fatal("read-only diff changed accepted revision")
	}
	spec.Selection.IncusProject = "other"
	if _, err := CompareCandidate(store, spec); err == nil {
		t.Fatal("allowed cross-project read comparison")
	}
}
