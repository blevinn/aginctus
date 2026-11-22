package environment

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// RestrictedIncusProjectObserver is a deliberately read-only subset of an
// Incus InstanceServer. Production callers must supply an actual TLS SDK
// connection constructed with a separately trusted, pinned server certificate
// and a project-restricted client credential. The interface is not proof of
// those properties when implemented by a caller-supplied fake.
type RestrictedIncusProjectObserver interface {
	GetConnectionInfo() (*incusclient.ConnectionInfo, error)
	GetServer() (*api.Server, string, error)
	GetProject(string) (*api.Project, string, error)
}

// RestrictedIncusProjectVerifier checks a pinned remote TLS Incus connection,
// an authenticated principal, exact selected project, and an independently
// denied default-project request. This rejects administrative socket access
// and unrestricted credentials on a standard Incus TLS authorization setup.
//
// The pinned SHA-256 fingerprint MUST be supplied by an independently trusted
// administrator provisioning path, never by a candidate, environment name,
// remote alias or unverified instance label. Real Incus restricted-principal
// negative VM evidence is still required before activating this verifier.
type RestrictedIncusProjectVerifier struct {
	Observer RestrictedIncusProjectObserver
	PinnedServerCertificateSHA256 string
}

func incusServerCertificateFingerprint(pemText string) (string, error) {
	block, rest := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" || strings.TrimSpace(string(rest)) != "" {
		return "", errors.New("invalid pinned Incus server certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", errors.New("invalid pinned Incus server certificate")
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:]), nil
}

func (v RestrictedIncusProjectVerifier) VerifyProject(ctx context.Context, project string) (Binding, error) {
	if ctx == nil {
		return Binding{}, errors.New("restricted Incus verification requires context")
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	scope, err := incusscope.NewProjectScope(project)
	if err != nil || project == "default" || v.Observer == nil ||
		len(v.PinnedServerCertificateSHA256) != 64 {
		return Binding{}, errors.New("pinned restricted Incus project required")
	}
	for _, ch := range v.PinnedServerCertificateSHA256 {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return Binding{}, errors.New("invalid trusted Incus server pin")
		}
	}
	conn, err := v.Observer.GetConnectionInfo()
	if err != nil || conn == nil || conn.Protocol != "incus" ||
		conn.SocketPath != "" || conn.Project != scope.Name {
		return Binding{}, errors.New("restricted remote project connection required")
	}
	endpoint, parseErr := url.Parse(conn.URL)
	if parseErr != nil || endpoint.Scheme != "https" || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.Fragment != "" {
		return Binding{}, errors.New("authenticated Incus HTTPS connection required")
	}
	fingerprint, err := incusServerCertificateFingerprint(conn.Certificate)
	if err != nil || fingerprint != v.PinnedServerCertificateSHA256 {
		return Binding{}, errors.New("incus server certificate differs from trusted pin")
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	server, _, err := v.Observer.GetServer()
	if err != nil || server == nil || server.Auth != "trusted" {
		return Binding{}, errors.New("incus project principal is not authenticated")
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	selected, etag, err := v.Observer.GetProject(project)
	if err != nil || selected == nil || selected.Name != project || etag == "" {
		return Binding{}, errors.New("authenticated Incus project lookup unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	// A default-project request must be independently denied by the server.
	// The default project exists on Incus; a successful read would indicate
	// an administrative or unexpectedly broad principal. A transport error
	// cannot masquerade as a permission denial.
	foreign, _, denied := v.Observer.GetProject("default")
	if foreign != nil || (!api.StatusErrorCheck(denied, http.StatusForbidden) &&
		!api.StatusErrorCheck(denied, http.StatusNotFound)) {
		return Binding{}, errors.New("restricted Incus principal confinement unverified")
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	again, err := v.Observer.GetConnectionInfo()
	if err != nil || again == nil || again.Protocol != conn.Protocol ||
		again.SocketPath != conn.SocketPath || again.Project != conn.Project ||
		again.Certificate != conn.Certificate || again.URL != conn.URL {
		return Binding{}, errors.New("incus connection identity changed during verification")
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	return Binding{ServerID: "sha256:" + fingerprint, Project: project}, nil
}
