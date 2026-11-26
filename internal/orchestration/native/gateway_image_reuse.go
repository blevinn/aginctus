package native

import (
	"fmt"

	"github.com/lxc/incus/v7/shared/api"
)

// GatewayImageEvidence records the full OCI identity from a separately
// protected, accepted deployment record. Its fields are data, not authority:
// callers must validate the provenance/revision before using this assessment.
type GatewayImageEvidence struct {
	Project  string
	Alias    string
	Identity GatewayOCIIdentity
	// AliasMarker corroborates the observed alias incarnation for persisted
	// evidence. A marker is public metadata, never evidence on its own.
	AliasMarker string `json:"aliasMarker,omitempty"`
}

func (e GatewayImageEvidence) Validate() error {
	if !localName(e.Alias) || (e.AliasMarker != "" && !gatewayAliasMarker.MatchString(e.AliasMarker)) {
		return fmt.Errorf("invalid gateway image alias identity")
	}
	return (GatewayImageAcquireIntent{Project: e.Project, Image: e.Identity}).Validate()
}

// AssessGatewayImageReuse is read-only and fail-closed. Incus fingerprints
// identify layers, not the OCI manifest or config. Existing images cannot be
// safely reused without independent, protected full-identity evidence.
// Passing this assessment is NOT permission to create an alias or run a guest.
func AssessGatewayImageReuse(want GatewayImageAcquireIntent, aliasName string, evidence GatewayImageEvidence, alias *api.ImageAliasesEntry, image *api.Image) error {
	if err := want.Validate(); err != nil {
		return fmt.Errorf("gateway image reuse: invalid immutable intent")
	}
	if !localName(aliasName) || evidence.Project != want.Project || evidence.Alias != aliasName ||
		evidence.Identity != want.Image {
		return fmt.Errorf("gateway image reuse: accepted identity evidence missing or mismatched")
	}
	if alias == nil || alias.Name != aliasName || alias.Target != want.Image.IncusFingerprint ||
		(evidence.AliasMarker != "" && alias.Description != evidence.AliasMarker) {
		return fmt.Errorf("gateway image reuse: alias identity unverified")
	}
	if image == nil || image.Project != want.Project || image.Type != "container" ||
		image.Fingerprint != want.Image.IncusFingerprint {
		return fmt.Errorf("gateway image reuse: selected-project image unverified")
	}
	return nil
}
