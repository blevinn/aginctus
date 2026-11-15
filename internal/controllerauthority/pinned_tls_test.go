package controllerauthority

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func ephemeralProbeCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}
}

type mockRestrictedDaemon struct {
	client        tls.Certificate
	restricted    bool
	projects      []string
	projectConfig map[string]string
	identity      string
	status        int
	redirect      bool
	oversize      bool
	nonJSON       bool
	requests      atomic.Int64
	writes        atomic.Int64
}

func newMockRestrictedDaemon(t *testing.T) (*mockRestrictedDaemon, *httptest.Server, PinnedTLSProbe) {
	t.Helper()
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	config, err := scope.ProposedRestrictedOperatorConfig("hm-mgmt", "default")
	if err != nil {
		t.Fatal(err)
	}
	clientCert := ephemeralProbeCertificate(t)
	fp := sha256.Sum256(clientCert.Certificate[0])
	mock := &mockRestrictedDaemon{
		client: clientCert, restricted: true,
		projects: []string{"aginctus"}, projectConfig: config,
		identity: hex.EncodeToString(fp[:]),
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.requests.Add(1)
		if r.Method != http.MethodGet {
			mock.writes.Add(1)
			http.Error(w, "not a GET", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			http.Error(w, "client certificate missing", http.StatusForbidden)
			return
		}
		if mock.redirect {
			http.Redirect(w, r, "/1.0", http.StatusTemporaryRedirect)
			return
		}
		if mock.status != 0 {
			http.Error(w, "denied", mock.status)
			return
		}
		if mock.nonJSON {
			_, _ = w.Write([]byte("<html>invalid</html>"))
			return
		}
		if mock.oversize {
			_, _ = w.Write([]byte(strings.Repeat("x", int(maxIncusGETBytes)+1)))
			return
		}
		var metadata any
		switch r.URL.Path {
		case "/1.0":
			if r.URL.Query().Get("project") != "aginctus" {
				http.Error(w, "wrong project", http.StatusForbidden)
				return
			}
			metadata = map[string]any{"auth": "trusted", "auth_user_method": "tls", "auth_user_name": mock.identity}
		case "/1.0/certificates/" + mock.identity:
			metadata = map[string]any{"fingerprint": mock.identity, "restricted": mock.restricted, "projects": mock.projects}
		case "/1.0/projects/aginctus":
			metadata = map[string]any{"name": "aginctus", "config": mock.projectConfig}
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "sync", "status_code": 200, "error_code": 0, "metadata": metadata,
		})
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	serverFingerprint := sha256.Sum256(server.Certificate().Raw)
	return mock, server, PinnedTLSProbe{
		Endpoint:                server.URL,
		ServerCertificateSHA256: hex.EncodeToString(serverFingerprint[:]),
		ServerCertificateDER:    server.Certificate().Raw,
		ClientCertificate:       clientCert,
	}
}

func TestPinnedTLSProbeUsesActualBoundClientCertificate(t *testing.T) {
	mock, server, config := newMockRestrictedDaemon(t)
	defer server.Close()
	observed, err := ObserveRestrictedOperatorOverTLS(
		context.Background(), config, "aginctus", "hm-mgmt", "default",
	)
	if err != nil || !observed.VerifiedRestriction || observed.Project != "aginctus" ||
		observed.CertificateFingerprint != mock.identity {
		t.Fatalf("pinned live TLS inspection failed: %+v %v", observed, err)
	}
	if mock.requests.Load() != 3 || mock.writes.Load() != 0 {
		t.Fatalf("probe did not issue exactly three GET-only requests: %d %d", mock.requests.Load(), mock.writes.Load())
	}
}

func TestPinnedTLSProbeFailsClosedForUntrustedTransportAndPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*mockRestrictedDaemon, *PinnedTLSProbe)
	}{
		{"wrong-server-pin", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) { p.ServerCertificateSHA256 = strings.Repeat("0", 64) }},
		{"wrong-server-certificate", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) {
			p.ServerCertificateDER = ephemeralProbeCertificate(t).Certificate[0]
		}},
		{"wrong-client-certificate", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) { p.ClientCertificate = ephemeralProbeCertificate(t) }},
		{"reported-identity-mismatch", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.identity = strings.Repeat("f", 64) }},
		{"broad-certificate", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.restricted = false }},
		{"foreign-project", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.projects = []string{"aginctus", "foreign"} }},
		{"disk-policy-widening", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.projectConfig["restricted.devices.disk"] = "allow" }},
		{"denied", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.status = http.StatusForbidden }},
		{"redirect", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.redirect = true }},
		{"malformed-envelope", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.nonJSON = true }},
		{"oversized-response", func(m *mockRestrictedDaemon, _ *PinnedTLSProbe) { m.oversize = true }},
		{"plaintext-endpoint", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) { p.Endpoint = "http://127.0.0.1:8443" }},
		{"user-info", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) {
			p.Endpoint = "https://operator:password@127.0.0.1:8443"
		}},
		{"invalid-server-pin", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) { p.ServerCertificateSHA256 = "latest" }},
		{"absent-client-cert", func(_ *mockRestrictedDaemon, p *PinnedTLSProbe) { p.ClientCertificate = tls.Certificate{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock, server, config := newMockRestrictedDaemon(t)
			defer server.Close()
			tc.edit(mock, &config)
			if _, err := ObserveRestrictedOperatorOverTLS(context.Background(), config, "aginctus", "hm-mgmt", "default"); err == nil {
				t.Fatal("untrusted TLS or authorization policy returned positive evidence")
			}
			if mock.writes.Load() != 0 {
				t.Fatal("probe issued an HTTP write")
			}
		})
	}
}

func TestPinnedTLSProbeRefusesCancellationAndDefaultScope(t *testing.T) {
	mock, server, config := newMockRestrictedDaemon(t)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ObserveRestrictedOperatorOverTLS(ctx, config, "aginctus", "hm-mgmt", "default"); err != context.Canceled {
		t.Fatalf("canceled probe succeeded: %v", err)
	}
	if _, err := ObserveRestrictedOperatorOverTLS(context.Background(), config, "default", "hm-mgmt", "default"); err == nil {
		t.Fatal("default Incus project was accepted")
	}
	if mock.requests.Load() != 0 {
		t.Fatal("invalid or canceled probe made a server request")
	}
}
