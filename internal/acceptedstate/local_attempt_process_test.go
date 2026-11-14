package acceptedstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This helper only runs in a separate test-binary process selected by the
// parent. It intentionally exits without releasing its accepted-revision pin,
// exercising kernel flock cleanup and immutable record recovery.
func TestLocalUnverifiedAttemptChildExit(t *testing.T) {
	if os.Getenv("AGINCTUS_ATTEMPT_CHILD") != "1" {
		return
	}
	root := os.Getenv("AGINCTUS_ATTEMPT_STATE")
	store, err := OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rev, _, err := store.ReadActive()
	if err != nil || rev == nil {
		t.Fatal("child cannot load accepted revision")
	}
	pin, err := store.PinLocalRevision(context.Background(), *rev, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := pin.RecordLocalUnverifiedAttempt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, rec.ID)
	os.Exit(0) // Deliberately no pin.Close: emulate abrupt process death.
}

func TestLocalUnverifiedAttemptSurvivesAbruptWriterExit(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalUnverifiedAttemptChildExit$")
	cmd.Env = append(os.Environ(), "AGINCTUS_ATTEMPT_CHILD=1", "AGINCTUS_ATTEMPT_STATE="+store.root)
	out, err := cmd.CombinedOutput()
	if err != nil || ctx.Err() != nil {
		t.Fatalf("crash simulation did not finish: %v (%v): %s", err, ctx.Err(), out)
	}
	id := strings.TrimSpace(string(out))
	if !localAttemptID.MatchString(id) {
		t.Fatalf("child returned no valid operation ID: %q", id)
	}
	restarted, err := OpenLocalStore(store.root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.ReadLocalUnverifiedAttempt(id)
	if err != nil || got.Outcome != "unverified" ||
		got.RevisionID != revision.ID || got.Workload != "alpha" {
		t.Fatalf("lost or misclassified crashed operation: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(store.root, "operations", id+".json")); err != nil {
		t.Fatal(err)
	}
	// The crashed process's advisory CAS pin must have been released by
	// the kernel; restarting can open a new pin but cannot claim the old
	// operation succeeded or silently retry it.
	next, err := restarted.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatalf("pin stuck after process exit: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	if gotAgain, err := restarted.ReadLocalUnverifiedAttempt(id); err != nil ||
		gotAgain.Outcome != "unverified" {
		t.Fatalf("restart changed historical outcome: %+v %v", gotAgain, err)
	}
}

func TestLocalUnverifiedAttemptDoesNotClaimForeignState(t *testing.T) {
	store, rev := pinnedLocalStore(t)
	if _, err := store.PinLocalRevision(context.Background(), rev, "demo", "foreign", "alpha"); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("foreign selection accepted: %v", err)
	}
	if _, err := store.ReadLocalUnverifiedAttempt(strings.Repeat("0", 32)); !errors.Is(err, os.ErrNotExist) {
		// Before any attempt, the operations directory is absent, not a
		// successful or empty convergence record.
		t.Fatalf("missing journal was not reported as absent: %v", err)
	}
}
