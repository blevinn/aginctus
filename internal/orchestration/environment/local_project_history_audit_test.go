package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHistoryAuditIndependentEnvironmentsAndRename(t *testing.T) {
	store, a := newLocalProjectFixture(t)
	ctx := context.Background()
	empty, err := store.AuditLocalPublishedHistory(ctx, a.Environment)
	if err != nil || empty.HasAccepted || empty.Reachable != 0 {
		t.Fatalf("empty history: %+v, %v", empty, err)
	}
	first, err := store.CommitLocalCandidate(ctx, a.Environment, a.Environment.Binding, nil, a)
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	b.Environment.Name = "another"
	other, err := store.CommitLocalCandidate(ctx, b.Environment, b.Environment.Binding, nil, b)
	if err != nil {
		t.Fatal(err)
	}
	next := a
	next.Environment.Name = "renamed"
	second, err := store.CommitLocalCandidate(ctx, next.Environment, next.Environment.Binding, &first, next)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalProjectStateStore(store.root, store.binding)
	if err != nil {
		t.Fatal(err)
	}
	history, err := reopened.AuditLocalPublishedHistory(ctx, next.Environment)
	if err != nil || history.Generation != 2 || history.Reachable != 2 || history.HeadID != second.ID {
		t.Fatalf("published chain: %+v, %v", history, err)
	}
	independent, err := reopened.AuditLocalPublishedHistory(ctx, b.Environment)
	if err != nil || independent.Generation != 1 || independent.Reachable != 1 || independent.HeadID != other.ID {
		t.Fatalf("other environment chain: %+v, %v", independent, err)
	}
	if _, err := reopened.AuditLocalPublishedHistory(ctx, Descriptor{
		ID: a.Environment.ID, Name: a.Environment.Name, Binding: Binding{
			ServerID: a.Environment.Binding.ServerID, Project: "foreign",
		},
	}); err == nil {
		t.Fatal("audited a foreign project")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := reopened.AuditLocalPublishedHistory(cancelled, a.Environment); err == nil {
		t.Fatal("canceled audit reported success")
	}
}

func TestLocalHistoryAuditRejectsTamperedParentAndPreservesPointer(t *testing.T) {
	store, a := newLocalProjectFixture(t)
	ctx := context.Background()
	first, err := store.CommitLocalCandidate(ctx, a.Environment, a.Environment.Binding, nil, a)
	if err != nil {
		t.Fatal(err)
	}
	changed := a
	changed.Environment.Name = "next"
	second, err := store.CommitLocalCandidate(ctx, changed.Environment, changed.Environment.Binding, &first, changed)
	if err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(store.root, "environments", a.Environment.ID, "revisions", first.ID+".json")
	if err := os.WriteFile(parentPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuditLocalPublishedHistory(ctx, a.Environment); err == nil {
		t.Fatal("published chain with corrupt parent passed audit")
	}
	active, _, err := store.ReadLocalAccepted(ctx, a.Environment)
	if err != nil || active == nil || active.ID != second.ID {
		t.Fatalf("read-only audit changed active pointer: %v, %+v", err, active)
	}
}
