package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareLocalRestoredProjectIndependentHistories(t *testing.T) {
	a, candidate := newLocalProjectFixture(t)
	root := filepath.Join(t.TempDir(), "restored")
	b, err := InitializeLocalProjectStateStore(root, candidate.Environment.Binding)
	if err != nil { t.Fatal(err) }
	ctx := context.Background()
	second := candidate
	second.Environment.ID = "7c82e6d1-1212-4123-8123-0123456789ac"
	second.Environment.Name = "secondary"
	for _, c := range []Candidate{candidate, second} {
		if _, err := a.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c); err != nil { t.Fatal(err) }
		if _, err := b.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c); err != nil { t.Fatal(err) }
	}
	desc := []Descriptor{second.Environment, candidate.Environment}
	status, err := CompareLocalRestoredProject(ctx, a, b, desc)
	if err != nil || len(status.Environments) != 2 ||
		status.Environments[0].EnvironmentID != candidate.Environment.ID ||
		status.Environments[1].EnvironmentID != second.Environment.ID {
		t.Fatalf("restore evidence invalid: %+v %v", status, err)
	}
	if _, err := CompareLocalRestoredProject(ctx, a, a, desc); err == nil {
		t.Fatal("source compared to itself")
	}
	if _, err := CompareLocalRestoredProject(ctx, a, b, []Descriptor{candidate.Environment, candidate.Environment}); err == nil {
		t.Fatal("duplicate environment scope accepted")
	}
	changed := second
	changed.Environment.Name = "changed"
	parent, _, err := b.ReadLocalAccepted(ctx, changed.Environment)
	if err != nil || parent == nil { t.Fatal(err) }
	if _, err := b.CommitLocalCandidate(ctx, changed.Environment, changed.Environment.Binding, parent, changed); err != nil {
		t.Fatal(err)
	}
	if _, err := CompareLocalRestoredProject(ctx, a, b, desc); err == nil {
		t.Fatal("unmatched restored revision passed")
	}
}

func TestCompareLocalRestoreRefusesCorruptionAndForeignProject(t *testing.T) {
	a, c := newLocalProjectFixture(t)
	b, err := InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "second"), c.Environment.Binding)
	if err != nil { t.Fatal(err) }
	ctx := context.Background()
	first, err := a.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c)
	if err != nil { t.Fatal(err) }
	if _, err := b.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c); err != nil { t.Fatal(err) }
	path := filepath.Join(b.root, "environments", c.Environment.ID, "revisions", first.ID + ".json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil { t.Fatal(err) }
	if _, err := CompareLocalRestoredProject(ctx, a, b, []Descriptor{c.Environment}); err == nil {
		t.Fatal("corrupt restored history passed")
	}
	foreign := c.Environment
	foreign.Binding.Project = "other"
	if _, err := CompareLocalRestoredProject(ctx, a, b, []Descriptor{foreign}); err == nil {
		t.Fatal("foreign project accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := CompareLocalRestoredProject(cancelled, a, b, []Descriptor{c.Environment}); err == nil {
		t.Fatal("canceled verification accepted")
	}
}
