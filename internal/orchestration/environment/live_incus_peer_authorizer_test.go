package environment

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"
)

type testLiveRestrictedTrust struct {
	*fixtureRestrictedIncusObserver
	record *api.Certificate
	err error
	calls int
	revokeAt int
}
func (r *testLiveRestrictedTrust) GetCertificate(_ string) (*api.Certificate, string, error) {
	r.calls++
	if r.revokeAt > 0 && r.calls >= r.revokeAt {
		return nil, "", errors.New("secret revoked certificate state")
	}
	if r.err != nil { return nil, "", r.err }
	return r.record, "trust-etag", nil
}

func liveIncusTrustFixture(t *testing.T) (LiveIncusPeerAuthorizer, *testLiveRestrictedTrust, *x509.Certificate) {
	t.Helper()
	verifier, observer := restrictedIncusVerifierFixture(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatal(err) }
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil { t.Fatal(err) }
	peer, err := x509.ParseCertificate(der)
	if err != nil { t.Fatal(err) }
	sum := sha256.Sum256(peer.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	registry := &testLiveRestrictedTrust{
		fixtureRestrictedIncusObserver: observer,
		record: &api.Certificate{
			CertificatePut: api.CertificatePut{
				Type: api.CertificateTypeClient, Restricted: true,
				Projects: []string{"team"},
				Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: peer.Raw})),
			},
			Fingerprint: fingerprint,
		},
	}
	return LiveIncusPeerAuthorizer{
		Reader: registry, Project: "team",
		PinnedServerCertificateSHA256: verifier.PinnedServerCertificateSHA256,
	}, registry, peer
}

func TestLiveIncusPeerAuthorizerChecksActualTrustRecord(t *testing.T) {
	authorizer, registry, peer := liveIncusTrustFixture(t)
	got, err := authorizer.AuthorizeProjectPeer(context.Background(), peer)
	if err != nil || got.Project != "team" ||
		got.ServerID != "sha256:"+authorizer.PinnedServerCertificateSHA256 ||
		registry.calls != 1 {
		t.Fatalf("verified peer=%+v err=%v calls=%d", got, err, registry.calls)
	}
}

func TestLiveIncusPeerAuthorizerRefusesRevokedAndForeignCertificates(t *testing.T) {
	for _, tc := range []struct{
		name string
		change func(*LiveIncusPeerAuthorizer, *testLiveRestrictedTrust)
	}{
		{"unrestricted peer", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record.Restricted = false }},
		{"foreign project", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record.Projects = []string{"foreign"} }},
		{"multiple projects", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record.Projects = []string{"team", "foreign"} }},
		{"forged fingerprint", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record.Fingerprint = "forged" }},
		{"wrong certificate", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record.Certificate = "not a certificate" }},
		{"wrong type", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record.Type = api.CertificateTypeServer }},
		{"revoked trust", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.record = nil }},
		{"denied trust lookup", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.err = errors.New("private trust store detail") }},
		{"untrusted server", func(a *LiveIncusPeerAuthorizer, _ *testLiveRestrictedTrust) { a.PinnedServerCertificateSHA256 = "wrong" }},
		{"administrator socket", func(_ *LiveIncusPeerAuthorizer, r *testLiveRestrictedTrust) { r.conn.SocketPath = "/var/lib/incus/unix.socket" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorizer, registry, peer := liveIncusTrustFixture(t)
			tc.change(&authorizer, registry)
			got, err := authorizer.AuthorizeProjectPeer(context.Background(), peer)
			if err == nil || got != (Binding{}) {
				t.Fatalf("unauthorized peer=%+v err=%v", got, err)
			}
		})
	}
}

func TestLiveIncusPeerRevocationBetweenHandlerChecksDeniesPayload(t *testing.T) {
	authorizer, registry, peer := liveIncusTrustFixture(t)
	registry.revokeAt = 2
	model, verifier, source, candidate := acceptedModelFixture()
	// The enrolled catalog and the live TLS authorizer must independently
	// agree on the exact *authenticated* server/project identity. Otherwise
	// the handler must deny before opening its accepted-state source.
	candidate.Environment.Binding.ServerID = "sha256:" + authorizer.PinnedServerCertificateSHA256
	candidate.Environment.Binding.Project = authorizer.Project
	verifier.binding = candidate.Environment.Binding
	model.Catalog.Source.(*testEnrollmentSource).descriptors = []Descriptor{candidate.Environment}
	rev, err := BuildAcceptedRevision(nil, nil, candidate)
	if err != nil { t.Fatal(err) }
	source.revision, source.candidate = &rev, &candidate
	handler := AuthenticatedAcceptedReadHandler{
		Authorizer: authorizer, Catalog: model.Catalog, Source: source,
	}
	request := authorizedAcceptedRequest(http.MethodGet, "/v1/accepted?environment="+candidate.Environment.ID)
	request.TLS.PeerCertificates[0] = peer
	request.TLS.VerifiedChains[0][0] = peer
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden ||
		source.calls != 1 || registry.calls != 2 {
		t.Fatalf("revoked project peer disclosed state: status=%d calls=%d reads=%d body=%s",
			response.Code, registry.calls, source.calls, response.Body)
	}
}
