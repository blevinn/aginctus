package testenv

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIncusReconcileOnlyExpiresOwnedVMs(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, err := newTestIncusBackend(client, "testenv")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	backend.now = func() time.Time { return now }
	expired, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	backend.now = func() time.Time { return now.Add(31 * time.Minute) }
	current, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	client.vms["foreign"] = IncusVMInstance{
		Name: "foreign", Type: "virtual-machine", Status: "Running",
	}
	summary, err := ReconcileExpiredIncusVMs(ctx, backend, now.Add(31*time.Minute))
	if err != nil || summary.Scanned != 2 || summary.Skipped != 1 ||
		summary.Destroyed != 1 || summary.Failed != 0 {
		t.Fatalf("incorrect reconciliation: %#v %v", summary, err)
	}
	if _, ok := client.vms[expired.Instance]; ok {
		t.Fatal("expired VM not deleted")
	}
	if _, ok := client.vms[current.Instance]; !ok {
		t.Fatal("active VM destroyed")
	}
	if _, ok := client.vms["foreign"]; !ok {
		t.Fatal("foreign VM deleted")
	}
}

func TestIncusReconcileRecordsFailedDeletionWithoutFalseSuccess(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	backend.now = func() time.Time { return now }
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	client.fail = "stop"
	summary, err := ReconcileExpiredIncusVMs(ctx, backend, now.Add(time.Hour))
	if err == nil || summary.Destroyed != 0 || summary.Failed != 1 {
		t.Fatalf("failure marked success: %#v %v", summary, err)
	}
	if _, ok := client.vms[ref.Instance]; !ok {
		t.Fatal("unverified VM got removed")
	}
}

func TestIncusReconcileFailsClosedOnForeignOrOverCapacity(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	now := time.Now().UTC()
	backend.now = func() time.Time { return now }
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil {
		t.Fatal(err)
	}
	vm := client.vms[ref.Instance]
	vm.Devices["external"] = map[string]string{"type": "disk", "source": "/pilot"}
	client.vms[ref.Instance] = vm
	summary, err := ReconcileExpiredIncusVMs(ctx, backend, now.Add(time.Hour))
	if !errors.Is(err, ErrOwnershipMismatch) || summary.Destroyed != 0 {
		t.Fatalf("malformed owned VM ignored: %#v %v", summary, err)
	}
	if _, ok := client.vms[ref.Instance]; !ok {
		t.Fatal("unsafe VM deleted")
	}
	delete(client.vms, ref.Instance)
	for i := 0; i < MaxIncusReconcileCandidates+1; i++ {
		if _, err := backend.Create(ctx, admittedSpec()); err != nil {
			t.Fatal(err)
		}
	}
	summary, err = ReconcileExpiredIncusVMs(ctx, backend, now.Add(2*time.Hour))
	if err == nil || summary.Destroyed != 0 || summary.Scanned != 0 {
		t.Fatalf("over-capacity scan modified resources: %#v %v", summary, err)
	}
}

func TestIncusReconcileRejectsNilClockAndCancellation(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := newTestIncusBackend(client, "testenv")
	if _, err := ReconcileExpiredIncusVMs(ctx, backend, time.Time{}); err == nil {
		t.Fatal("accepted missing trusted wall clock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReconcileExpiredIncusVMs(ctx, backend, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery executed: %v", err)
	}
}
