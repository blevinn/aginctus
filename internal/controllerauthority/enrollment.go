// Package controllerauthority implements strictly bounded, non-executing
// host controller enrollment evidence. It does not create trusted leases,
// authenticate Incus, or confer a capability to mutate infrastructure.
package controllerauthority

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const (
	EnrollmentVersion = "aginctus.dev/controller-enrollment/v1alpha1"
	// The only production source of these records is an administrator-owned
	// directory. Do not accept an alternate path from CLI/XDG/HTTP callers.
	hostEnrollmentDir  = "/etc/aginctus"
	hostEnrollmentFile = "controller-enrollment.json"
	maxEnrollmentBytes = 128 << 10
	maxEnrollments     = 256
)

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
	hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Enrollment struct {
	ControllerID string   `json:"controllerID"`
	Domain       string   `json:"domain"`
	UID          uint32   `json:"uid"`
	Certificate  string   `json:"certificateFingerprint"`
	DeploymentID string   `json:"deploymentID"`
	IncusProject string   `json:"incusProject"`
	WorkloadID   string   `json:"workloadID"`
	RevisionID   string   `json:"revisionID"`
	Generation   uint64   `json:"generation"`
	Epoch        uint64   `json:"epoch"`
	Actions      []string `json:"actions"`
	Status       string   `json:"status"`
}

type enrollmentFile struct {
	APIVersion  string       `json:"apiVersion"`
	Enrollments []Enrollment `json:"enrollments"`
}

// HostEvidence can only be constructed within this package by validating an
// owner-anchored file. It is a snapshot and cannot itself fence another writer.
// A subsequent check must re-read the host's current authority before a write.
type HostEvidence struct {
	entries []Enrollment
}

func (e Enrollment) validate() error {
	if !idPattern.MatchString(e.ControllerID) || !idPattern.MatchString(e.DeploymentID) ||
		!idPattern.MatchString(e.WorkloadID) || !idPattern.MatchString(e.IncusProject) ||
		e.IncusProject == "default" || !hashPattern.MatchString(e.RevisionID) ||
		e.Generation == 0 || e.Epoch == 0 ||
		(e.Status != "active" && e.Status != "revoked") ||
		len(e.Actions) == 0 || len(e.Actions) > 2 {
		return errors.New("invalid or unbounded controller enrollment")
	}
	switch e.Domain {
	case "host":
		if e.UID != 0 || e.Certificate != "" {
			return errors.New("host enrollment must remain root-owned")
		}
	case "operator":
		if e.UID == 0 || !hashPattern.MatchString(e.Certificate) {
			return errors.New("operator enrollment requires a nonroot UID and exact TLS certificate")
		}
	default:
		return errors.New("unsupported controller authority domain")
	}
	seen := map[string]bool{}
	for _, a := range e.Actions {
		if a != "start" && a != "stop" || seen[a] {
			return errors.New("unexpected or duplicate controller action")
		}
		seen[a] = true
	}
	return nil
}

func resourceKey(e Enrollment) string {
	return e.DeploymentID + "/" + e.IncusProject + "/" + e.WorkloadID
}

// ParseEnrollmentFile is useful for testing syntax but its output is NOT
// trusted evidence. Only LoadHostEnrollmentEvidence pins a root-owned source.
func ParseEnrollmentFile(raw []byte) error {
	_, err := parse(raw)
	return err
}

func parse(raw []byte) ([]Enrollment, error) {
	if len(raw) == 0 || len(raw) > maxEnrollmentBytes {
		return nil, errors.New("invalid controller enrollment size")
	}
	if err := strictJSON(raw); err != nil {
		return nil, err
	}
	var data enrollmentFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return nil, errors.New("invalid controller enrollment JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing controller enrollment JSON")
	}
	if data.APIVersion != EnrollmentVersion || data.Enrollments == nil || len(data.Enrollments) > maxEnrollments {
		return nil, errors.New("unsupported or oversized controller enrollment")
	}
	seen := make(map[string]bool, len(data.Enrollments))
	for _, e := range data.Enrollments {
		if err := e.validate(); err != nil {
			return nil, err
		}
		key := resourceKey(e)
		if seen[key] {
			return nil, errors.New("duplicate resource enrollment across controller domains")
		}
		seen[key] = true
	}
	return data.Enrollments, nil
}

