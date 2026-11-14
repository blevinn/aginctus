package acceptedstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
)

func TestLocalUnverifiedAttemptPersistsAndIsNeverSuccess(t *testing.T) {
	s, revision := pinnedLocalStore(t)
	pin, err := s.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "beta")
	if err != nil {
		t.Fatal(err)
	}
	record, err := pin.RecordLocalUnverifiedAttempt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Outcome != "unverified" || record.Operation != "selected-workload" ||
		record.RevisionID != revision.ID || record.Generation != revision.Generation ||
		record.Workload != "beta" || len(record.ID) != 32 {
		t.Fatalf("unexpected local journal identity: %+v", record)
	}
	other, err := pin.RecordLocalUnverifiedAttempt(context.Background())
	if err != nil || other.ID == record.ID {
		t.Fatalf("operation IDs reused: %v", err)
	}
	path := filepath.Join(s.root, "operations", record.ID+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("local operation record not a private regular file: %+v %v", info, err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(s.root)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.ReadLocalUnverifiedAttempt(record.ID)
	if err != nil || recovered != record {
		t.Fatalf("record lost across reopen: %+v %v", recovered, err)
	}
	changed := validSpec()
	changed.Workloads[0].ImageFingerprint = strings.Repeat("c", 64)
	if _, err := reopened.CommitLocal(revision.Generation, revision.ID, changed); err != nil {
		t.Fatal(err)
	}
	// An accepted revision change does not erase or magically verify a
	// possibly partial historical operation.
	recovered, err = reopened.ReadLocalUnverifiedAttempt(record.ID)
	if err != nil || recovered.Outcome != "unverified" || recovered.RevisionID != revision.ID {
		t.Fatalf("history inaccurately converged: %+v %v", recovered, err)
	}
}

func TestLocalUnverifiedAttemptRequiresLiveSelectedPin(t *testing.T) {
	s, rev := pinnedLocalStore(t)
	if _, err := (*LocalRevisionPin)(nil).RecordLocalUnverifiedAttempt(context.Background()); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("nil pin created journal: %v", err)
	}
	pin, err := s.PinLocalRevision(context.Background(), rev, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pin.RecordLocalUnverifiedAttempt(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled operation recorded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "operations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled request initialized a journal")
	}
	// A cooperating accepted promotion cannot race across the pin.
	changed := validSpec()
	changed.Workloads[0].ImageFingerprint = strings.Repeat("d", 64)
	if _, err := s.CommitLocal(rev.Generation, rev.ID, changed); !errors.Is(err, filestate.ErrRegistryBusy) {
		t.Fatalf("pin did not exclude same-UID promotion: %v", err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pin.RecordLocalUnverifiedAttempt(context.Background()); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("released pin recorded operation: %v", err)
	}
}

func TestLocalUnverifiedAttemptRejectsUnsafeJournalAndCorruption(t *testing.T) {
	s, rev := pinnedLocalStore(t)
	pin, err := s.PinLocalRevision(context.Background(), rev, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	foreign := t.TempDir()
	dir := filepath.Join(s.root, "operations")
	if err := os.Symlink(foreign, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := pin.RecordLocalUnverifiedAttempt(context.Background()); err == nil {
		t.Fatal("journal followed a symlink")
	}
	if entries, err := os.ReadDir(foreign); err != nil || len(entries) != 0 {
		t.Fatalf("untrusted path changed: %+v %v", entries, err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	record, err := pin.RecordLocalUnverifiedAttempt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape", "", strings.Repeat("0", 64), "A" + record.ID[1:]} {
		if _, err := s.ReadLocalUnverifiedAttempt(bad); err == nil {
			t.Fatalf("read through invalid operation ID %q", bad)
		}
	}
	badRecord := record
	badRecord.Outcome = "verified"
	raw, err := json.Marshal(badRecord)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, record.ID+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadLocalUnverifiedAttempt(record.ID); err == nil {
		t.Fatal("accepted unverified journal as successful outcome")
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := pin.RecordLocalUnverifiedAttempt(context.Background()); err == nil {
		t.Fatal("recorded into nonprivate directory")
	}
}
