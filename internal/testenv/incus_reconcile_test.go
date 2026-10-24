package testenv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIncusReconcileOnlyExpiresOwnedVMs(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, err := NewIncusVMBackend(client, "testenv")
	if err != nil { t.Fatal(err) }
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	backend.now = func() time.Time { return now }
	expired, err := backend.Create(ctx, admittedSpec())
	if err != nil { t.Fatal(err) }
	current, err := backend.Create(ctx, admittedSpec())
	if err != nil { t.Fatal(err) }
	active := client.vms[current.Instance]
	active.Config["user.aginctus.testenv.expires"] = now.Add(time.Hour).Format(time.RFC3339Nano)
	client.vms[current.Instance] = active
	client.vms["foreign"] = IncusVMInstance{
		Name: "foreign", Type: "virtual-machine", Status: "Running",
	}
	summary, err := ReconcileExpiredIncusVMs(ctx, backend, now.Add(31*time.Minute))
	if err != nil || summary.Scanned != 2 || summary.Skipped != 1 ||
		summary.Destroyed != 1 || summary.Failed != 0 {
		t.Fatalf("incorrect reconciliation: %#v %v", summary, err)
	}
	if _, ok := client.vms[expired.Instance]; ok { t.Fatal("expired VM not deleted") }
	if _, ok := client.vms[current.Instance]; !ok { t.Fatal("active VM destroyed") }
	if _, ok := client.vms["foreign"]; !ok { t.Fatal("foreign VM deleted") }
}

func TestIncusReconcileRecordsFailedDeletionWithoutFalseSuccess(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := NewIncusVMBackend(client, "testenv")
	now := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	backend.now = func() time.Time { return now }
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil { t.Fatal(err) }
	client.fail = "stop"
	summary, err := ReconcileExpiredIncusVMs(ctx, backend, now.Add(time.Hour))
	if err == nil || summary.Destroyed != 0 || summary.Failed != 1 {
		t.Fatalf("failure marked success: %#v %v", summary, err)
	}
	if _, ok := client.vms[ref.Instance]; !ok { t.Fatal("unverified VM got removed") }
}

func TestIncusReconcileFailsClosedOnForeignOrOverCapacity(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := NewIncusVMBackend(client, "testenv")
	now := time.Now().UTC()
	backend.now = func() time.Time { return now }
	ref, err := backend.Create(ctx, admittedSpec())
	if err != nil { t.Fatal(err) }
	vm := client.vms[ref.Instance]
	vm.Devices["external"] = map[string]string{"type": "disk", "source": "/pilot"}
	client.vms[ref.Instance] = vm
	summary, err := ReconcileExpiredIncusVMs(ctx, backend, now.Add(time.Hour))
	if !errors.Is(err, ErrOwnershipMismatch) || summary.Destroyed != 0 {
		t.Fatalf("malformed owned VM ignored: %#v %v", summary, err)
	}
	if _, ok := client.vms[ref.Instance]; !ok { t.Fatal("unsafe VM deleted") }
	delete(client.vms, ref.Instance)
	for i := 0; i < MaxIncusReconcileCandidates+1; i++ {
		name := fmt.Sprintf("testenv-vm-%024x", i)
		runID := "vm-" + strings.Repeat("a", 23) + string("0123456789abcdef"[i%16])
		client.vms[name] = IncusVMInstance{Name: name, Config: map[string]string{
			"user.aginctus.testenv.run": runID,
		}}
	}
	// Fake entries above intentionally have mismatched name IDs and must
	// fail ownership checks; neither they nor foreign resources may be touched.
	summary, err = ReconcileExpiredIncusVMs(ctx, backend, now.Add(time.Hour))
	if err == nil || summary.Destroyed != 0 {
		t.Fatalf("malformed foreign candidate accepted: %#v %v", summary, err)
	}
}

func TestIncusReconcileRejectsNilClockAndCancellation(t *testing.T) {
	ctx := context.Background()
	client := newFakeIncusClient(t)
	backend, _ := NewIncusVMBackend(client, "testenv")
	if _, err := ReconcileExpiredIncusVMs(ctx, backend, time.Time{}); err == nil {
		t.Fatal("accepted missing trusted wall clock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReconcileExpiredIncusVMs(ctx, backend, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery executed: %v", err)
	}
}
