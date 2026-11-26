package testenv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDurableRecoveryRechecksAbsentInstance(t *testing.T) {
	ctx := context.Background()
	c := &acceptanceFake{fakeIncusVMClient: newFakeIncusClient(t), lingeringVolume: true}
	b, err := newTestIncusBackend(c, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := b.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	now := ref.Policy.ExpiresAt
	first, err := ReconcileExpiredIncusVMsDurable(ctx, b, now, dir)
	if err == nil || first.Destroyed != 1 || first.PendingCleanup != 1 || first.RootVolumesUnverified != 1 {
		t.Fatalf("first sweep: %+v %v", first, err)
	}
	c.calls = nil
	second, err := ReconcileExpiredIncusVMsDurable(ctx, b, now, dir)
	if err == nil || second.Destroyed != 0 || second.PendingCleanup != 1 || second.RecoveredPending != 0 {
		t.Fatalf("pending volume forgotten: %+v %v", second, err)
	}
	c.lingeringVolume = false
	third, err := ReconcileExpiredIncusVMsDurable(ctx, b, now, dir)
	if err != nil || third.Destroyed != 0 || third.PendingCleanup != 0 || third.RecoveredPending != 1 {
		t.Fatalf("recheck: %+v %v", third, err)
	}
	for _, call := range c.calls {
		if call == "stop" || call == "delete" {
			t.Fatal("pending reference authorized mutation")
		}
	}
}

func TestDurableRecoveryRefusesTamperedLedgerBeforeMutation(t *testing.T) {
	ctx := context.Background()
	c := &acceptanceFake{fakeIncusVMClient: newFakeIncusClient(t)}
	b, _ := newTestIncusBackend(c, "testenv")
	ref, err := b.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := &recoveryLedger{SchemaVersion: 1, Pending: []VMRef{ref}, path: filepath.Join(dir, "pending.json")}
	ledger.Pending[0].PolicyMAC = strings.Repeat("0", 64)
	if err := ledger.save(); err != nil {
		t.Fatal(err)
	}
	c.calls = nil
	summary, err := ReconcileExpiredIncusVMsDurable(ctx, b, ref.Policy.ExpiresAt, dir)
	if err == nil || summary.Destroyed != 0 || len(c.calls) != 0 {
		t.Fatalf("unsafe ledger admitted: %+v %v %v", summary, err, c.calls)
	}
}

func TestDurableRecoveryDoesNotAdoptForeignInstanceAtPendingName(t *testing.T) {
	ctx := context.Background()
	c := &acceptanceFake{fakeIncusVMClient: newFakeIncusClient(t), lingeringVolume: true}
	b, _ := newTestIncusBackend(c, "testenv")
	ref, err := b.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileExpiredIncusVMsDurable(ctx, b, ref.Policy.ExpiresAt, dir); err == nil {
		t.Fatal("expected unresolved volume")
	}
	c.vms[ref.Instance] = IncusVMInstance{Name: ref.Instance, Type: "virtual-machine", Status: "Stopped"}
	c.calls = nil
	summary, err := ReconcileExpiredIncusVMsDurable(ctx, b, ref.Policy.ExpiresAt, dir)
	if err == nil || summary.Destroyed != 0 || len(c.vms) != 1 {
		t.Fatalf("foreign resource adopted: %+v %v", summary, err)
	}
	for _, call := range c.calls {
		if call == "stop" || call == "delete" {
			t.Fatal("foreign resource mutated")
		}
	}
}
