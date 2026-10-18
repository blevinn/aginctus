package filestate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRegistryLockContentionAndReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	first, err := LockRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockRegistry(path); !errors.Is(err, ErrRegistryBusy) {
		t.Fatalf("second writer must fail busy: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := LockRegistry(path)
	if err != nil {
		t.Fatalf("released lock cannot be reacquired: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path + ".flock")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("persistent private lock missing: %v, %v", info, err)
	}
}

func TestRegistryLockRejectsUnsafeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	if _, err := LockRegistry("relative.json"); err == nil {
		t.Fatal("relative path accepted")
	}
	if err := os.Symlink(path, path+".flock"); err != nil {
		t.Fatal(err)
	}
	if _, err := LockRegistry(path); err == nil {
		t.Fatal("symlink lock accepted")
	}
	if err := os.Remove(path + ".flock"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".flock", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LockRegistry(path); err == nil {
		t.Fatal("directory lock accepted")
	}
	if err := os.Remove(path + ".flock"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path+".flock", 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LockRegistry(path); err == nil {
		t.Fatal("FIFO lock accepted")
	}
	if err := os.Remove(path + ".flock"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".flock", []byte("do not truncate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LockRegistry(path); err == nil {
		t.Fatal("world-readable lock accepted")
	}
	if err := os.Chmod(path+".flock", 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := LockRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".flock")
	if err != nil || string(data) != "do not truncate" {
		t.Fatalf("lock file contents unexpectedly changed: %q, %v", data, err)
	}
}

func TestRegistryLockRejectsUntrustedParent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := LockRegistry(path); err == nil {
		t.Fatal("accepted world-writable parent")
	}
}

func TestRegistryLockLeavesLegacyDirectoryIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	legacy := path + ".lock"
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "unverified"), []byte("retain"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := LockRegistry(path)
	if err != nil {
		t.Fatalf("orphaned legacy directory still blocked new advisory lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(legacy, "unverified"))
	if err != nil || string(data) != "retain" {
		t.Fatalf("legacy directory modified: %q, %v", data, err)
	}
}

func TestRegistryLockChildProcess(t *testing.T) {
	path := os.Getenv("AGINCTUS_REGISTRY_LOCK_CHILD")
	if path == "" {
		return
	}
	lock, err := LockRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("LOCKED")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestRegistryLockReleasedWhenHolderKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRegistryLockChildProcess$")
	cmd.Env = append(os.Environ(), "AGINCTUS_REGISTRY_LOCK_CHILD="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	defer func() {
		_ = stdin.Close()
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && line != "LOCKED\n" {
			err = fmt.Errorf("unexpected child readiness: %q", line)
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("child failed to acquire lock: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not acquire lock")
	}
	if _, err := LockRegistry(path); !errors.Is(err, ErrRegistryBusy) {
		t.Fatalf("child-held advisory lock did not exclude parent: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	reaped = true
	lock, err := LockRegistry(path)
	if err != nil {
		t.Fatalf("killed holder left stale registry lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
