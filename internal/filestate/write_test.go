package filestate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePrivateFileReplacesContentWithPrivateMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := WritePrivateFile(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivateFile(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second\n" {
		t.Fatalf("content = %q, want %q", data, "second\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %#o, want 0600", perm)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory holds %v, want only the target file", names)
	}
}

func TestWritePrivateFileReplacesSymlinkInsteadOfTarget(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.json")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivateFile(path, []byte("private")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "untouched" {
		t.Fatalf("link target = %q (err %v), want it untouched", data, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("mode = %v, want a regular file replacing the symlink", info.Mode())
	}
}

func TestWritePrivateFileRejectsRelativePath(t *testing.T) {
	err := WritePrivateFile("relative/state.json", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %v, want a relative-path rejection", err)
	}
}

func TestWritePrivateFileReportsMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")
	err := WritePrivateFile(path, []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "create temporary file") {
		t.Fatalf("error = %v, want a temporary-file creation failure", err)
	}
}
