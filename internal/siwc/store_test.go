package siwc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedStateRoundTripAndLease(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.read()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.write(state); err != nil {
		t.Fatal(err)
	}
	restored, err := store.read()
	if err != nil || restored.HostID != state.HostID {
		t.Fatalf("identity did not survive restart: %v", err)
	}
	release, err := store.lock(context.Background(), "runtime", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if second, err := store.lock(context.Background(), "runtime", 0); err == nil {
		second()
		t.Fatal("accepted a second credential owner")
	}
	path := filepath.Join(store.Root, "state.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.read(); err == nil {
		t.Fatal("accepted publicly readable credentials")
	}
}

func TestProtectedStoreRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	if err := os.Symlink(parent, filepath.Join(parent, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(filepath.Join(parent, "link")); err == nil {
		t.Fatal("accepted a symlink credential directory")
	}
}
