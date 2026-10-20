package filestate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStateRootPrefersAbsoluteXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	root, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != state {
		t.Fatalf("root = %q, want %q", root, state)
	}
}

func TestStateRootRejectsRelativeXDGStateHome(t *testing.T) {
	for _, value := range []string{"relative/path", ".", "state"} {
		t.Setenv("XDG_STATE_HOME", value)
		if _, err := StateRoot(); err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("XDG_STATE_HOME=%q: error = %v, want an absolute-path rejection", value, err)
		}
	}
}

func TestStateRootFallsBackToXDGDefaultUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	root, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".local", "state"); root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
}

func TestStateRootRejectsRelativeHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "relative/home")
	if _, err := StateRoot(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %v, want an absolute-path rejection", err)
	}
}
