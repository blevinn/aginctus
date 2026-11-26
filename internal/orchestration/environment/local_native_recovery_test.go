package environment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
)

func TestLocalNativeRecoveryRequiresExactVerifiedHandoff(t *testing.T) {
	ctx := t.Context()
	s, c := newLocalProjectFixture(t)
	d := c.Environment
	rev, err := s.CommitLocalCandidate(ctx, d, d.Binding, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := s.PinLocalRevision(ctx, d, rev)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Close()
	j, err := pin.NewLocalNativeJournal()
	if err != nil {
		t.Fatal(err)
	}
	if err := j.BeginEffect(ctx, "beta", "beta", "start"); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkApplied(); err != nil {
		t.Fatal(err)
	}
	// An open operation still blocks even if its last submission was applied.
	if err := pin.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.root, "environments", d.ID, "native-operations", j.ID()+".json")
	old := time.Unix(1, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	verify := func(_ context.Context, req LocalNativeRecoveryRequest) (Binding, error) {
		if req.Environment != d || req.CurrentRevision != rev || req.Operation.ID != j.ID() ||
			req.Operation.Revision != rev || req.Operation.Status != LocalNativeOpen ||
			len(req.Operation.Effects) != 1 || req.Operation.Effects[0].Outcome != LocalNativeApplied {
			t.Fatal("verification did not receive the exact operation")
		}
		if _, err := s.PinLocalRevision(ctx, d, rev); !errors.Is(err, filestate.ErrRegistryBusy) {
			t.Fatalf("recovery did not hold the writer lock: %v", err)
		}
		return d.Binding, nil
	}
	if _, err := s.PinLocalRevision(ctx, d, rev); !errors.Is(err, ErrLocalNativeUnresolved) {
		t.Fatalf("old timestamp or applied submission granted takeover: %v", err)
	}
	if err := s.ResolveLocalNativeOperation(ctx, d, rev, j.ID(), func(context.Context, LocalNativeRecoveryRequest) (Binding, error) {
		return Binding{}, errors.New("terminal status unknown")
	}); err == nil {
		t.Fatal("denied observer resolved the operation")
	}
	if _, err := s.PinLocalRevision(ctx, d, rev); !errors.Is(err, ErrLocalNativeUnresolved) {
		t.Fatalf("denied recovery allowed writer: %v", err)
	}
	if err := s.ResolveLocalNativeOperation(ctx, d, rev, j.ID(), verify); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalProjectStateStore(s.root, d.Binding)
	if err != nil {
		t.Fatal(err)
	}
	next, err := reopened.PinLocalRevision(ctx, d, rev)
	if err != nil {
		t.Fatalf("verified recovery receipt did not survive reopen: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	still, err := reopened.ReadLocalNativeOperation(ctx, d, j.ID())
	if err != nil || still.Status != LocalNativeOpen || still.Effects[0].Outcome != LocalNativeApplied {
		t.Fatal("recovery changed the historical operation")
	}
	changed := c
	changed.Environment.Name = "after-recovery"
	newRev, err := reopened.CommitLocalCandidate(ctx, d, d.Binding, &rev, changed)
	if err != nil {
		t.Fatalf("verified recovery did not release CAS: %v", err)
	}
	receipt := filepath.Join(filepath.Dir(path), j.ID()+".recovered")
	if err := os.WriteFile(receipt, []byte("wrong journal digest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.PinLocalRevision(ctx, d, newRev); !errors.Is(err, ErrLocalNativeUnresolved) {
		t.Fatalf("corrupt recovery receipt or changed revision bypassed guard: %v", err)
	}
}
