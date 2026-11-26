package environment

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newPromotionFixture(t *testing.T, auth LiveIncusPeerAuthorizer) (AuthenticatedPromotionHandler, Candidate) {
	t.Helper()
	c := candidateFixture()
	c.Environment.Binding = Binding{ServerID: "sha256:" + auth.PinnedServerCertificateSHA256, Project: auth.Project}
	store, err := InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), c.Environment.Binding)
	if err != nil {
		t.Fatal(err)
	}
	return AuthenticatedPromotionHandler{
		Authorizer: auth, Store: store,
		Catalog: Catalog{Verifier: RestrictedIncusProjectVerifier{Observer: auth.Reader, PinnedServerCertificateSHA256: auth.PinnedServerCertificateSHA256},
			Source: &testEnrollmentSource{descriptors: []Descriptor{c.Environment}}},
	}, c
}

func promotionBody(t *testing.T, c Candidate, expected *AcceptedRevision) []byte {
	t.Helper()
	body, err := c.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(acceptedPromotionWireRequest{acceptedPromotionWireVersion, c.Environment.ID, expected, body})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAuthenticatedPromotionCASOverMTLS(t *testing.T) {
	auth, registry, peer, key := liveIncusTrustKeyFixture(t)
	h, c := newPromotionFixture(t, auth)
	genesis, err := BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	nextCandidate := c
	nextCandidate.Environment.Name = "reviewed-change"
	next, err := BuildAcceptedRevision(&genesis, &c, nextCandidate)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := registry.record.Fingerprint
	h.AuthorizePromoter = func(_ context.Context, actor string, req LocalPromotionRequest) error {
		if actor == fingerprint && req.Environment == c.Environment &&
			(req.Expected == (AcceptedRevision{}) && req.CandidateDigest == genesis.CandidateDigest ||
				req.Expected == genesis && req.CandidateDigest == next.CandidateDigest) {
			return nil
		}
		return errors.New("private promoter decision")
	}
	pool := x509.NewCertPool()
	pool.AddCert(peer)
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	client := srv.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.Certificates = []tls.Certificate{{Certificate: [][]byte{peer.Raw}, PrivateKey: key}}
	post := func(candidate Candidate, expected *AcceptedRevision, code int, want AcceptedRevision) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v1/promotion", bytes.NewReader(promotionBody(t, candidate, expected)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Promoter-Fingerprint", "untrusted-claim")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != code || bytes.Contains(body, []byte("private promoter decision")) {
			t.Fatalf("unexpected promotion outcome: status=%d err=%v", response.StatusCode, err)
		}
		if code == http.StatusOK {
			var got acceptedPromotionWireResponse
			if json.Unmarshal(body, &got) != nil || got.APIVersion != acceptedPromotionWireVersion || got.Revision != want {
				t.Fatal("incorrect promotion receipt")
			}
		}
	}
	post(c, nil, http.StatusOK, genesis)
	post(c, nil, http.StatusConflict, AcceptedRevision{})
	post(nextCandidate, &genesis, http.StatusOK, next)
	post(nextCandidate, &genesis, http.StatusConflict, AcceptedRevision{})
	post(nextCandidate, &next, http.StatusForbidden, AcceptedRevision{})
	active, _, err := h.Store.ReadLocalAccepted(t.Context(), c.Environment)
	if err != nil || active == nil || *active != next {
		t.Fatal("wrong active revision after successful promotions and rejections")
	}
}

func TestAuthenticatedPromotionDenialsPreserveUnpublishedState(t *testing.T) {
	for _, scenario := range []string{"no peer", "no capability", "member denied", "trust revoked", "capability revoked", "foreign candidate", "missing expected", "ambiguous expected", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			auth, registry, peer := liveIncusTrustFixture(t)
			h, c := newPromotionFixture(t, auth)
			policyCalls := 0
			h.AuthorizePromoter = func(context.Context, string, LocalPromotionRequest) error {
				policyCalls++
				if scenario == "member denied" || scenario == "capability revoked" && policyCalls == 3 {
					return errors.New("private promoter decision")
				}
				return nil
			}
			body := promotionBody(t, c, nil)
			request := authorizedAcceptedRequest(http.MethodPost, "/v1/promotion")
			request.TLS.PeerCertificates[0] = peer
			request.TLS.VerifiedChains[0][0] = peer
			want := http.StatusForbidden
			switch scenario {
			case "no peer":
				request.TLS = nil
				want = http.StatusUnauthorized
			case "no capability":
				h.AuthorizePromoter = nil
				want = http.StatusServiceUnavailable
			case "trust revoked":
				registry.revokeAt = 4 // Final in-lock live trust check.
			case "foreign candidate":
				foreign := c
				foreign.Environment.Binding.Project = "foreign"
				body = promotionBody(t, foreign, nil)
			case "missing expected":
				body = bytes.Replace(body, []byte(`"expected":null,`), nil, 1)
				want = http.StatusBadRequest
			case "ambiguous expected":
				body = bytes.Replace(body, []byte(`"expected":null,`), []byte(`"expected":null,"Expected":null,`), 1)
				want = http.StatusBadRequest
			case "oversized":
				body = []byte(strings.Repeat(" ", maxAcceptedPromotionBytes+1))
				want = http.StatusRequestEntityTooLarge
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			if response.Code != want || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("unexpected denial: HTTP %d, want %d", response.Code, want)
			}
			rev, _, err := h.Store.ReadLocalAccepted(t.Context(), c.Environment)
			if err != nil || rev != nil {
				t.Fatal("denied operation published accepted state")
			}
		})
	}
}
