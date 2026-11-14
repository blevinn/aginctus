package acceptedstate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/blevinn/aginctus/internal/filestate"
)

const LocalAttemptAPIVersion = "aginctus.dev/local-operation/v1alpha1"

var localAttemptID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// LocalUnverifiedAttempt is immutable, bounded, nonsecret accounting for a
// selected operation intention. "unverified" is deliberately the ONLY
// outcome: an Incus call may never have occurred, or may have partially
// succeeded before a crash. It is never proof of success or of rollback.
//
// This same-UID local record grants NO principal, controller, Incus or
// cross-user/root write authority. It is not a production audit attestation.
type LocalUnverifiedAttempt struct {
	APIVersion  string `json:"apiVersion"`
	ID          string `json:"id"`
	RevisionID  string `json:"revisionID"`
	Generation  uint64 `json:"generation"`
	Deployment  string `json:"deploymentID"`
	Project     string `json:"incusProject"`
	Workload    string `json:"workloadID"`
	Operation   string `json:"operation"`
	Outcome     string `json:"outcome"`
}

func (a LocalUnverifiedAttempt) validate() error {
	if a.APIVersion != LocalAttemptAPIVersion || !localAttemptID.MatchString(a.ID) ||
		len(a.RevisionID) != 64 || !fingerprintHex(a.RevisionID) ||
		a.Generation == 0 || !localID.MatchString(a.Deployment) ||
		!localID.MatchString(a.Project) || a.Project == "default" ||
		!localID.MatchString(a.Workload) ||
		a.Operation != "selected-workload" || a.Outcome != "unverified" {
		return errors.New("invalid local operation record")
	}
	return nil
}

// RecordLocalUnverifiedAttempt durably records a non-authorizing intention
// under the SAME active-revision lock used by CommitLocal, before any future
// execution. Holding this local pin does not exclude independent Incus
// administrators or different UIDs. There is deliberately no "succeeded"
// update API and no Incus write path here.
//
// A returned error may mean the immutable record reached disk but the final
// directory fsync failed. Do not assume no operation occurred; inspect the
// private journal before any future retry.
func (p *LocalRevisionPin) RecordLocalUnverifiedAttempt(ctx context.Context) (LocalUnverifiedAttempt, error) {
	if p == nil {
		return LocalUnverifiedAttempt{}, ErrLocalPinScope
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return LocalUnverifiedAttempt{}, ErrLocalPinScope
	}
	if err := p.check(ctx); err != nil {
		return LocalUnverifiedAttempt{}, err
	}
	journalDir := filepath.Join(p.store.root, "operations")
	if err := os.Mkdir(journalDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return LocalUnverifiedAttempt{}, fmt.Errorf("initialize private local operation journal: %w", err)
	}
	if err := privateStoreDirectory(journalDir); err != nil {
		return LocalUnverifiedAttempt{}, err
	}
	// Directory creation must be durable before a record can be reported as
	// durably stored. Existing same-UID stores are upgraded on explicit use.
	if err := syncDirectory(p.store.root); err != nil {
		return LocalUnverifiedAttempt{}, fmt.Errorf("sync operation journal directory entry: %w", err)
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return LocalUnverifiedAttempt{}, errors.New("cannot allocate unique operation identity")
	}
	rec := LocalUnverifiedAttempt{
		APIVersion: LocalAttemptAPIVersion, ID: hex.EncodeToString(entropy[:]),
		RevisionID: p.revision.ID, Generation: p.revision.Generation,
		Deployment: p.deployment, Project: p.project, Workload: p.workload,
		Operation: "selected-workload", Outcome: "unverified",
	}
	if err := rec.validate(); err != nil {
		return LocalUnverifiedAttempt{}, err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return LocalUnverifiedAttempt{}, errors.New("cannot encode local operation record")
	}
	if err := ctx.Err(); err != nil {
		return LocalUnverifiedAttempt{}, err
	}
	// Immutable O_EXCL-like linking avoids overwriting an existing attempt;
	// file and containing directory are synced before success is returned.
	if err := writeImmutableRecord(journalDir, rec.ID+".json", raw); err != nil {
		return rec, fmt.Errorf("local operation record persistence uncertain; inspect journal: %w", err)
	}
	return rec, nil
}

// ReadLocalUnverifiedAttempt reads one explicitly named immutable record
// after restart. It never interprets absent/invalid state as convergence.
// These records are historical even when the active revision has moved.
func (s *LocalStore) ReadLocalUnverifiedAttempt(id string) (LocalUnverifiedAttempt, error) {
	if s == nil || !localAttemptID.MatchString(id) {
		return LocalUnverifiedAttempt{}, errors.New("invalid local operation identity")
	}
	if err := privateStoreDirectory(s.root); err != nil {
		return LocalUnverifiedAttempt{}, err
	}
	dir := filepath.Join(s.root, "operations")
	if err := privateStoreDirectory(dir); err != nil {
		return LocalUnverifiedAttempt{}, err
	}
	raw, err := filestate.ReadPrivateFile(filepath.Join(dir, id+".json"), maxRecordBytes)
	if err != nil {
		return LocalUnverifiedAttempt{}, fmt.Errorf("read private operation record: %w", err)
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return LocalUnverifiedAttempt{}, errors.New("invalid operation record JSON")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return LocalUnverifiedAttempt{}, errors.New("invalid operation record JSON")
	}
	if err := strictKeySet(keys, "apiVersion", "id", "revisionID", "generation", "deploymentID", "incusProject", "workloadID", "operation", "outcome"); err != nil {
		return LocalUnverifiedAttempt{}, errors.New("invalid operation record fields")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var rec LocalUnverifiedAttempt
	if err := decoder.Decode(&rec); err != nil {
		return LocalUnverifiedAttempt{}, errors.New("invalid operation record JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return LocalUnverifiedAttempt{}, errors.New("trailing operation record contents")
	}
	if rec.ID != id || rec.validate() != nil {
		return LocalUnverifiedAttempt{}, errors.New("operation record identity mismatch")
	}
	return rec, nil
}
