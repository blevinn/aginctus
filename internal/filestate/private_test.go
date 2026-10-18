package filestate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func writeTestPrivateFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadPrivateFileAcceptsOwnedRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private")
	writeTestPrivateFile(t, path, "test-key")
	data, err := ReadPrivateFile(path, 8)
	if err != nil || string(data) != "test-key" {
		t.Fatalf("read owned private file: %q, %v", data, err)
	}
	if _, err := ReadPrivateFile(path, 7); err == nil {
		t.Fatal("accepted over-limit file")
	}
	if _, err := ReadPrivateFile(path, 0); err == nil {
		t.Fatal("accepted invalid size limit")
	}
	if _, err := ReadPrivateFile("relative", 8); err == nil {
		t.Fatal("accepted relative file path")
	}
	if _, err := ReadPrivateFile(filepath.Join(dir, "missing"), 8); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file must retain not-exist semantics: %v", err)
	}
}

func TestReadPrivateFileRejectsUnsafeFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private")
	writeTestPrivateFile(t, path, "test-key")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path, 32); err == nil {
		t.Fatal("accepted world-readable file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(link, 32); err == nil {
		t.Fatal("accepted final symlink")
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(dangling, 32); err == nil {
		t.Fatal("accepted dangling symlink")
	}
	if _, err := ReadPrivateFile(dir, 32); err == nil {
		t.Fatal("accepted directory in place of file")
	}
	linkedDir := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(dir, linkedDir); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(filepath.Join(linkedDir, "private"), 32); err == nil {
		t.Fatal("accepted symlinked immediate parent")
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path, 32); err == nil {
		t.Fatal("accepted group/world writable parent")
	}
}

func TestReadPrivateFileRejectsSpecialFileWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path, 64); err == nil {
		t.Fatal("accepted named pipe")
	}
}

func TestReadPrivateFileRejectsSymlinkSubstitutionAfterDirectoryOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private")
	writeTestPrivateFile(t, path, "original")
	pinned, err := openTrustedDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	if err := os.Rename(path, filepath.Join(dir, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "original"), path); err != nil {
		t.Fatal(err)
	}
	_, err = readPrivateFileAt(pinned, "private", 64)
	if err == nil {
		t.Fatal("followed symlink substituted after directory was checked")
	}
}

func TestReadPrivateFileUsesPinnedDirectoryAfterPathReplacement(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "trusted")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestPrivateFile(t, filepath.Join(dir, "private"), "original")
	pinned, err := openTrustedDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	moved := filepath.Join(root, "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestPrivateFile(t, filepath.Join(dir, "private"), "replacement")
	got, err := readPrivateFileAt(pinned, "private", 64)
	if err != nil || strings.TrimSpace(string(got)) != "original" {
		t.Fatalf("directory substitution changed read target: %q, %v", got, err)
	}
}

func TestReadPrivateFileRejectsWrongOwnerWhenPossible(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown requires root; normal tests exercise current-user ownership")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "private")
	writeTestPrivateFile(t, path, "test-key")
	if err := os.Chown(path, 65534, -1); err != nil {
		t.Skipf("chown not supported: %v", err)
	}
	if _, err := ReadPrivateFile(path, 64); err == nil {
		t.Fatal("accepted foreign-owned private file")
	}
}
