package filestate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrRegistryBusy reports an advisory write lock held by another process.
var ErrRegistryBusy = errors.New("registry write lock busy")

// LockRegistry obtains a nonblocking, process-scoped advisory lock for a
// registry update. Keep the returned file open until the atomic registry
// replacement completes, then Close it. Close or process death releases flock.
// Never unlink the persistent lock file: replacing its inode can split writers.
//
// Use a new .flock suffix rather than the legacy .lock directory. Existing
// .lock directories are left untouched; do not delete unknown lock content
// or run old and new registry writers simultaneously during an upgrade.
func LockRegistry(registryPath string) (*os.File, error) {
	if !filepath.IsAbs(registryPath) || filepath.Base(registryPath) == "." {
		return nil, fmt.Errorf("registry lock path must be absolute")
	}
	dir, err := openTrustedDirectory(filepath.Dir(registryPath))
	if err != nil {
		return nil, fmt.Errorf("inspect registry lock directory: %w", err)
	}
	defer dir.Close()

	// O_NONBLOCK prevents an unexpected FIFO from blocking the open.
	name := filepath.Base(registryPath) + ".flock"
	fd, err := syscall.Openat(int(dir.Fd()), name,
		syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open registry lock safely: %w", err)
	}
	lock := os.NewFile(uintptr(fd), "registry advisory lock")
	closeOnError := func(err error) (*os.File, error) {
		_ = lock.Close()
		return nil, err
	}
	info, err := lock.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("inspect registry lock: %w", err))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 ||
		stat.Uid != uint32(os.Geteuid()) {
		return closeOnError(fmt.Errorf("registry lock must be an owned private regular file"))
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return closeOnError(ErrRegistryBusy)
		}
		return closeOnError(fmt.Errorf("acquire registry lock: %w", err))
	}
	// Ensure the persistent pathname still names the inode holding our lock.
	// Same-UID mutation remains within the operator's trust boundary.
	named, err := os.Lstat(filepath.Join(filepath.Dir(registryPath), name))
	if err != nil {
		return closeOnError(fmt.Errorf("verify registry lock path: %w", err))
	}
	if !os.SameFile(info, named) || !named.Mode().IsRegular() {
		return closeOnError(fmt.Errorf("registry lock path was replaced"))
	}
	return lock, nil
}
