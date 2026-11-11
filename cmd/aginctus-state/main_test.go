package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/blevinn/aginctus/internal/acceptedstate"
)

func TestRunRequiresPreinitializedLocalState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, "", ""); err == nil {
		t.Fatal("missing arguments accepted")
	}
	base := t.TempDir()
	if err := run(ctx, filepath.Join(base, "missing"), filepath.Join(base, "a.sock")); err == nil {
		t.Fatal("auto-initialized missing accepted-state directory")
	}
}

func TestRunDoesNotEscalateOrWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	if _, err := acceptedstate.InitializeLocalStore(root); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, root, filepath.Join(root, "state.sock")); err != nil {
		t.Fatal(err)
	}
	opened, err := acceptedstate.OpenLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if rev, _, err := opened.ReadActive(); err != nil || rev != nil {
		t.Fatal("server initialized accepted revision")
	}
}
