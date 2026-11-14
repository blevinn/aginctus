package cli

import (
	imageops "github.com/blevinn/aginctus/internal/cli/operations/images"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Root compatibility seams allow existing CLI regression tests to substitute
// the cooperative project lease without changing the actual image operation.
func scopedImageLeaseAddress(scope incusscope.ProjectScope) (string, error) {
	return imageops.ScopedImageLeaseAddress(scope)
}
var acquireScopedImageMutationLease = imageops.AcquireScopedImageMutationLease
