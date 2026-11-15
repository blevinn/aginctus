package controllerauthority

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxIncusGETBytes int64 = 1 << 20

// PinnedTLSProbe is non-mutating configuration, not a controller credential
// or a write grant. The endpoint and server pin must come from an independently
// trusted host source, never a Home Manager candidate or the server itself.
type PinnedTLSProbe struct {
	Endpoint                string
	ServerCertificateSHA256 string
	ServerCertificateDER    []byte
	ClientCertificate       tls.Certificate
}

// ObserveRestrictedOperatorOverTLS uses an actual HTTPS client-certificate
// session to collect read-only daemon identity and project policy evidence.
// Each GET uses a new connection: no ambient Incus CLI remote, local admin
// socket, persistent authenticated session or HTTP redirects are supported.
//
// The exact server leaf SHA256 pin is the trust anchor for self-signed Incus
// servers. This function does NOT authenticate the invoking OS principal,
// hold an exclusive writer fence, or authorize any native Incus mutation.
func ObserveRestrictedOperatorOverTLS(
	ctx context.Context, config PinnedTLSProbe, project, bridge, pool string,
) (DaemonTLSObservation, error) {
	if ctx == nil {
		return DaemonTLSObservation{}, errors.New("pinned TLS inspection requires context")
	}
	if err := ctx.Err(); err != nil {
		return DaemonTLSObservation{}, err
	}
	base, err := url.Parse(config.Endpoint)
	if err != nil || base.Scheme != "https" || base.Hostname() == "" ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		(base.Path != "" && base.Path != "/") ||
		strings.HasSuffix(base.Host, ":") || !hashPattern.MatchString(config.ServerCertificateSHA256) {
		return DaemonTLSObservation{}, errors.New("pinned TLS inspection requires an exact HTTPS endpoint and server certificate")
	}
	if len(config.ClientCertificate.Certificate) == 0 || config.ClientCertificate.PrivateKey == nil {
		return DaemonTLSObservation{}, errors.New("pinned TLS inspection requires an explicit client certificate and private key")
	}
	serverCertificate, err := x509.ParseCertificate(config.ServerCertificateDER)
	if err != nil {
		return DaemonTLSObservation{}, errors.New("pinned TLS inspection requires the pinned server certificate")
	}
	serverSum := sha256.Sum256(serverCertificate.Raw)
	if hex.EncodeToString(serverSum[:]) != config.ServerCertificateSHA256 {
		return DaemonTLSObservation{}, errors.New("pinned TLS server certificate does not match its fingerprint")
	}
	clientLeaf, err := x509.ParseCertificate(config.ClientCertificate.Certificate[0])
	if err != nil || time.Now().Before(clientLeaf.NotBefore) || time.Now().After(clientLeaf.NotAfter) {
		return DaemonTLSObservation{}, errors.New("pinned TLS client certificate is invalid or expired")
	}
	clientSum := sha256.Sum256(clientLeaf.Raw)
	clientFingerprint := hex.EncodeToString(clientSum[:])
	serverRoots := x509.NewCertPool()
	serverRoots.AddCert(serverCertificate)
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   base.Hostname(),
		RootCAs:      serverRoots,
		Certificates: []tls.Certificate{config.ClientCertificate},
		// The pinned certificate is the only trust anchor; normal TLS chain,
		// validity, and hostname checks remain enabled.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("incus server certificate unavailable")
			}
			cert := state.PeerCertificates[0]
			sum := sha256.Sum256(cert.Raw)
			if hex.EncodeToString(sum[:]) != config.ServerCertificateSHA256 ||
				time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
				return errors.New("incus server certificate pin or validity mismatch")
			}
			return nil
		},
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("incus GET redirect refused")
		},
	}
	reader := func(ctx context.Context, path string) (json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target, err := url.Parse(path)
		if err != nil || target.IsAbs() || target.Host != "" || target.User != nil ||
			target.Fragment != "" || (target.Path != "/1.0" && !strings.HasPrefix(target.Path, "/1.0/")) {
			return nil, errors.New("invalid pinned incus GET path")
		}
		dest := *base
		dest.Path = target.Path
		dest.RawQuery = target.RawQuery
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dest.String(), nil)
		if err != nil {
			return nil, errors.New("cannot build restricted incus GET")
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("restricted incus GET unavailable: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("restricted incus GET denied or unavailable")
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIncusGETBytes+1))
		if err != nil || int64(len(raw)) > maxIncusGETBytes {
			return nil, errors.New("restricted incus GET oversized or unavailable")
		}
		var envelope struct {
			Type       string          `json:"type"`
			StatusCode int             `json:"status_code"`
			ErrorCode  int             `json:"error_code"`
			Metadata   json.RawMessage `json:"metadata"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Type != "sync" ||
			envelope.StatusCode != 200 || envelope.ErrorCode != 0 ||
			len(envelope.Metadata) == 0 || string(envelope.Metadata) == "null" {
			return nil, errors.New("invalid restricted incus GET envelope")
		}
		return envelope.Metadata, nil
	}
	observed, err := ObserveRestrictedOperator(ctx, project, bridge, pool, reader)
	if err != nil {
		return DaemonTLSObservation{}, err
	}
	if observed.CertificateFingerprint != clientFingerprint {
		return DaemonTLSObservation{}, errors.New("daemon identity differs from the connected TLS client certificate")
	}
	return observed, nil
}
