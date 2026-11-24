package environment

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"encoding/pem"
	"strings"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
)

// ExplicitRestrictedIncusConnection requires independently provisioned
// certificate material. No fields are read from environment candidates,
// local Incus remotes, XDG configuration, or a host administrative socket.
// Supplying PEM strings does not prove client project restriction: the Incus
// server must enforce it, and the verifier checks negative authorization.
type ExplicitRestrictedIncusConnection struct {
	URL string
	Project string
	ServerCertificatePEM string
	TrustedServerSHA256 string
	ClientCertificatePEM string
	ClientPrivateKeyPEM string
}

// OpenExplicitRestrictedIncusProject connects only to an explicitly selected
// HTTPS Incus server with a separately pinned X.509 server certificate and
// a caller-provisioned client keypair. Successful authentication alone never
// establishes project confinement: the restricted verifier must pass too.
//
// The result is for read-only project-state adapters. This SDK interface can
// perform writes, so it must never escape into a lower-trust service handler.
func OpenExplicitRestrictedIncusProject(
	ctx context.Context, c ExplicitRestrictedIncusConnection,
) (incusclient.InstanceServer, error) {
	if ctx == nil { return nil, errors.New("restricted Incus connection requires context") }
	if err := ctx.Err(); err != nil { return nil, err }
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Fragment != "" || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("explicit HTTPS Incus endpoint required")
	}
	if _, err := net.LookupPort("tcp", u.Port()); u.Port() != "" && err != nil {
		return nil, errors.New("invalid Incus HTTPS port")
	}
	if c.Project == "" || c.Project == "default" ||
		c.ClientCertificatePEM == "" || c.ClientPrivateKeyPEM == "" ||
		c.ServerCertificatePEM == "" {
		return nil, errors.New("explicit project and independent TLS credentials required")
	}
	if err := (Descriptor{
		ID: "11111111-1111-4111-8111-111111111111", Name: "verified",
		Binding: Binding{ServerID: "sha256:" + c.TrustedServerSHA256, Project: c.Project},
	}).Validate(); err != nil {
		return nil, errors.New("invalid explicit project or server identity")
	}
	fingerprint, err := incusServerCertificateFingerprint(c.ServerCertificatePEM)
	if err != nil || fingerprint != c.TrustedServerSHA256 {
		return nil, errors.New("trusted Incus server certificate pin mismatch")
	}
	serverBlock, _ := pem.Decode([]byte(c.ServerCertificatePEM))
	serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
	if err != nil || time.Now().Before(serverCert.NotBefore) || time.Now().After(serverCert.NotAfter) ||
		serverCert.VerifyHostname(u.Hostname()) != nil {
		return nil, errors.New("incus server certificate invalid for selected endpoint")
	}
	if _, err := tls.X509KeyPair([]byte(c.ClientCertificatePEM), []byte(c.ClientPrivateKeyPEM)); err != nil {
		return nil, errors.New("invalid restricted client TLS keypair")
	}
	// Use the SDK's verified server-certificate option. Do not disable TLS
	// verification, invent an Incus remote alias, or fall back to Unix sockets.
	remote, err := incusclient.ConnectIncusWithContext(ctx, strings.TrimSuffix(c.URL, "/"), &incusclient.ConnectionArgs{
		TLSServerCert: c.ServerCertificatePEM,
		TLSClientCert: c.ClientCertificatePEM,
		TLSClientKey: c.ClientPrivateKeyPEM,
		InsecureSkipVerify: false,
		IdenticalCertificate: false,
		SkipGetEvents: true,
	})
	if err != nil { return nil, errors.New("restricted Incus TLS connection unavailable") }
	scoped := remote.UseProject(c.Project)
	if _, err := (RestrictedIncusProjectVerifier{
		Observer: scoped, PinnedServerCertificateSHA256: c.TrustedServerSHA256,
	}).VerifyProject(ctx, c.Project); err != nil {
		remote.Disconnect()
		return nil, errors.New("restricted Incus project credential verification failed")
	}
	if err := ctx.Err(); err != nil {
		remote.Disconnect()
		return nil, err
	}
	return scoped, nil
}
