package environment

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func acceptedMTLSFixture(t *testing.T) (RestrictedHTTPSReadSessionFactory, *httptest.Server, *testProjectPeerAuthorizer, *acceptedSourceFixture, Candidate) {
	t.Helper()
	model, verifier, source, candidate := acceptedModelFixture()
	authorizer := &testProjectPeerAuthorizer{binding: candidate.Environment.Binding}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatal(err) }
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &key.PublicKey, key)
	if err != nil { t.Fatal(err) }
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil { t.Fatal(err) }
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(pemCert) { t.Fatal("client CA unavailable") }
	handler := AuthenticatedAcceptedReadHandler{
		Authorizer: authorizer, Catalog: model.Catalog, Source: source,
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: ca,
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	serverPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
	})
	serverHash := sha256.Sum256(server.Certificate().Raw)
	return RestrictedHTTPSReadSessionFactory{
		Endpoint: server.URL,
		ServiceCertificatePEM: string(serverPEM),
		ServiceCertificateSHA256: hex.EncodeToString(serverHash[:]),
		ClientCertificatePEM: string(pemCert),
		ClientPrivateKeyPEM: string(pemKey),
		Verifier: verifier,
	}, server, authorizer, source, candidate
}

func TestRestrictedHTTPSReadSessionReadsAcceptedOverActualMTLS(t *testing.T) {
	factory, _, _, source, candidate := acceptedMTLSFixture(t)
	ctx := context.Background()
	session, err := factory.OpenReadSession(ctx, candidate.Environment)
	if err != nil { t.Fatal(err) }
	absent, body, err := ReadRestrictedProjectAccepted(ctx, session, candidate.Environment)
	if err != nil || absent != nil || body != nil {
		t.Fatalf("authenticated absent state: rev=%+v candidate=%+v err=%v", absent, body, err)
	}
	revision, err := BuildAcceptedRevision(nil, nil, candidate)
	if err != nil { t.Fatal(err) }
	source.revision, source.candidate = &revision, &candidate
	got, gotBody, err := ReadRestrictedProjectAccepted(ctx, session, candidate.Environment)
	if err != nil || got == nil || got.ID != revision.ID ||
		gotBody == nil || !gotBody.Environment.SameEnvironment(candidate.Environment) {
		t.Fatalf("published state: rev=%+v candidate=%+v err=%v", got, gotBody, err)
	}
}

func TestRestrictedHTTPSReadSessionRefusesRevocationAndUntrustedServer(t *testing.T) {
	factory, server, authorizer, source, candidate := acceptedMTLSFixture(t)
	rev, err := BuildAcceptedRevision(nil, nil, candidate)
	if err != nil { t.Fatal(err) }
	source.revision, source.candidate = &rev, &candidate
	session, err := factory.OpenReadSession(context.Background(), candidate.Environment)
	if err != nil { t.Fatal(err) }
	authorizer.revokeAfter = 1
	if result, body, err := ReadRestrictedProjectAccepted(context.Background(), session, candidate.Environment);
		err == nil || result != nil || body != nil {
		t.Fatalf("revoked response leaked: rev=%+v body=%+v err=%v", result, body, err)
	}
	authorizer.revokeAfter = 0
	authorizer.calls = 0
	factory.ServiceCertificateSHA256 = strings.Repeat("0", 64)
	if session, err := factory.OpenReadSession(context.Background(), candidate.Environment);
		err == nil || session != nil {
		t.Fatalf("unpinned server accepted: session=%v err=%v", session, err)
	}
	factory.ServiceCertificateSHA256 = hex.EncodeToString(func() []byte {
		hash := sha256.Sum256(server.Certificate().Raw); return hash[:]
	}())
	factory.Endpoint = "http://127.0.0.1"
	if session, err := factory.OpenReadSession(context.Background(), candidate.Environment);
		err == nil || session != nil {
		t.Fatalf("plaintext endpoint accepted: session=%v err=%v", session, err)
	}
	factory.Endpoint = server.URL
	factory.ClientPrivateKeyPEM = ""
	if session, err := factory.OpenReadSession(context.Background(), candidate.Environment);
		err == nil || session != nil {
		t.Fatalf("missing TLS private key accepted: session=%v err=%v", session, err)
	}
}

func TestRestrictedHTTPSReadSessionRejectsForeignIdentityAndMalformedSelectors(t *testing.T) {
	factory, _, _, _, candidate := acceptedMTLSFixture(t)
	foreign := candidate.Environment
	foreign.Binding.Project = "other"
	if session, err := factory.OpenReadSession(context.Background(), foreign); err == nil || session != nil {
		t.Fatalf("foreign project accepted: %v", err)
	}
	session, err := factory.OpenReadSession(context.Background(), candidate.Environment)
	if err != nil { t.Fatal(err) }
	if rev, body, err := session.ReadPublished(context.Background(), "dev");
		err == nil || rev != nil || body != nil {
		t.Fatalf("display name used as UUID: rev=%+v body=%+v err=%v", rev, body, err)
	}
	ctx, cancel := context.WithCancel(context.Background()); cancel()
	if rev, body, err := session.ReadPublished(ctx, candidate.Environment.ID);
		err == nil || rev != nil || body != nil {
		t.Fatalf("canceled request returned state: %+v %+v %v", rev, body, err)
	}
	if result, body, err := session.ReadPublished(context.Background(), candidate.Environment.ID);
		err != nil || result != nil || body != nil {
		t.Fatalf("published absence was not authenticated: %+v %+v %v", result, body, err)
	}
	// The server forbids mutations even when the client possesses TLS access.
}
