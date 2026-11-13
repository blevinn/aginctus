package acceptedstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
)

func pinnedLocalStore(t *testing.T) (*LocalStore, Revision) {
	t.Helper()
	s, err := InitializeLocalStore(filepath.Join(t.TempDir(), "accepted"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CommitLocal(0, "", validSpec())
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}

func TestLocalRevisionPinExcludesCooperatingPromotion(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	pin, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	if err := pin.Check(context.Background()); err != nil {
		t.Fatalf("valid pin rejected: %v", err)
	}
	changed := validSpec()
	changed.Workloads[0].ImageFingerprint = strings.Repeat("e", 64)
	if _, err := store.CommitLocal(revision.Generation, revision.ID, changed); !errors.Is(err, filestate.ErrRegistryBusy) {
		t.Fatalf("concurrent accepted promotion not blocked: %v", err)
	}
	if err := pin.Check(context.Background()); err != nil {
		t.Fatalf("pin invalidated without accepted promotion: %v", err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pin.Check(context.Background()); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("released pin remained usable: %v", err)
	}
	next, err := store.CommitLocal(revision.Generation, revision.ID, changed)
	if err != nil || next.Generation != 2 {
		t.Fatalf("post-release promotion: %+v %v", next, err)
	}
	if _, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale pin accepted: %v", err)
	}
}

func TestLocalRevisionPinRejectsUnselectedAndUntrustedScopes(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	cases := []struct{ deployment, project, workload string }{
		{"", "aginctus", "alpha"},
		{"demo", "", "alpha"},
		{"demo", "default", "alpha"},
		{"demo", "foreign", "alpha"},
		{"foreign", "aginctus", "alpha"},
		{"demo", "aginctus", "not-selected"},
		{"demo", "aginctus", "../alpha"},
	}
	for _, tc := range cases {
		pin, err := store.PinLocalRevision(context.Background(), revision, tc.deployment, tc.project, tc.workload)
		if !errors.Is(err, ErrLocalPinScope) || pin != nil {
			if pin != nil {
				pin.Close()
			}
			t.Errorf("scope %+v granted: pin=%v err=%v", tc, pin, err)
		}
	}
	invalid := revision
	invalid.ID = strings.Repeat("f", 64)
	if pin, err := store.PinLocalRevision(context.Background(), invalid, "demo", "aginctus", "alpha"); !errors.Is(err, ErrLocalPinScope) || pin != nil {
		t.Fatalf("invalid self-claimed revision accepted: %+v %v", pin, err)
	}
	if _, err := (*LocalStore)(nil).PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha"); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("nil store accepted: %v", err)
	}
}

func TestLocalRevisionPinFailsClosedOnCancelCorruptionAndPrivateState(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if pin, err := store.PinLocalRevision(ctx, revision, "demo", "aginctus", "alpha"); !errors.Is(err, context.Canceled) || pin != nil {
		t.Fatalf("canceled pin acquired: %+v %v", pin, err)
	}
	pin, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "beta")
	if err != nil {
		t.Fatal(err)
	}
	if err := pin.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled check passed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.root, "active.json"), []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pin.Check(context.Background()); err == nil {
		t.Fatal("accepted corruption under pin")
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha"); err == nil {
		t.Fatal("accepted nonprivate state directory")
	}
}

func TestLocalRevisionPinFailedAcquisitionReleasesLock(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	changed := validSpec()
	changed.Workloads[0].ImageFingerprint = strings.Repeat("e", 64)
	next, err := store.CommitLocal(revision.Generation, revision.ID, changed)
	if err != nil {
		t.Fatal(err)
	}
	// Both failures occur after acquiring the lock, unlike malformed inputs.
	for _, tc := range []struct {
		name     string
		expected Revision
		workload string
		want     error
	}{
		{"stale", revision, "alpha", ErrRevisionConflict},
		{"unselected", next, "not-selected", ErrLocalPinScope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if pin, err := store.PinLocalRevision(context.Background(), tc.expected, "demo", "aginctus", tc.workload); pin != nil || !errors.Is(err, tc.want) {
				t.Fatalf("failed pin: %v %v", pin, err)
			}
			pin, err := store.PinLocalRevision(context.Background(), next, "demo", "aginctus", "alpha")
			if err != nil {
				t.Fatalf("failed acquisition leaked lock: %v", err)
			}
			if err := pin.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalRevisionPinRejectsMissingActiveAndRechecksValidReplacement(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	pin, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	// A same-UID noncooperating writer is outside the exclusion guarantee.
	// Still fail closed if it removes the active pointer or publishes another
	// otherwise valid revision before Check, rather than trusting cached state.
	if err := os.Remove(filepath.Join(store.root, "active.json")); err != nil {
		t.Fatal(err)
	}
	if err := pin.Check(context.Background()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("missing active pointer accepted: %v", err)
	}
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("pin acquired without active revision: %v", err)
	}
	// Restore a valid active pointer through the normal CAS path.
	if _, err := store.CommitLocal(0, "", validSpec()); err != nil {
		t.Fatal(err)
	}
	pin, err = store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	changed := validSpec()
	changed.Workloads[0].ImageFingerprint = strings.Repeat("e", 64)
	next, err := BuildRevision(&revision, changed)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := changed.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(storedRevision{Revision: next, Spec: canonical})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeImmutableRecord(filepath.Join(store.root, "revisions"), next.ID+".json", record); err != nil {
		t.Fatal(err)
	}
	pointer, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicPrivatePointer(store.root, "active.json", pointer); err != nil {
		t.Fatal(err)
	}
	if err := pin.Check(context.Background()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("valid replacement revision accepted under old pin: %v", err)
	}
}

func TestLocalRevisionPinConcurrentCheckAndClose(t *testing.T) {
	store, revision := pinnedLocalStore(t)
	pin, err := store.PinLocalRevision(context.Background(), revision, "demo", "aginctus", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(closePin bool) {
			defer wg.Done()
			<-start
			if closePin {
				if err := pin.Close(); err != nil {
					t.Errorf("concurrent Close: %v", err)
				}
			} else if err := pin.Check(context.Background()); err != nil && !errors.Is(err, ErrLocalPinScope) {
				t.Errorf("concurrent Check: %v", err)
			}
		}(i%2 == 0)
	}
	close(start)
	wg.Wait()
	if err := pin.Check(context.Background()); !errors.Is(err, ErrLocalPinScope) {
		t.Fatalf("closed pin remained usable: %v", err)
	}
	changed := validSpec()
	changed.Workloads[0].ImageFingerprint = strings.Repeat("e", 64)
	if _, err := store.CommitLocal(revision.Generation, revision.ID, changed); err != nil {
		t.Fatalf("concurrent Close leaked lock: %v", err)
	}
}
