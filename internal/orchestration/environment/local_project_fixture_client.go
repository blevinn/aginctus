package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

const maxLocalFixtureResponseBytes = 16 << 10

// LocalFixtureRevisionStatus is an explicitly untrusted, SAME-UID socket
// diagnostic. It carries revision identity only, not an authenticated accepted
// candidate or proof of project rights.
type LocalFixtureRevisionStatus struct {
	EnvironmentID string           `json:"environmentId"`
	Revision      AcceptedRevision `json:"revision"`
}

// ReadLocalFixtureRevisionStatus speaks to the opt-in Unix-socket fixture.
// It never uses an ambient remote, HTTP proxy, TCP fallback, Incus credential,
// project query parameter, or production PublishedAcceptedSource adapter.
//
// A 404 does NOT mean an authenticated absence; it means the fixture has no
// record or enrollment for this UUID. The caller cannot use this API to
// authorize acceptance, adoption, reconciliation or cross-UID access.
func ReadLocalFixtureRevisionStatus(
	ctx context.Context, socket string, enrolled Descriptor,
) (LocalFixtureRevisionStatus, error) {
	if ctx == nil || enrolled.Validate() != nil || !filepath.IsAbs(socket) ||
		filepath.Clean(socket) != socket {
		return LocalFixtureRevisionStatus{}, errors.New("explicit fixture socket and valid environment required")
	}
	if err := ctx.Err(); err != nil {
		return LocalFixtureRevisionStatus{}, err
	}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("fixture redirects forbidden")
		},
	}
	endpoint := "http://unix/v1/accepted?environment=" + url.QueryEscape(enrolled.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return LocalFixtureRevisionStatus{}, errors.New("invalid fixture read request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return LocalFixtureRevisionStatus{}, errors.New("fixture accepted-state read failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK ||
		resp.Header.Get("Content-Type") != "application/json" {
		return LocalFixtureRevisionStatus{}, errors.New("fixture accepted revision unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxLocalFixtureResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxLocalFixtureResponseBytes {
		return LocalFixtureRevisionStatus{}, errors.New("invalid fixture response size")
	}
	if err := rejectCandidateDuplicateKeys(raw); err != nil {
		return LocalFixtureRevisionStatus{}, errors.New("invalid fixture response JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil ||
		candidateKeys(fields, "environmentId", "revision") != nil {
		return LocalFixtureRevisionStatus{}, errors.New("invalid fixture response fields")
	}
	var result LocalFixtureRevisionStatus
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return LocalFixtureRevisionStatus{}, errors.New("invalid fixture response data")
	}
	if result.EnvironmentID != enrolled.ID ||
		result.Revision.EnvironmentID != enrolled.ID ||
		result.Revision.ServerID != enrolled.Binding.ServerID ||
		result.Revision.Project != enrolled.Binding.Project ||
		result.Revision.ValidateIdentity() != nil {
		return LocalFixtureRevisionStatus{}, errors.New("foreign or corrupt fixture revision")
	}
	if err := ctx.Err(); err != nil {
		return LocalFixtureRevisionStatus{}, err
	}
	return result, nil
}
