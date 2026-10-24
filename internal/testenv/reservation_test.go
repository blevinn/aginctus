package testenv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReservationPrecedesCgroupAndHasPrivateIdentity(t *testing.T) {
	store := sessionTestStore(t)
	id, dir, err := store.reserveVMRunWithTTL(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.readVMReservation(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != id || rec.OwnerUID != os.Getuid() ||
		rec.ExpiresAt.Sub(rec.CreatedAt) != time.Minute {
		t.Fatalf("incorrect reservation: %#v", rec)
	}
	info, err := os.Stat(filepath.Join(dir, "reservation.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("reservation not private: %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "run.json")); !os.IsNotExist(err) {
		t.Fatalf("run was bound prematurely: %v", err)
	}
}

func TestPreBindRecoveryGraceThenReclaimsOwnedCgroup(t *testing.T) {
	store, manager, mkdir := recoveryFixture(t)
	id, _, err := store.reserveVMRunWithTTL(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := manager.prepare(id, mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	rec, err := store.readVMReservation(id)
	if err != nil {
		t.Fatal(err)
	}
	early, err := RecoverAbandoned(context.Background(), store, manager, rec.CreatedAt.Add(time.Minute))
	if err != nil || early.Skipped != 1 {
		t.Fatalf("startup within grace should be skipped: %#v, %v", early, err)
	}
	killFile := filepath.Join(manager.root, id, "cgroup.kill")
	before, err := os.ReadFile(killFile)
	if err != nil || len(before) != 0 {
		t.Fatalf("pre-grace attempted cgroup kill: %q %v", before, err)
	}
	late, err := RecoverAbandoned(context.Background(), store, manager, rec.CreatedAt.Add(unboundVMGrace+time.Second))
	// Fake control files prevent rmdir, so this is intentionally NOT
	// recorded as clean even though cgroup.kill was invoked.
	if err == nil || late.Failed != 1 || late.Recovered != 0 {
		t.Fatalf("incomplete fake cleanup was attested: %#v %v", late, err)
	}
	after, err := os.ReadFile(killFile)
	if err != nil || string(after) != "1" {
		t.Fatalf("unbound cgroup was not killed: %q %v", after, err)
	}
	data, err := os.ReadFile(filepath.Join(store.root, id, "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt RecoveryReceipt
	if err := json.Unmarshal(data, &receipt); err != nil ||
		receipt.Reason != "startup_unbound" || receipt.Cgroup != "not_verified" {
		t.Fatalf("incorrect orphan receipt: %s %v", data, err)
	}
}

func TestReservationCannotOverrideMalformedBoundRecord(t *testing.T) {
	store, manager, mkdir := recoveryFixture(t)
	id, dir, err := store.reserveVMRunWithTTL(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := manager.prepare(id, mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "run.json")); err != nil {
		t.Fatal(err)
	}
	rec, err := store.readVMReservation(id)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RecoverAbandoned(context.Background(), store, manager, rec.CreatedAt.Add(unboundVMGrace+time.Second))
	if err == nil || out.Failed != 1 {
		t.Fatalf("symlinked run.json bypassed ownership: %#v %v", out, err)
	}
	b, err := os.ReadFile(filepath.Join(manager.root, id, "cgroup.kill"))
	if err != nil || len(b) != 0 {
		t.Fatalf("tampered record killed VM: %q %v", b, err)
	}
}

func TestReservationTamperingAndLifetimeRejected(t *testing.T) {
	store := sessionTestStore(t)
	if _, _, err := store.reserveVMRunWithTTL(0); err == nil {
		t.Fatal("accepted zero TTL")
	}
	id, dir, err := store.reserveVMRunWithTTL(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "reservation.json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", file); err != nil {
		t.Fatal(err)
	}
	if _, err := store.readVMReservation(id); err == nil {
		t.Fatal("accepted symlinked reservation")
	}
}
