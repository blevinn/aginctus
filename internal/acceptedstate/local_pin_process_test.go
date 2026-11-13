package acceptedstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
)

// Run only as a subprocess launched by TestLocalPinCrossProcessLocking.
// A distinct process is essential: a goroutine-only test cannot establish
// the advisory flock semantics used by separate cooperating CLI processes.
func TestLocalPinSubprocess(t *testing.T) {
	root := os.Getenv("AGINCTUS_TEST_LOCAL_PIN_ROOT")
	if root == "" {
		return
	}
	store, err := OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := store.ReadActive()
	if err != nil || revision == nil {
		t.Fatalf("read child accepted revision: %v", err)
	}
	pin, err := store.PinLocalRevision(context.Background(), *revision, "demo", "aginctus", "alpha")
	switch {
	case errors.Is(err, filestate.ErrRegistryBusy):
		t.Log("PIN_BUSY")
	case err != nil:
		t.Fatalf("unexpected child pin error: %v", err)
	default:
		t.Log("PIN_OK")
		if err := pin.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalPinCrossProcessLocking(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	runChild := func(want string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestLocalPinSubprocess$", "-test.v")
		cmd.Env = append(os.Environ(), "AGINCTUS_TEST_LOCAL_PIN_ROOT="+store.root)
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), want) {
			t.Fatalf("child want %s, got %q (%v)", want, output, err)
		}
	}
	pin, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	runChild("PIN_BUSY")
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	runChild("PIN_OK")
	// Child release does not remove or replace the persistent lock inode.
	lock := filepath.Join(store.root, "active.json.flock")
	if info, err := os.Lstat(lock); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("persistent advisory lock missing: %v", err)
	}
	if reacquired, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha"); err != nil {
		t.Fatalf("cannot reacquire pin after child exit: %v", err)
	} else {
		reacquired.Close()
	}
}