// strictJSON forbids duplicate case-folded keys, unknown names, missing keys,
// implicit nulls, case aliases, nested or arbitrary host/device configuration.
func strictJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	seenObject := 0
	var walk func(map[string]bool) error
	walk = func(fields map[string]bool) error {
		token, err := dec.Token()
		if err != nil {
			return errors.New("invalid enrollment JSON")
		}
		d, ok := token.(json.Delim)
		if !ok || d != '{' {
			return errors.New("controller enrollment expects objects")
		}
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return errors.New("invalid enrollment field")
			}
			name, ok := key.(string)
			if !ok || !fields[name] || seen[strings.ToLower(name)] {
				return errors.New("unknown or duplicate enrollment field")
			}
			seen[strings.ToLower(name)] = true
			if fields == nil {
				return errors.New("invalid enrollment fields")
			}
			switch name {
			case "enrollments":
				next, err := dec.Token()
				if err != nil || next != json.Delim('[') {
					return errors.New("enrollments must be a list")
				}
				for dec.More() {
					seenObject++
					if seenObject > maxEnrollments {
						return errors.New("too many controller enrollments")
					}
					if err := walk(enrollmentKeys); err != nil {
						return err
					}
				}
				if end, err := dec.Token(); err != nil || end != json.Delim(']') {
					return errors.New("invalid enrollment list")
				}
			case "actions":
				next, err := dec.Token()
				if err != nil || next != json.Delim('[') {
					return errors.New("actions must be a list")
				}
				count := 0
				for dec.More() {
					v, err := dec.Token()
					if err != nil {
						return errors.New("invalid action")
					}
					if _, ok := v.(string); !ok {
						return errors.New("controller action must be a string")
					}
					count++
					if count > 2 {
						return errors.New("too many controller actions")
					}
				}
				if end, err := dec.Token(); err != nil || end != json.Delim(']') {
					return errors.New("invalid action list")
				}
			default:
				value, err := dec.Token()
				if err != nil || value == nil {
					return errors.New("null or missing enrollment value")
				}
				if _, nested := value.(json.Delim); nested {
					return errors.New("unexpected nested enrollment value")
				}
			}
		}
		if end, err := dec.Token(); err != nil || end != json.Delim('}') {
			return errors.New("invalid enrollment object")
		}
		if len(seen) != len(fields) {
			return errors.New("missing required enrollment field")
		}
		return nil
	}
	if err := walk(rootKeys); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("extra enrollment JSON")
	}
	return nil
}

var rootKeys = map[string]bool{"apiVersion": true, "enrollments": true}
var enrollmentKeys = map[string]bool{
	"controllerID": true, "domain": true, "uid": true,
	"certificateFingerprint": true, "deploymentID": true,
	"incusProject": true, "workloadID": true, "revisionID": true,
	"generation": true, "epoch": true, "actions": true, "status": true,
}

// LoadHostEnrollmentEvidence loads ONLY the administrator-owned policy under
// /etc/aginctus. No user-provided policy path, environment default, or
// Home Manager declaration is accepted. Missing policy fails closed.
// File contents are nonsecret; security comes from its root ownership, not
// from secrecy or a caller-supplied JSON signature.
func LoadHostEnrollmentEvidence() (*HostEvidence, error) {
	return loadOwnerFile(hostEnrollmentDir, 0)
}

func loadOwnerFile(directory string, owner uint32) (*HostEvidence, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errors.New("enrollment directory must be absolute and normalized")
	}
	// The production parent /etc is root-owned; open the final directory
	// without following a symlink. Do not accept a caller-selected path in
	// production, even if some fake filesystem claims its UID is zero.
	fd, err := syscall.Open(directory, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open host controller authority directory: %w", err)
	}
	defer syscall.Close(fd)
	if err := validateOwnerFD(fd, owner, true); err != nil {
		return nil, err
	}
	fileFD, err := syscall.Openat(fd, hostEnrollmentFile, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open host controller authority file: %w", err)
	}
	file := os.NewFile(uintptr(fileFD), "host controller enrollment")
	defer file.Close()
	if err := validateOwnerFD(fileFD, owner, false); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxEnrollmentBytes+1))
	if err != nil {
		return nil, errors.New("host controller enrollment read failed")
	}
	entries, err := parse(raw)
	if err != nil {
		return nil, err
	}
	// Check the path still points to the opened inode after parsing. This
	// is not a mutation-boundary fence; callers must reload fresh evidence.
	named, err := os.Lstat(filepath.Join(directory, hostEnrollmentFile))
	if err != nil {
		return nil, errors.New("host enrollment path became unavailable")
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(opened, named) {
		return nil, errors.New("host enrollment changed during inspection")
	}
	return &HostEvidence{entries: entries}, nil
}

