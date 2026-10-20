package filestate

import (
	"fmt"
	"os"
	"path/filepath"
)

// WritePrivateFile replaces path with data through a private temporary file in
// the same directory. The temporary file is chmod 0600 before any content is
// written and is fsynced before the rename, so a reader never observes a
// partially written or group/world-readable path, and a crash leaves the
// previous contents or no file at all.
//
// The caller owns the parent directory's creation and permission checks. A
// symlink at path is replaced by the rename itself rather than followed, so the
// write cannot be redirected onto a link's target.
func WritePrivateFile(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("private file path must be absolute")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := tmp.Name()
	// A successful rename makes this remove a no-op; any earlier failure must
	// not leave scratch state behind in the state directory.
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("protect temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("persist private file: %w", err)
	}
	return nil
}
