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
	"testing"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type fixtureRestrictedIncusObserver struct {
	conn *incusclient.ConnectionInfo
	server *api.Server
	selected *api.Project
	foreign *api.Project
	foreignErr error
	second *incusclient.ConnectionInfo
	infoCalls int
	selectedCalls int
	foreignCalls int
}

func (f *fixtureRestrictedIncusObserver) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	f.infoCalls++
	if f.infoCalls > 1 && f.second != nil { return f.second, nil }
	return f.conn, nil
}
func (f *fixtureRestrictedIncusObserver) GetServer() (*api.Server, string, error) {
	return f.server, "server-etag", nil
}
func (f *fixtureRestrictedIncusObserver) GetProject(project string) (*api.Project, string, error) {
	if project == "default" {
		f.foreignCalls++
		return f.foreign, "", f.foreignErr
	}
	f.selectedCalls++
	return f.selected, "project-etag", nil
}

func restrictedIncusVerifierFixture(t *testing.T) (RestrictedIncusProjectVerifier, *fixtureRestrictedIncusObserver) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatal(err) }
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"incus.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil { t.Fatal(err) }
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	hash := sha256.Sum256(der)
	observer := &fixtureRestrictedIncusObserver{
		conn: &incusclient.ConnectionInfo{
			Protocol: "incus", Project: "team", URL: "https://incus.test:8443", Certificate: certificate,
		},
		server: &api.Server{ServerUntrusted: api.ServerUntrusted{Auth: "trusted"}},
		selected: &api.Project{Name: "team"},
		foreignErr: api.StatusErrorf(http.StatusForbidden, "not authorized"),
	}
	return RestrictedIncusProjectVerifier{
		Observer: observer, PinnedServerCertificateSHA256: hex.EncodeToString(hash[:]),
	}, observer
}

func TestRestrictedIncusVerifierProvesPinnedProjectAndDefaultDenial(t *testing.T) {
	verifier, observer := restrictedIncusVerifierFixture(t)
	got, err := verifier.VerifyProject(context.Background(), "team")
	if err != nil || got.ServerID != "sha256:"+verifier.PinnedServerCertificateSHA256 ||
		got.Project != "team" || observer.selectedCalls != 1 || observer.foreignCalls != 1 ||
		observer.infoCalls != 2 {
		t.Fatalf("verified=%+v err=%v selected=%d foreign=%d info=%d", got, err,
			observer.selectedCalls, observer.foreignCalls, observer.infoCalls)
	}
}

func TestRestrictedIncusVerifierFailsClosedForUntrustedOrBroadConnections(t *testing.T) {
	for _, tc := range []struct{
		name string
		change func(*RestrictedIncusProjectVerifier, *fixtureRestrictedIncusObserver)
	}{
		{"administrator can read default", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.foreign = &api.Project{Name: "default"}; f.foreignErr = nil
		}},
		{"foreign request transport failure", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.foreignErr = errors.New("private transport failure")
		}},
		{"wrong project", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.conn.Project = "other"
		}},
		{"local administrative socket", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.conn.SocketPath = "/var/lib/incus/unix.socket"
		}},
		{"unencrypted transport", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.conn.URL = "http://incus.test:8443"
		}},
		{"untrusted peer", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.server.Auth = "untrusted"
		}},
		{"server pin mismatch", func(v *RestrictedIncusProjectVerifier, _ *fixtureRestrictedIncusObserver) {
			v.PinnedServerCertificateSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
		}},
		{"missing project", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			f.selected = nil
		}},
		{"changed connection", func(_ *RestrictedIncusProjectVerifier, f *fixtureRestrictedIncusObserver) {
			c := *f.conn
			c.URL = "https://foreign.test:8443"
			f.second = &c
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier, observer := restrictedIncusVerifierFixture(t)
			tc.change(&verifier, observer)
			result, err := verifier.VerifyProject(context.Background(), "team")
			if err == nil || result != (Binding{}) {
				t.Fatalf("unsafe connection accepted: %+v %v", result, err)
			}
		})
	}
}

func TestRestrictedIncusVerifierRequiresExplicitScopeAndContext(t *testing.T) {
	verifier, observer := restrictedIncusVerifierFixture(t)
	var noContext context.Context
	if result, err := verifier.VerifyProject(noContext, "team"); err == nil || result != (Binding{}) ||
		observer.infoCalls != 0 {
		t.Fatalf("nil-context query permitted: result=%+v err=%v", result, err)
	}
	for _, project := range []string{"", "default", "other"} {
		if result, err := verifier.VerifyProject(context.Background(), project); err == nil || result != (Binding{}) {
			t.Fatalf("invalid project %q accepted: %+v %v", project, result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := verifier.VerifyProject(ctx, "team"); !errors.Is(err, context.Canceled) ||
		result != (Binding{}) {
		t.Fatalf("cancellation ignored: %+v %v", result, err)
	}
}