func validateOwnerFD(fd int, owner uint32, directory bool) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return errors.New("cannot verify host enrollment ownership")
	}
	mode := stat.Mode & syscall.S_IFMT
	if (directory && mode != syscall.S_IFDIR) ||
		(!directory && (mode != syscall.S_IFREG || stat.Nlink != 1)) ||
		stat.Uid != owner || stat.Mode&0o022 != 0 {
		return errors.New("host enrollment must be owner-controlled, non-writable by other principals")
	}
	return nil
}

// RevisionCode is a diagnostics-only selector. No return value from this
// package constitutes authorization, controller exclusion or writer fencing.
type RevisionCode string

const (
	EnrollmentMatched   RevisionCode = "enrollment-matched-not-authorized"
	EnrollmentMissing   RevisionCode = "enrollment-missing"
	EnrollmentRevoked   RevisionCode = "enrollment-revoked"
	EnrollmentMismatch  RevisionCode = "enrollment-mismatch"
	EnrollmentUntrusted RevisionCode = "enrollment-unverified"
)

// AssessedPrincipal MUST originate from an independently authenticated
// server-side peer (UID) and Incus daemon TLS identity, NOT from a CLI flag,
// candidate field, environment variable or caller-supplied certificate.
type AssessedPrincipal struct {
	UID               uint32
	TLSFingerprint    string
	ProjectRestricted bool
}

type SelectedRevision struct {
	DeploymentID string
	Project      string
	WorkloadID   string
	RevisionID   string
	Generation   uint64
}

type Assessment struct {
	Code     RevisionCode
	// Epoch is solely a value to compare with a future trusted fencing
	// authority. A record epoch alone is not monotonic across recovery.
	Epoch    uint64
	ControllerID string
}

// Assess compares one exact enrollment to a pinned selection and observed
// principal. It intentionally returns a diagnostic, not a write capability.
// A "matched" enrollment still needs actual Incus authorization, exclusive
// writer lease/fencing, accepted revision recheck and mutation journaling.
func (h *HostEvidence) Assess(principal AssessedPrincipal, selection SelectedRevision, action string) Assessment {
	block := func(code RevisionCode) Assessment { return Assessment{Code: code} }
	if h == nil || action != "start" && action != "stop" ||
		!idPattern.MatchString(selection.DeploymentID) ||
		!idPattern.MatchString(selection.Project) || selection.Project == "default" ||
		!idPattern.MatchString(selection.WorkloadID) ||
		!hashPattern.MatchString(selection.RevisionID) || selection.Generation == 0 {
		return block(EnrollmentUntrusted)
	}
	for _, e := range h.entries {
		if e.DeploymentID != selection.DeploymentID || e.IncusProject != selection.Project ||
			e.WorkloadID != selection.WorkloadID {
			continue
		}
		if e.Status == "revoked" {
			return block(EnrollmentRevoked)
		}
		if e.RevisionID != selection.RevisionID || e.Generation != selection.Generation {
			return block(EnrollmentMismatch)
		}
		matches := false
		for _, a := range e.Actions {
			if a == action {
				matches = true
			}
		}
		if !matches {
			return block(EnrollmentMismatch)
		}
		if e.Domain == "operator" && (principal.UID != e.UID ||
			!principal.ProjectRestricted || principal.TLSFingerprint != e.Certificate) {
			return block(EnrollmentMismatch)
		}
		if e.Domain == "host" && (principal.UID != 0 || principal.TLSFingerprint != "" ||
			principal.ProjectRestricted) {
			return block(EnrollmentMismatch)
		}
		return Assessment{Code: EnrollmentMatched, Epoch: e.Epoch, ControllerID: e.ControllerID}
	}
	return block(EnrollmentMissing)
}

// ParseUID is used only for human-readable policy diagnostics, not to select
// the effective peer UID. It refuses implicit current-user or root fallback.
func ParseUID(value string) (uint32, error) {
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, errors.New("invalid explicit UID")
	}
	return uint32(id), nil
}
