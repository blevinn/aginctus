package imageops

import (
	incusscope "github.com/blevinn/aginctus/internal/incus"
	incusimages "github.com/blevinn/aginctus/internal/incus/images"
)

// Project-level writer exclusion belongs to Incus image operations. These
// adapters preserve the v0.2 CLI surface and existing injectable call sites.
func ScopedImageLeaseAddress(scope incusscope.ProjectScope) (string, error) {
	return incusimages.ScopedImageLeaseAddress(scope)
}

func AcquireScopedImageMutationLease(scope incusscope.ProjectScope) (func(), error) {
	return incusimages.AcquireScopedImageMutationLease(scope)
}
