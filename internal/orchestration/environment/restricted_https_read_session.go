package environment

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/sha256"
	"encoding/hex"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RestrictedHTTPSReadSessionFactory creates an authenticated mTLS session
// to a separately pinned project-owned state-service HTTPS endpoint. The
// service certificate pin must be provisioned independently of candidates,
// Incus labels and response data. An Incus BindingVerifier must independently
// verify the project-restricted operator identity on the Incus connection.
//
// mTLS does NOT by itself prove the service is authorized to hold project
// state or that its peer authorizer tracks Incus revocation. Those server-side
// gates must be demonstrated before enabling the accepted-state CLI reader.
type RestrictedHTTPSReadSessionFactory struct {
	Endpoint string
	ServiceCertificatePEM string
	ServiceCertificateSHA256 string
	ClientCertificatePEM string
	ClientPrivateKeyPEM string
	Verifier BindingVerifier
}

type restrictedHTTPSReadSession struct {
	project string
	binding Binding
	verifier BindingVerifier
	endpoint string
	client *http.Client
}

func (f RestrictedHTTPSReadSessionFactory) OpenReadSession(
	ctx context.Context, enrolled Descriptor,
) (RestrictedProjectReadSession, error) {
	fail := func() (RestrictedProjectReadSession, error) {
		return nil, errors.New("restricted project-state HTTPS session unavailable")
	}
	if ctx == nil || ctx.Err() != nil || enrolled.Validate() != nil || f.Verifier == nil {
		return fail()
	}
	u, err := url.Parse(f.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" ||
		u.User != nil || u.Fragment != "" || u.RawQuery != "" ||
		(u.Path != "" && u.Path != "/") {
		return fail()
	}
	fingerprint, err := incusServerCertificateFingerprint(f.ServiceCertificatePEM)
	if err != nil || fingerprint != f.ServiceCertificateSHA256 { return fail() }
	cert, err := tls.X509KeyPair([]byte(f.ClientCertificatePEM), []byte(f.ClientPrivateKeyPEM))
	if err != nil { return fail() }
	// This private root pool contains only the independently pinned service
	// certificate. Go verifies DNS and X.509 chains; verification is NEVER
	// skipped or replaced with an insecure custom comparison callback.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(f.ServiceCertificatePEM)) { return fail() }
	binding, err := f.Verifier.VerifyProject(ctx, enrolled.Binding.Project)
	if err != nil || binding != enrolled.Binding || ctx.Err() != nil { return fail() }
	transport := &http.Transport{
		Proxy: nil,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12, RootCAs: pool,
			Certificates: []tls.Certificate{cert},
			// Keep normal X.509/hostname verification enabled, then require
			// exactly the pinned leaf, even if the supplied cert is a CA.
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 { return errors.New("missing service TLS peer") }
				fingerprint := sha256.Sum256(state.PeerCertificates[0].Raw)
				if hex.EncodeToString(fingerprint[:]) != f.ServiceCertificateSHA256 {
					return errors.New("project state service TLS leaf differs from trusted pin")
				}
				return nil
			},
		},
		DisableKeepAlives: true,
		TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &restrictedHTTPSReadSession{
		project: enrolled.Binding.Project,
		binding: binding,
		verifier: f.Verifier,
		endpoint: strings.TrimSuffix(f.Endpoint, "/"),
		client: &http.Client{
			Transport: transport,
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("redirects forbidden on accepted-state authority")
			},
		},
	}, nil
}

func (s *restrictedHTTPSReadSession) VerifiedBinding(ctx context.Context) (Binding, error) {
	if ctx == nil { return Binding{}, errors.New("restricted session requires context") }
	if err := ctx.Err(); err != nil { return Binding{}, err }
	binding, err := s.verifier.VerifyProject(ctx, s.project)
	if err != nil || binding != s.binding {
		return Binding{}, errors.New("restricted project binding unavailable")
	}
	if err := ctx.Err(); err != nil { return Binding{}, err }
	return binding, nil
}

func (s *restrictedHTTPSReadSession) ReadPublished(
	ctx context.Context, id string,
) (*AcceptedRevision, *Candidate, error) {
	deny := func() (*AcceptedRevision, *Candidate, error) {
		return nil, nil, errors.New("authenticated published state unavailable")
	}
	if ctx == nil || !idPattern.MatchString(id) || s == nil || s.client == nil {
		return deny()
	}
	if _, err := s.VerifiedBinding(ctx); err != nil { return deny() }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.endpoint+"/v1/accepted?environment="+url.QueryEscape(id), nil)
	if err != nil { return deny() }
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil { return deny() }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK ||
		resp.Header.Get("Content-Type") != "application/json" {
		return deny()
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAcceptedReadWireBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxAcceptedReadWireBytes ||
		rejectCandidateDuplicateKeys(raw) != nil {
		return deny()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 4 {
		return deny()
	}
	for _, field := range []string{"apiVersion", "environmentId", "revision", "candidate"} {
		if _, ok := fields[field]; !ok { return deny() }
	}
	var result acceptedReadWireResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		result.APIVersion != AcceptedReadWireVersion || result.EnvironmentID != id {
		return deny()
	}
	if result.Revision == nil {
		if !bytes.Equal(bytes.TrimSpace(fields["candidate"]), []byte("null")) { return deny() }
		if _, err := s.VerifiedBinding(ctx); err != nil { return deny() }
		return nil, nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(fields["candidate"]), []byte("null")) { return deny() }
	candidate, err := ParseCandidate(result.Candidate)
	if err != nil || result.Revision.ValidateAgainst(candidate) != nil ||
		result.Revision.EnvironmentID != id ||
		result.Revision.ServerID != s.binding.ServerID ||
		result.Revision.Project != s.binding.Project ||
		candidate.Environment.Binding != s.binding {
		return deny()
	}
	if _, err := s.VerifiedBinding(ctx); err != nil { return deny() }
	return result.Revision, &candidate, nil
}
