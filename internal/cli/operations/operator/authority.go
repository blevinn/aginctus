package operatorops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
)

// Operator authority probes are GET-only; transport errors never become
// authorization evidence and unverified restriction does not imply safety.
type ReadStatus string

const (
	ReadOK          ReadStatus = "readable"
	ReadDenied      ReadStatus = "denied"
	ReadNoIdentity  ReadStatus = "unauthenticated"
	ReadUnavailable ReadStatus = "unavailable-or-denied"
	ReadNoCLI       ReadStatus = "client-unavailable"
	ReadCanceled    ReadStatus = "canceled"
	ReadTimeout     ReadStatus = "timeout"
	ReadInvalid     ReadStatus = "invalid-response"
)

// Query only GET endpoints and interpret the structured response, never stderr.
// A subprocess transport failure cannot reliably distinguish offline from denied.
func ReadAPI(ctx context.Context, path string, inspect func(context.Context, ...string) ([]byte, error)) (json.RawMessage, ReadStatus) {
	if err := ctx.Err(); err != nil {
		return nil, ReadFailure(err)
	}
	data, err := inspect(ctx, "query", "--raw", "--request", "GET", path)
	if ctx.Err() != nil {
		return nil, ReadFailure(ctx.Err())
	}
	if err != nil {
		return nil, ReadFailure(err)
	}
	var response struct {
		Type       string          `json:"type"`
		StatusCode int             `json:"status_code"`
		ErrorCode  int             `json:"error_code"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &response) != nil {
		return nil, ReadInvalid
	}
	if response.Type == "error" {
		switch response.ErrorCode {
		case 401:
			return nil, ReadNoIdentity
		case 403:
			return nil, ReadDenied
		default:
			return nil, ReadUnavailable
		}
	}
	if response.Type != "sync" || response.StatusCode != 200 || response.ErrorCode != 0 ||
		len(response.Metadata) == 0 || string(response.Metadata) == "null" {
		return nil, ReadInvalid
	}
	return response.Metadata, ReadOK
}

func ReadFailure(err error) ReadStatus {
	switch {
	case errors.Is(err, context.Canceled):
		return ReadCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ReadTimeout
	case errors.Is(err, exec.ErrNotFound):
		return ReadNoCLI
	case errors.Is(err, os.ErrPermission):
		return ReadDenied
	default:
		return ReadUnavailable
	}
}

var CertificateFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

// A certificate restriction is daemon-reported identity evidence, not an
// entitlement check. External authorizers may further constrain any identity.
func InspectIdentity(ctx context.Context, server json.RawMessage, project string, read func(context.Context, string) (json.RawMessage, ReadStatus)) (string, bool) {
	var identity struct {
		Auth   string `json:"auth"`
		Method string `json:"auth_user_method"`
		Name   string `json:"auth_user_name"`
	}
	if json.Unmarshal(server, &identity) != nil {
		return "unproven (invalid daemon identity)", false
	}
	if identity.Auth == "untrusted" {
		return "unauthenticated", false
	}
	if identity.Auth != "trusted" {
		return "unproven (unsupported daemon identity)", false
	}
	if identity.Method != "tls" || !CertificateFingerprint.MatchString(identity.Name) {
		return "trusted; restriction and administrative capabilities unproven", true
	}
	data, status := read(ctx, "/1.0/certificates/"+identity.Name)
	if status != ReadOK {
		return "trusted; credential restriction unavailable (not an administrator proof)", true
	}
	var cert struct {
		Fingerprint string   `json:"fingerprint"`
		Restricted  *bool    `json:"restricted"`
		Projects    []string `json:"projects"`
	}
	if json.Unmarshal(data, &cert) != nil || cert.Fingerprint != identity.Name || cert.Restricted == nil {
		return "trusted; credential restriction unproven (invalid self-certificate response)", true
	}
	if !*cert.Restricted {
		return "broad unrestricted TLS credential; effective administrative capabilities unproven", true
	}
	for _, allowed := range cert.Projects {
		if allowed == project {
			return "restricted TLS credential; selected project declared permitted (writes unproven)", true
		}
	}
	return "restricted TLS credential; selected project not declared permitted", false
}
