package acceptedstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
