package environment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const maxRestrictedOperatorConfigBytes = 48 << 10

// RestrictedOperatorReadConfig is an explicit, user-owned credential selection
// for the restricted Incus TLS read path. It must never be generated into a
// world-readable Nix store, log, CLI error, or configuration candidate.
// The server/project are still independently verified by Incus; this file is
// not an authorization grant.
type RestrictedOperatorReadConfig struct {
	APIVersion string `json:"apiVersion"`
	Endpoint string `json:"endpoint"`
	Project string `json:"project"`
	ServerCertificatePEM string `json:"serverCertificatePem"`
	TrustedServerSHA256 string `json:"trustedServerSha256"`
	ClientCertificatePEM string `json:"clientCertificatePem"`
	ClientPrivateKeyPEM string `json:"clientPrivateKeyPem"`
	StateServiceEndpoint string `json:"stateServiceEndpoint,omitempty"`
	StateServiceCertificatePEM string `json:"stateServiceCertificatePem,omitempty"`
	StateServiceSHA256 string `json:"stateServiceSha256,omitempty"`
	ConfiguredCandidateFile string `json:"configuredCandidateFile,omitempty"`
}

const RestrictedOperatorReadConfigVersion = "aginctus.dev/restricted-operator-read/v1alpha1"

// ReadRestrictedOperatorReadConfig reads exactly one explicitly selected,
// owner-private regular file. It follows no final-component symlinks, cannot
// block on FIFOs, rejects duplicate or unknown keys, and never falls back to
// XDG, Incus local remotes, an administrator socket, or candidate input.
func ReadRestrictedOperatorReadConfig(path string) (RestrictedOperatorReadConfig, error) {
	invalid := func() (RestrictedOperatorReadConfig, error) {
		return RestrictedOperatorReadConfig{}, errors.New("restricted operator read configuration unavailable")
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return invalid()
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil { return invalid() }
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() ||
		info.Size() <= 0 || info.Size() > maxRestrictedOperatorConfigBytes ||
		info.Mode().Perm()&0o077 != 0 {
		return invalid()
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) { return invalid() }
	raw, err := io.ReadAll(io.LimitReader(file, maxRestrictedOperatorConfigBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxRestrictedOperatorConfigBytes {
		return invalid()
	}
	if rejectCandidateDuplicateKeys(raw) != nil { return invalid() }
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil { return invalid() }
	// An all-or-none optional state block avoids quietly dropping one
	// source and accidentally presenting partial state as complete.
	keys := []string{
		"apiVersion", "endpoint", "project", "serverCertificatePem",
		"trustedServerSha256", "clientCertificatePem", "clientPrivateKeyPem",
	}
	if len(fields) == 11 {
		keys = append(keys, "stateServiceEndpoint", "stateServiceCertificatePem",
			"stateServiceSha256", "configuredCandidateFile")
	}
	if candidateKeys(fields, keys...) != nil { return invalid() }
	var selected RestrictedOperatorReadConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&selected) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		selected.APIVersion != RestrictedOperatorReadConfigVersion ||
		selected.Endpoint == "" || selected.Project == "" ||
		selected.ServerCertificatePEM == "" || selected.TrustedServerSHA256 == "" ||
		selected.ClientCertificatePEM == "" || selected.ClientPrivateKeyPEM == "" ||
		(len(fields) == 11 && (selected.StateServiceEndpoint == "" ||
		selected.StateServiceCertificatePEM == "" || selected.StateServiceSHA256 == "" ||
		selected.ConfiguredCandidateFile == "")) {
		return invalid()
	}
	return selected, nil
}

func (c RestrictedOperatorReadConfig) Connection() ExplicitRestrictedIncusConnection {
	return ExplicitRestrictedIncusConnection{
		URL: c.Endpoint, Project: c.Project,
		ServerCertificatePEM: c.ServerCertificatePEM,
		TrustedServerSHA256: c.TrustedServerSHA256,
		ClientCertificatePEM: c.ClientCertificatePEM,
		ClientPrivateKeyPEM: c.ClientPrivateKeyPEM,
	}
}
