package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"runtime"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Linux abstract Unix-domain sockets make one bind succeed per name in a
// network namespace, regardless of the caller's UID. No filesystem lock path,
// pre-created directory, stale pidfile or deletion on crash is involved.
//
// One socket per *project*, not alias: imports of identical content under
// different aliases must also be serialized among participating Aginctus
// clients. This is cooperative exclusion, NOT proof of lifecycle ownership
// or a lock on external Incus commands or other network namespaces.
func scopedImageLeaseAddress(scope incusscope.ProjectScope) (string, error) {
	if _, err := incusscope.NewProjectScope(scope.Name); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(scope.Name))
	return "@aginctus-image-write-v1-" + hex.EncodeToString(sum[:]), nil
}

var acquireScopedImageMutationLease = func(scope incusscope.ProjectScope) (func(), error) {
	// The socket name would become a literal relative pathname on other
	// platforms; never create an accidental file or silently skip locking.
	if runtime.GOOS != "linux" {
		return nil, errors.New("host image writer exclusion requires Linux")
	}
	name, err := scopedImageLeaseAddress(scope)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", name)
	if err != nil {
		// The caller reports a deliberately generic error. A conflicting
		// binder (even malicious) must block work, never authorize it.
		return nil, fmt.Errorf("host project writer lease unavailable: %w", err)
	}
	return func() { _ = listener.Close() }, nil
}
