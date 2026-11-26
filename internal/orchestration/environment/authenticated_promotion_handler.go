package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"
)

const acceptedPromotionWireVersion = "aginctus.dev/project-accepted-promotion/v1alpha1"
const maxAcceptedPromotionBytes = maxCandidateBytes + (16 << 10)

// AuthenticatedPromotionHandler requires an independently provisioned and
// isolated store, restricted Incus reader, catalog and promoter policy. Use
// only behind RequireAndVerifyClientCert.
type AuthenticatedPromotionHandler struct {
	Authorizer LiveIncusPeerAuthorizer
	Catalog    Catalog
	Store      *LocalProjectStateStore
	// AuthorizePromoter is mandatory and server-owned. It must check current
	// promotion authority for the exact DER SHA-256 fingerprint and complete
	// scoped CAS request. Project membership alone cannot supply this grant.
	AuthorizePromoter func(context.Context, string, LocalPromotionRequest) error
}

type acceptedPromotionWireRequest struct {
	APIVersion    string            `json:"apiVersion"`
	EnvironmentID string            `json:"environmentId"`
	Expected      *AcceptedRevision `json:"expected"`
	Candidate     json.RawMessage   `json:"candidate"`
}

type acceptedPromotionWireResponse struct {
	APIVersion string           `json:"apiVersion"`
	Revision   AcceptedRevision `json:"revision"`
}

func parseAcceptedPromotion(data []byte) (acceptedPromotionWireRequest, Candidate, error) {
	var req acceptedPromotionWireRequest
	invalid := errors.New("invalid promotion request")
	if !utf8.Valid(data) || rejectCandidateDuplicateKeys(data) != nil {
		return req, Candidate{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 4 || fields["expected"] == nil ||
		fields["apiVersion"] == nil || fields["environmentId"] == nil || fields["candidate"] == nil ||
		json.Unmarshal(data, &req) != nil || req.APIVersion != acceptedPromotionWireVersion || !idPattern.MatchString(req.EnvironmentID) {
		return acceptedPromotionWireRequest{}, Candidate{}, invalid
	}
	if req.Expected != nil {
		var revisionFields map[string]json.RawMessage
		if json.Unmarshal(fields["expected"], &revisionFields) != nil ||
			candidateKeys(revisionFields, "apiVersion", "environmentId", "serverId", "project", "generation", "parentId", "candidateDigest", "id") != nil ||
			req.Expected.ValidateIdentity() != nil {
			return acceptedPromotionWireRequest{}, Candidate{}, invalid
		}
	}
	candidate, err := ParseCandidate(req.Candidate)
	return req, candidate, err
}

func (h AuthenticatedPromotionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	deny := func(code int) { http.Error(w, http.StatusText(code), code) }
	if r.URL.Path != "/v1/promotion" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.Fragment != "" {
		deny(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		deny(http.StatusMethodNotAllowed)
		return
	}
	if h.Store == nil || h.AuthorizePromoter == nil || h.Catalog.Verifier == nil || h.Catalog.Source == nil {
		deny(http.StatusServiceUnavailable)
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
		deny(http.StatusUnauthorized)
		return
	}
	peer := r.TLS.PeerCertificates[0]
	if peer == nil || len(peer.Raw) == 0 || !peer.Equal(r.TLS.VerifiedChains[0][0]) {
		deny(http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	verifyPeer := func(ctx context.Context) (Binding, error) {
		now := time.Now()
		if now.Before(peer.NotBefore) || !now.Before(peer.NotAfter) {
			return Binding{}, errLocalPromotionDenied
		}
		return h.Authorizer.AuthorizeProjectPeer(ctx, peer)
	}
	binding, err := verifyPeer(ctx)
	if err != nil || binding != h.Store.binding {
		deny(http.StatusForbidden)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		deny(http.StatusUnsupportedMediaType)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAcceptedPromotionBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			deny(http.StatusRequestEntityTooLarge)
		} else {
			deny(http.StatusBadRequest)
		}
		return
	}
	req, candidate, err := parseAcceptedPromotion(data)
	if err != nil {
		deny(http.StatusBadRequest)
		return
	}
	desc, err := h.Catalog.Get(ctx, binding.Project, req.EnvironmentID)
	if err != nil || desc.Binding != binding || !candidate.Environment.SameEnvironment(desc) {
		deny(http.StatusForbidden)
		return
	}
	sum := sha256.Sum256(peer.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	result, err := h.Store.PromoteLocalCandidate(ctx, desc, req.Expected, candidate, func(ctx context.Context, exact LocalPromotionRequest) (Binding, error) {
		current, err := h.Catalog.Get(ctx, binding.Project, desc.ID)
		if err != nil || !current.SameEnvironment(desc) || h.AuthorizePromoter(ctx, fingerprint, exact) != nil {
			return Binding{}, errLocalPromotionDenied
		}
		// Check the actual peer again after enrollment and policy lookups,
		// including during the store's final in-lock publication check.
		return verifyPeer(ctx)
	})
	switch {
	case errors.Is(err, errLocalPromotionDenied):
		deny(http.StatusForbidden)
	case errors.Is(err, ErrAcceptedConflict), errors.Is(err, ErrAcceptedUnchanged):
		deny(http.StatusConflict)
	case err != nil:
		// Publication may have succeeded before sync or transport failure.
		http.Error(w, "promotion outcome unavailable; re-read accepted state before retry", http.StatusServiceUnavailable)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(acceptedPromotionWireResponse{APIVersion: acceptedPromotionWireVersion, Revision: result})
	}
}
