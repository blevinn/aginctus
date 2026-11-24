package environment

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
)

// AcceptedReadWireVersion separates the authenticated project-state read
// protocol from both the same-UID fixture and mutable state promotion APIs.
const AcceptedReadWireVersion = "aginctus.dev/project-accepted-read/v1alpha1"

const maxAcceptedReadWireBytes = maxCandidateBytes + 16<<10

type acceptedReadWireResponse struct {
	APIVersion string `json:"apiVersion"`
	EnvironmentID string `json:"environmentId"`
	Revision *AcceptedRevision `json:"revision"`
	Candidate json.RawMessage `json:"candidate"`
}

// ProjectPeerAuthorizer must independently verify the authenticated TLS
// client's *current* authorized Incus server/project from a trusted service
// policy source, including revocation. It cannot simply decode client claims,
// accept the service's own Incus credential, or trust request headers.
// No production authorizer is installed here: the server remains opt-in
// until its physical trust/Revocation implementation is independently tested.
type ProjectPeerAuthorizer interface {
	AuthorizeProjectPeer(context.Context, *x509.Certificate) (Binding, error)
}

// AuthenticatedAcceptedReadHandler is a narrow read-only HTTP handler. Mount
// it only behind TLS configured with mandatory client-certificate verification
// and an independently provisioned project principal authorizer. It owns no
// Incus admin socket and has no accept, remove or generic Incus proxy method.
//
// Source must read the protected, project-owned store using the verified
// enrollment identity. A same-UID fixture does NOT become production simply
// by supplying it here.
type AuthenticatedAcceptedReadHandler struct {
	Authorizer ProjectPeerAuthorizer
	Catalog Catalog
	Source PublishedAcceptedSource
}

func (h AuthenticatedAcceptedReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	deny := func(code int) { http.Error(w, http.StatusText(code), code) }
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		deny(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/v1/accepted" || r.URL.RawPath != "" ||
		r.URL.Fragment != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		deny(http.StatusBadRequest)
		return
	}
	if h.Authorizer == nil || h.Catalog.Verifier == nil ||
		h.Catalog.Source == nil || h.Source == nil {
		deny(http.StatusServiceUnavailable)
		return
	}
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 ||
		len(r.TLS.VerifiedChains[0]) == 0 || len(r.TLS.PeerCertificates) == 0 {
		deny(http.StatusUnauthorized)
		return
	}
	peer := r.TLS.PeerCertificates[0]
	if peer == nil || !peer.Equal(r.TLS.VerifiedChains[0][0]) {
		deny(http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	if ctx.Err() != nil {
		deny(http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	ids, exists := q["environment"]
	if !exists || len(q) != 1 || len(ids) != 1 || !idPattern.MatchString(ids[0]) {
		deny(http.StatusBadRequest)
		return
	}
	// The only project selector comes from independently verified TLS peer
	// authorization, never HTTP query/header/candidate metadata.
	binding, err := h.Authorizer.AuthorizeProjectPeer(ctx, peer)
	if err != nil || binding.Project == "" || binding.ServerID == "" ||
		binding.Project == "default" {
		deny(http.StatusForbidden)
		return
	}
	desc, err := h.Catalog.Get(ctx, binding.Project, ids[0])
	if err != nil || desc.Binding != binding {
		deny(http.StatusForbidden)
		return
	}
	rev, candidate, err := h.Source.ReadAccepted(ctx, desc)
	if err != nil || (rev == nil) != (candidate == nil) {
		deny(http.StatusServiceUnavailable)
		return
	}
	if rev != nil && (rev.ValidateAgainst(*candidate) != nil ||
		!candidate.Environment.SameEnvironment(desc)) {
		deny(http.StatusServiceUnavailable)
		return
	}
	response := acceptedReadWireResponse{
		APIVersion: AcceptedReadWireVersion, EnvironmentID: desc.ID, Revision: rev,
	}
	if candidate != nil {
		response.Candidate, err = candidate.Canonical()
		if err != nil { deny(http.StatusServiceUnavailable); return }
	}
	data, err := json.Marshal(response)
	if err != nil || len(data) > maxAcceptedReadWireBytes {
		deny(http.StatusServiceUnavailable)
		return
	}
	// Every successful response is checked again after storage access and
	// serialization. This detects live policy revocation and retargeting; a
	// physical authorizer MUST use fresh source evidence on each call.
	after, err := h.Authorizer.AuthorizeProjectPeer(ctx, peer)
	if err != nil || after != binding || ctx.Err() != nil {
		deny(http.StatusForbidden)
		return
	}
	// Revalidate exact enrollment authority after potentially slow storage
	// access. A record is never disclosed after a project was retargeted.
	refreshed, err := h.Catalog.Get(ctx, binding.Project, desc.ID)
	if err != nil || !refreshed.SameEnvironment(desc) || ctx.Err() != nil {
		deny(http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// Ensure the explicit authorization interface is not accidentally confused
// with catalog verification: the latter observes the SERVICE connection,
// while this verifies each CALLER's presented TLS credential.
