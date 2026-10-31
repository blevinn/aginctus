package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
)

type operatorReadStatus string

const (
	operatorReadOK          operatorReadStatus = "readable"
	operatorReadDenied      operatorReadStatus = "denied"
	operatorReadNoIdentity  operatorReadStatus = "unauthenticated"
	operatorReadUnavailable operatorReadStatus = "unavailable-or-denied"
	operatorReadNoCLI       operatorReadStatus = "client-unavailable"
	operatorReadCanceled    operatorReadStatus = "canceled"
	operatorReadTimeout     operatorReadStatus = "timeout"
	operatorReadInvalid     operatorReadStatus = "invalid-response"
)

// Query only GET endpoints and interpret the structured response, never stderr.
// A subprocess transport failure cannot reliably distinguish offline from denied.
func readOperatorAPI(ctx context.Context, path string) (json.RawMessage, operatorReadStatus) {
	if err := ctx.Err(); err != nil {
		return nil, operatorReadFailure(err)
	}
	data, err := inspectOperatorIncus(ctx, "query", "--raw", "--request", "GET", path)
	if ctx.Err() != nil {
		return nil, operatorReadFailure(ctx.Err())
	}
	if err != nil {
		return nil, operatorReadFailure(err)
	}
	var response struct {
		Type       string          `json:"type"`
		StatusCode int             `json:"status_code"`
		ErrorCode  int             `json:"error_code"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &response) != nil {
		return nil, operatorReadInvalid
	}
	if response.Type == "error" {
		switch response.ErrorCode {
		case 401:
			return nil, operatorReadNoIdentity
		case 403:
			return nil, operatorReadDenied
		default:
			return nil, operatorReadUnavailable
		}
	}
	if response.Type != "sync" || response.StatusCode != 200 || response.ErrorCode != 0 ||
		len(response.Metadata) == 0 || string(response.Metadata) == "null" {
		return nil, operatorReadInvalid
	}
	return response.Metadata, operatorReadOK
}

func operatorReadFailure(err error) operatorReadStatus {
	switch {
	case errors.Is(err, context.Canceled):
		return operatorReadCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return operatorReadTimeout
	case errors.Is(err, exec.ErrNotFound):
		return operatorReadNoCLI
	case errors.Is(err, os.ErrPermission):
		return operatorReadDenied
	default:
		return operatorReadUnavailable
	}
}

var operatorCertificateFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

// A certificate restriction is daemon-reported identity evidence, not an
// entitlement check. External authorizers may further constrain any identity.
func inspectOperatorIdentity(ctx context.Context, server json.RawMessage, project string) (string, bool) {
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
	if identity.Method != "tls" || !operatorCertificateFingerprint.MatchString(identity.Name) {
		return "trusted; restriction and administrative capabilities unproven", true
	}
	data, status := readOperatorAPI(ctx, "/1.0/certificates/"+identity.Name)
	if status != operatorReadOK {
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
