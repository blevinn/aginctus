package testenv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recoveryFixture(t *testing.T) (*RunStore, *CgroupManager, func(string, os.FileMode) error) {
	t.Helper()
	store := sessionTestStore(t)
	manager, mkdir := fakeCgroupFilesystem(t)
	return store, manager, mkdir
}

func TestRecoverySkipsLiveOwnedVM(t *testing.T) {
	store, manager, mkdir := recoveryFixture(t)
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := manager.prepare(rec.ID, mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	summary, err := RecoverAbandoned(context.Background(), store, manager, time.Now())
	if err != nil || summary.Skipped != 1 || summary.Recovered != 0 || summary.Failed != 0 {
		t.Fatalf("active process was not preserved: %#v %v", summary, err)
	}
	contents, err := os.ReadFile(filepath.Join(manager.root, rec.ID, "cgroup.kill"))
	if err != nil || len(contents) != 0 {
		t.Fatalf("active cgroup was targeted: %q %v", contents, err)
	}
}

func TestRecoveryKillsOnlyExpiredRunCgroup(t *testing.T) {
	store, manager, mkdir := recoveryFixture(t)
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := manager.prepare(rec.ID, mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	foreign := filepath.Join(manager.root, "do-not-delete")
	if err := os.WriteFile(foreign, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	// Pretend the TTL passed. Fake cgroup filesystem cannot rmdir while
	// its control files remain, so recovery must report NOT VERIFIED.
	summary, err := RecoverAbandoned(context.Background(), store, manager, rec.ExpiresAt.Add(time.Second))
	if err == nil || summary.Recovered != 0 || summary.Failed != 1 {
		t.Fatalf("fake cgroup removal was inaccurately attested: %#v %v", summary, err)
	}
	kill, err := os.ReadFile(filepath.Join(manager.root, rec.ID, "cgroup.kill"))
	if err != nil || string(kill) != "1" {
		t.Fatalf("expired run not killed: %q %v", kill, err)
	}
	original, err := os.ReadFile(foreign)
	if err != nil || string(original) != "preserve" {
		t.Fatalf("recovery modified foreign file: %q %v", original, err)
	}
	receipt, err := os.ReadFile(filepath.Join(store.root, rec.ID, "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record RecoveryReceipt
	if json.Unmarshal(receipt, &record) != nil || record.Cgroup != "not_verified" ||
		record.Cleanup != "not_verified" || record.Reason != "expired" {
		t.Fatalf("untruthful recovery record: %s", receipt)
	}
}

func TestRecoveryRefusesTamperedRunAndSymlink(t *testing.T) {
	store, manager, mkdir := recoveryFixture(t)
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := manager.prepare(rec.ID, mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	manifest := filepath.Join(store.root, rec.ID, "run.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", manifest); err != nil {
		t.Fatal(err)
	}
	summary, err := RecoverAbandoned(context.Background(), store, manager, rec.ExpiresAt.Add(time.Second))
	if err == nil || summary.Failed != 1 {
		t.Fatalf("tampered run record was accepted: %#v %v", summary, err)
	}
	kill, err := os.ReadFile(filepath.Join(manager.root, rec.ID, "cgroup.kill"))
	if err != nil || len(kill) != 0 {
		t.Fatalf("untrusted record triggered a kill: %q %v", kill, err)
	}
}

func TestRecoverySkipsCompletedSessionWithoutCgroup(t *testing.T) {
	store, manager, _ := recoveryFixture(t)
	rec, err := store.RegisterRun(os.Getpid(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(store.root, rec.ID, "session.json")
	if err := os.WriteFile(receipt, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	summary, err := RecoverAbandoned(context.Background(), store, manager, rec.ExpiresAt.Add(time.Second))
	if err != nil || summary.Skipped != 1 || summary.Recovered != 0 {
		t.Fatalf("completed session treated as orphan: %#v %v", summary, err)
	}
}

func TestRecoveryScanIsBounded(t *testing.T) {
	store, manager, _ := recoveryFixture(t)
	for i := 0; i < maxRecoveryCandidates+1; i++ {
		name := "vm-" + strings.Repeat("a", 22) + string(rune('a'+i/16)) + string(rune('a'+i%16))
		if err := os.Mkdir(filepath.Join(store.root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := RecoverAbandoned(context.Background(), store, manager, time.Now())
	if err == nil || summary.Scanned != 0 {
		t.Fatalf("unbounded recovery was accepted: %#v %v", summary, err)
	}
}
