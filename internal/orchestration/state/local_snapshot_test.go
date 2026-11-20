package acceptedstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedWorkloadSnapshotUsesAcceptedImmutableIntent(t *testing.T) {
	store, rev := pinnedLocalStore(t)
	pin, err := store.PinLocalRevision(context.Background(), rev, "demo", "aginctus", "beta")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	first, err := pin.SnapshotSelectedWorkload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != rev || first.DeploymentID != "demo" ||
		first.IncusProject != "aginctus" || first.Workload.ID != "beta" ||
		first.Workload.InstanceName != "beta" || first.Workload.InstanceType != "container" ||
		first.Workload.ImageFingerprint != strings.Repeat("b", 64) ||
		!first.Workload.EnsureRunning || len(first.Workload.Profiles) != 1 {
		t.Fatalf("unexpected accepted workload snapshot: %+v", first)
	}
	first.Workload.Profiles[0] = "untrusted-mutation"
	second, err := pin.SnapshotSelectedWorkload(context.Background())
	if err != nil || second.Workload.Profiles[0] != "base" {
		t.Fatalf("snapshot slice aliased or changed: %+v %v", second, err)
	}
	active, _, err := store.ReadActive()
	if err != nil || active == nil || *active != rev {
		t.Fatalf("read-only snapshot changed accepted state: %+v %v", active, err)
	}
	if _, err := os.Stat(filepath.Join(store.root, "operations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot unexpectedly created an operation journal")
	}
}

func TestPinnedWorkloadSnapshotRefusesReleasedCanceledAndStaleState(t *testing.T) {
	store, rev := pinnedLocalStore(t)
	pin, err := store.PinLocalRevision(context.Background(), rev, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pin.SnapshotSelectedWorkload(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled snapshot succeeded: %v", err)
	}
	if err := os.Remove(filepath.Join(store.root, "active.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := pin.SnapshotSelectedWorkload(context.Background()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("missing active pointer trusted: %v", err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pin.SnapshotSelectedWorkload(context.Background()); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("released pin returned a workload: %v", err)
	}
	if _, err := (*LocalRevisionPin)(nil).SnapshotSelectedWorkload(context.Background()); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("nil pin accepted: %v", err)
	}
}

func TestPinnedWorkloadSnapshotCannotSelectAnotherWorkload(t *testing.T) {
	store, rev := pinnedLocalStore(t)
	pin, err := store.PinLocalRevision(context.Background(), rev, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	snapshot, err := pin.SnapshotSelectedWorkload(context.Background())
	if err != nil || snapshot.Workload.ID != "alpha" ||
		snapshot.Workload.ImageFingerprint != strings.Repeat("a", 64) ||
		snapshot.Workload.EnsureRunning {
		t.Fatalf("selector escaped pin: %+v %v", snapshot, err)
	}
}
