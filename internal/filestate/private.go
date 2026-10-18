// Package filestate provides bounded, descriptor-based reads of private local
// state files on Linux. It does not create, rename, or change any files.
package filestate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const maxReadSize int64 = 16 << 20

// ReadPrivateFile reads at most maxBytes from an owned private regular file.
// The immediate parent must be a non-symlink directory owned by this process
// or root and must not be group/world writable. Opening the child via openat
// binds its lookup to the verified directory, and O_NOFOLLOW rejects a
// substituted final symlink. All data is read through the verified file
// descriptor. Trusted ancestors of the parent remain the operator's
// responsibility: this does not establish a secure root for arbitrary paths
// in an attacker-controlled filesystem.
func ReadPrivateFile(path string, maxBytes int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("private file path must be absolute")
	}
	if maxBytes <= 0 || maxBytes > maxReadSize {
		return nil, fmt.Errorf("invalid private file read limit")
	}
	dir, err := openTrustedDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readPrivateFileAt(dir, filepath.Base(path), maxBytes)
}

func openTrustedDirectory(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open private file directory: %w", err)
	}
	dir := os.NewFile(uintptr(fd), "private file directory")
	info, err := dir.Stat()
	if err != nil {
		dir.Close()
		return nil, fmt.Errorf("inspect private file directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0o022 != 0 ||
		(stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) {
		dir.Close()
		return nil, fmt.Errorf("private file directory is not trusted")
	}
	return dir, nil
}

func readPrivateFileAt(dir *os.File, base string, maxBytes int64) ([]byte, error) {
	fd, err := syscall.Openat(int(dir.Fd()), base,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open private file safely: %w", err)
	}
	file := os.NewFile(uintptr(fd), "private file")
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened private file: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() ||
		info.Mode().Perm()&0o077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("private file must be an owned regular file with restrictive permissions")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read private file: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("private file exceeds size limit")
	}
	return data, nil
}
