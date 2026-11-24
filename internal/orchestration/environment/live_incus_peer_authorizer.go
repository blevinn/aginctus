package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"

	incusapi "github.com/lxc/incus/v7/shared/api"
)

// RestrictedIncusTrustReader is the read-only observation needed to evaluate
// the current TLS peer's Incus certificate authorization. In production it
// MUST be the same independently authenticated, genuinely project-restricted
// Incus SDK connection used for VerifyProject, with no local admin socket,
// unrestricted certificate, or delegated host-root endpoint.
//
// If Incus does not permit this narrow lookup to a restricted principal,
// authorization MUST fail; do not grant an admin trust-store capability to
// the state service merely to make this implementation pass.
type RestrictedIncusTrustReader interface {
	RestrictedIncusProjectObserver
	GetCertificate(string) (*incusapi.Certificate, string, error)
}

// LiveIncusPeerAuthorizer implements ProjectPeerAuthorizer by consulting the
// current, server-returned Incus certificate trust record for the actual
// mTLS client peer. Neither a presented client certificate nor an HTTP field
// is a project grant. This authorizer cannot itself deploy the service or
// prove that a selected Incus authorization driver enforces these records.
type LiveIncusPeerAuthorizer struct {
	Reader RestrictedIncusTrustReader
	PinnedServerCertificateSHA256 string
	Project string
}

func (a LiveIncusPeerAuthorizer) AuthorizeProjectPeer(
	ctx context.Context, peer *x509.Certificate,
) (Binding, error) {
	deny := func() (Binding, error) {
		return Binding{}, errors.New("current restricted Incus TLS peer authorization unavailable")
	}
	if ctx == nil || ctx.Err() != nil || peer == nil || len(peer.Raw) == 0 ||
		a.Reader == nil || a.Project == "" || a.Project == "default" {
		return deny()
	}
	// Recheck current, restricted project scope for the service's OWN Incus
	// connection; an administrator-selected project is never good enough.
	verifier := RestrictedIncusProjectVerifier{
		Observer: a.Reader,
		PinnedServerCertificateSHA256: a.PinnedServerCertificateSHA256,
	}
	binding, err := verifier.VerifyProject(ctx, a.Project)
	if err != nil || binding.Project != a.Project { return deny() }
	sum := sha256.Sum256(peer.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	record, etag, err := a.Reader.GetCertificate(fingerprint)
	if err != nil || record == nil || etag == "" ||
		record.Fingerprint != fingerprint ||
		record.Type != incusapi.CertificateTypeClient ||
		!record.Restricted || len(record.Projects) != 1 ||
		record.Projects[0] != a.Project {
		return deny()
	}
	block, rest := pem.Decode([]byte(record.Certificate))
	if block == nil || block.Type != "CERTIFICATE" ||
		len(bytes.TrimSpace(rest)) != 0 || !bytes.Equal(block.Bytes, peer.Raw) {
		return deny()
	}
	// No stale cached trust records: the handler invokes this method again
	// after the published read. Do not publish on cancellation either.
	if ctx.Err() != nil { return deny() }
	return binding, nil
}
