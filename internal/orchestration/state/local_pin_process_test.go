package acceptedstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	revision, spec, err := store.ReadActive()
	if err != nil || revision == nil {
		t.Fatalf("read child accepted revision: %v", err)
	}
	if os.Getenv("AGINCTUS_TEST_LOCAL_PIN_MODE") == "commit" {
		spec.Workloads[0].ImageFingerprint = strings.Repeat("e", 64)
		_, err := store.CommitLocal(revision.Generation, revision.ID, *spec)
		switch {
		case errors.Is(err, filestate.ErrRegistryBusy):
			t.Log("COMMIT_BUSY")
		case err != nil:
			t.Fatalf("unexpected child commit error: %v", err)
		default:
			t.Log("COMMIT_OK")
		}
		return
	}
	pin, err := store.PinLocalRevision(context.Background(), *revision, "demo", "aginctus", "alpha")
	switch {
	case errors.Is(err, filestate.ErrRegistryBusy):
		t.Log("PIN_BUSY")
	case err != nil:
		t.Fatalf("unexpected child pin error: %v", err)
	default:
		if os.Getenv("AGINCTUS_TEST_LOCAL_PIN_MODE") == "exit" {
			t.Log("PIN_EXIT")
			// Deliberately bypass Close and deferred cleanup. Process exit must
			// release the flock without unlinking its persistent inode.
			os.Exit(23)
		}
		t.Log("PIN_OK")
		if err := pin.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLocalPinCrossProcessLocking(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	runChild := func(mode, want string, exitCode int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalPinSubprocess$", "-test.v")
		cmd.Env = append(os.Environ(), "AGINCTUS_TEST_LOCAL_PIN_ROOT="+store.root, "AGINCTUS_TEST_LOCAL_PIN_MODE="+mode)
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != exitCode || !strings.Contains(string(output), want) {
			t.Fatalf("child want %s (exit %d), got %q (%v, context %v)", want, exitCode, output, err, ctx.Err())
		}
	}
	lock := filepath.Join(store.root, "active.json.flock")
	original, err := os.Lstat(lock)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	runChild("pin", "PIN_BUSY", 0)
	runChild("commit", "COMMIT_BUSY", 0)
	if err := pin.Check(context.Background()); err != nil {
		t.Fatalf("child changed pinned accepted state: %v", err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	runChild("pin", "PIN_OK", 0)
	runChild("exit", "PIN_EXIT", 23)
	runChild("commit", "COMMIT_OK", 0)
	next, _, err := store.ReadActive()
	if err != nil || next == nil || next.Generation != revision.Generation+1 || next.ParentID != revision.ID {
		t.Fatalf("post-release child promotion: %+v %v", next, err)
	}
	if stale, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha"); !errors.Is(err, ErrRevisionConflict) || stale != nil {
		if stale != nil {
			stale.Close()
		}
		t.Fatalf("stale parent pin accepted after child promotion: %v", err)
	}
	// Child release does not remove or replace the persistent lock inode.
	if info, err := os.Lstat(lock); err != nil || !info.Mode().IsRegular() || !os.SameFile(original, info) {
		t.Fatalf("persistent advisory lock missing or replaced: %v", err)
	}
	if reacquired, err := store.PinLocalRevision(context.Background(), *next, "demo", "aginctus", "alpha"); err != nil {
		t.Fatalf("cannot reacquire pin after child exit: %v", err)
	} else {
		if err := reacquired.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
