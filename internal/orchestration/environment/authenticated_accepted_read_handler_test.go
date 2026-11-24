package environment

import (
	"context"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testProjectPeerAuthorizer struct {
	binding Binding
	calls int
	revokeAfter int
}

func (a *testProjectPeerAuthorizer) AuthorizeProjectPeer(_ context.Context, _ *x509.Certificate) (Binding, error) {
	a.calls++
	if a.revokeAfter > 0 && a.calls > a.revokeAfter {
		return Binding{}, errors.New("private revocation policy")
	}
	return a.binding, nil
}

func authorizedAcceptedRequest(method, uri string) *http.Request {
	request := httptest.NewRequest(method, uri, nil)
	peer := &x509.Certificate{Raw: []byte("verified-peer"), SerialNumber: big.NewInt(1)}
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{peer},
		VerifiedChains: [][]*x509.Certificate{{peer}},
	}
	return request
}

func acceptedHTTPFixture(t *testing.T) (AuthenticatedAcceptedReadHandler, *testProjectPeerAuthorizer, *acceptedSourceFixture, Candidate) {
	t.Helper()
	model, _, source, candidate := acceptedModelFixture()
	authorizer := &testProjectPeerAuthorizer{binding: candidate.Environment.Binding}
	return AuthenticatedAcceptedReadHandler{
		Authorizer: authorizer, Catalog: model.Catalog, Source: source,
	}, authorizer, source, candidate
}

func TestAuthenticatedAcceptedReadHandlerReturnsOnlyVerifiedPublishedState(t *testing.T) {
	handler, authorizer, source, candidate := acceptedHTTPFixture(t)
	path := "/v1/accepted?environment=" + candidate.Environment.ID
	empty := httptest.NewRecorder()
	handler.ServeHTTP(empty, authorizedAcceptedRequest(http.MethodGet, path))
	if empty.Code != http.StatusOK || authorizer.calls != 2 || source.calls != 1 {
		t.Fatalf("authenticated absence: status=%d body=%s auth=%d source=%d", empty.Code, empty.Body, authorizer.calls, source.calls)
	}
	var absent acceptedReadWireResponse
	if err := json.Unmarshal(empty.Body.Bytes(), &absent); err != nil ||
		absent.APIVersion != AcceptedReadWireVersion ||
		absent.EnvironmentID != candidate.Environment.ID ||
		absent.Revision != nil || !bytes.Equal(absent.Candidate, []byte("null")) {
		t.Fatalf("invalid absence document: %+v err=%v", absent, err)
	}
	revision, err := BuildAcceptedRevision(nil, nil, candidate)
	if err != nil { t.Fatal(err) }
	source.revision, source.candidate = &revision, &candidate
	authorizer.calls = 0
	present := httptest.NewRecorder()
	handler.ServeHTTP(present, authorizedAcceptedRequest(http.MethodGet, path))
	var result acceptedReadWireResponse
	if err := json.Unmarshal(present.Body.Bytes(), &result); err != nil ||
		present.Code != http.StatusOK || result.Revision == nil ||
		result.Revision.ID != revision.ID || len(result.Candidate) == 0 {
		t.Fatalf("published read: status=%d doc=%+v err=%v", present.Code, result, err)
	}
	parsed, err := ParseCandidate(result.Candidate)
	if err != nil || result.Revision.ValidateAgainst(parsed) != nil {
		t.Fatalf("published candidate invalid: %+v err=%v", result, err)
	}
	if present.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("published revision was cacheable")
	}
}

func TestAuthenticatedAcceptedReadHandlerFailsBeforeDisclosure(t *testing.T) {
	for _, tc := range []struct{
		name string
		request func(string) *http.Request
		change func(*AuthenticatedAcceptedReadHandler, *testProjectPeerAuthorizer, *acceptedSourceFixture, Candidate)
		expect int
	}{
		{"missing TLS", func(path string) *http.Request { return httptest.NewRequest(http.MethodGet, path, nil) }, nil, http.StatusUnauthorized},
		{"POST", func(path string) *http.Request { return authorizedAcceptedRequest(http.MethodPost, path) }, nil, http.StatusMethodNotAllowed},
		{"foreign project", nil, func(_ *AuthenticatedAcceptedReadHandler, a *testProjectPeerAuthorizer, _ *acceptedSourceFixture, _ Candidate) {
			a.binding.Project = "foreign"
		}, http.StatusForbidden},
		{"revoked after storage read", nil, func(_ *AuthenticatedAcceptedReadHandler, a *testProjectPeerAuthorizer, _ *acceptedSourceFixture, _ Candidate) {
			a.revokeAfter = 1
		}, http.StatusForbidden},
		{"unavailable authorization", nil, func(h *AuthenticatedAcceptedReadHandler, _ *testProjectPeerAuthorizer, _ *acceptedSourceFixture, _ Candidate) {
			h.Authorizer = nil
		}, http.StatusServiceUnavailable},
		{"backend failure", nil, func(_ *AuthenticatedAcceptedReadHandler, _ *testProjectPeerAuthorizer, s *acceptedSourceFixture, _ Candidate) {
			s.err = errors.New("sensitive state volume location")
		}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, authorizer, source, candidate := acceptedHTTPFixture(t)
			if tc.change != nil { tc.change(&handler, authorizer, source, candidate) }
			path := "/v1/accepted?environment=" + candidate.Environment.ID
			request := authorizedAcceptedRequest(http.MethodGet, path)
			if tc.request != nil { request = tc.request(path) }
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.expect || strings.Contains(response.Body.String(), "sensitive") ||
				strings.Contains(response.Body.String(), candidate.Environment.ID) ||
				strings.Contains(response.Body.String(), "candidateDigest") {
				t.Fatalf("unsafe response: status=%d body=%s", response.Code, response.Body)
			}
		})
	}
	for _, path := range []string{"/v1/accepted?environment=dev",
		"/v1/accepted?environment=11111111-1111-4111-8111-111111111111&project=foreign",
		"/v1/accepted?environment=11111111-1111-4111-8111-111111111111&environment=22222222-2222-4222-8222-222222222222"} {
		h, _, source, _ := acceptedHTTPFixture(t)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, authorizedAcceptedRequest(http.MethodGet, path))
		if response.Code != http.StatusBadRequest || source.calls != 0 {
			t.Fatalf("invalid selector accessed source: status=%d calls=%d", response.Code, source.calls)
		}
	}
}
