package environment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
)

const localNativeJournalVersion = "aginctus.dev/local-native-operation/v1alpha1"
const maxLocalNativeEffects = 4096

const localNativeRecoveryVersion = "aginctus.dev/local-native-recovery/v1alpha1"

// ErrLocalNativeUnresolved means the journal cannot establish a safe handoff
// to a competing writer. Acquiring the advisory lock alone cannot clear it.
var ErrLocalNativeUnresolved = errors.New("unresolved local native operation; inspect journal before writing")

var journalIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type LocalNativeOutcome string

const (
	LocalNativeUncertain LocalNativeOutcome = "uncertain"
	LocalNativeApplied   LocalNativeOutcome = "applied"
)

// LocalNativeEffectRecord stores only logical IDs, the attempted action and
// submission outcome. Applied does not mean ready or converged.
type LocalNativeEffectRecord struct {
	Resource string             `json:"resource"`
	Target   string             `json:"target"`
	Action   string             `json:"action"`
	Outcome  LocalNativeOutcome `json:"outcome"`
	DaemonID string             `json:"daemonOperationId,omitempty"`
}

type LocalNativeOperation struct {
	APIVersion string                    `json:"apiVersion"`
	ID         string                    `json:"id"`
	Revision   AcceptedRevision          `json:"revision"`
	Status     LocalNativeStatus         `json:"status"`
	Effects    []LocalNativeEffectRecord `json:"effects"`
	Execution  *projectExecutionGrant    `json:"execution,omitempty"`
}

// LocalNativeStatus describes the invocation, not ongoing resource health.
type LocalNativeStatus string

const (
	LocalNativeOpen      LocalNativeStatus = "open"
	LocalNativeFailed    LocalNativeStatus = "failed"
	LocalNativeCompleted LocalNativeStatus = "completed"
)

// LocalNativeJournal is scoped to an existing fixture pin. Call methods
// synchronously within one invocation. It contains no execution authority.
type LocalNativeJournal struct {
	pin *LocalEnvironmentPin
	op  LocalNativeOperation
}

func (p *LocalEnvironmentPin) NewLocalNativeJournal() (*LocalNativeJournal, error) {
	if p == nil {
		return nil, ErrAcceptedConflict
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.nativeJournalAllocated {
		return nil, ErrAcceptedConflict
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	p.nativeJournalAllocated = true
	return &LocalNativeJournal{pin: p, op: LocalNativeOperation{
		APIVersion: localNativeJournalVersion, ID: hex.EncodeToString(nonce[:]), Revision: p.revision, Status: LocalNativeOpen,
	}}, nil
}

func (j *LocalNativeJournal) ID() string { return j.op.ID }

// BeginEffect durably records uncertainty before submission. A crash at any
// later point (even before SDK submission) requires observation, never replay.
func (j *LocalNativeJournal) BeginEffect(ctx context.Context, resource, target, action string) error {
	effect := LocalNativeEffectRecord{Resource: resource, Target: target, Action: action, Outcome: LocalNativeUncertain}
	if j.op.Status != LocalNativeOpen || !effect.valid() || len(j.op.Effects) >= maxLocalNativeEffects ||
		len(j.op.Effects) > 0 && j.op.Effects[len(j.op.Effects)-1].Outcome != LocalNativeApplied {
		return errors.New("invalid or interrupted local native operation")
	}
	next := j.op
	next.Effects = append(slices.Clone(j.op.Effects), effect)
	if err := j.save(ctx, next); err != nil {
		return err
	}
	j.op = next
	return nil
}

var daemonOperationID = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

// recordDaemonOperation records a client-reported correlation hint. It does
// not change the outcome or authorize recovery; verify it independently.
func (j *LocalNativeJournal) recordDaemonOperation(ctx context.Context, id string) error {
	n := len(j.op.Effects)
	if j.op.Execution == nil || j.op.Status != LocalNativeOpen || n == 0 || !daemonOperationID.MatchString(id) ||
		j.op.Effects[n-1].Outcome != LocalNativeUncertain || j.op.Effects[n-1].DaemonID != "" ||
		slices.ContainsFunc(j.op.Effects, func(e LocalNativeEffectRecord) bool { return e.DaemonID == id }) {
		return ErrLocalNativeUnresolved
	}
	next := j.op
	next.Effects = slices.Clone(j.op.Effects)
	next.Effects[n-1].DaemonID = id
	if err := j.save(ctx, next); err != nil {
		return err
	}
	j.op = next
	return nil
}

// MarkApplied finalizes only the most recently submitted effect. SDK errors
// and cancellations leave its durable record uncertain.
func (j *LocalNativeJournal) MarkApplied() error {
	n := len(j.op.Effects)
	if j.op.Status != LocalNativeOpen || n == 0 || j.op.Effects[n-1].Outcome != LocalNativeUncertain {
		return errors.New("no uncertain local native effect to finalize")
	}
	next := j.op
	next.Effects = slices.Clone(j.op.Effects)
	next.Effects[n-1].Outcome = LocalNativeApplied
	if err := j.save(context.Background(), next); err != nil {
		return err
	}
	j.op = next
	return nil
}

// Finish durably records an observed engine return. A process that disappears
// leaves an open journal; even if all its effects report applied, readiness is
// not inferred. No journal is created when nothing reached the effect boundary.
func (j *LocalNativeJournal) Finish(succeeded bool) error {
	if len(j.op.Effects) == 0 {
		return nil
	}
	if j.op.Status != LocalNativeOpen {
		return errors.New("local native operation already finished")
	}
	next := j.op
	next.Status = LocalNativeFailed
	if succeeded {
		if next.Effects[len(next.Effects)-1].Outcome != LocalNativeApplied {
			return errors.New("cannot complete a local native operation with uncertain effects")
		}
		next.Status = LocalNativeCompleted
	}
	if err := j.save(context.Background(), next); err != nil {
		return err
	}
	j.op = next
	return nil
}

func (j *LocalNativeJournal) save(ctx context.Context, next LocalNativeOperation) error {
	p := j.pin
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrAcceptedConflict
	}
	if err := p.check(ctx); err != nil {
		return err
	}
	path, err := p.store.namespace(p.enrolled)
	if err != nil {
		return err
	}
	dir := filepath.Join(path, "native-operations")
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := checkPrivateLocalDir(dir); err != nil {
		return err
	}
	if err := syncLocalProjectDir(path); err != nil {
		return err
	}
	data, err := json.Marshal(next)
	if err != nil || int64(len(data)) > localProjectRecordLimit {
		return errors.New("local native journal exceeds record bounds")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name := next.ID + ".json"
	if len(j.op.Effects) == 0 {
		return createImmutableLocalRecord(dir, name, data)
	}
	if err := filestate.WritePrivateFile(filepath.Join(dir, name), data); err != nil {
		return err
	}
	return syncLocalProjectDir(dir)
}

func (r LocalNativeEffectRecord) valid() bool {
	if !journalIdentifier.MatchString(r.Resource) || !journalIdentifier.MatchString(r.Target) ||
		(r.Outcome != LocalNativeApplied && r.Outcome != LocalNativeUncertain) {
		return false
	}
	switch r.Action {
	case "create", "update", "start", "stop", "delete", "deliver", "create-state":
		return true
	}
	return false
}

// ReadLocalNativeOperation recovers submission evidence against verified
// published history. It never submits or claims convergence.
func (s *LocalProjectStateStore) ReadLocalNativeOperation(ctx context.Context, enrolled Descriptor, id string) (LocalNativeOperation, error) {
	if ctx == nil || !localEnvironmentAttemptID.MatchString(id) {
		return LocalNativeOperation{}, errors.New("explicit local operation ID required")
	}
	if err := ctx.Err(); err != nil {
		return LocalNativeOperation{}, err
	}
	path, err := s.namespace(enrolled)
	if err != nil {
		return LocalNativeOperation{}, err
	}
	if err := checkPrivateLocalDir(path); err != nil {
		return LocalNativeOperation{}, err
	}
	dir := filepath.Join(path, "native-operations")
	if err := checkPrivateLocalDir(dir); err != nil {
		return LocalNativeOperation{}, err
	}
	data, err := filestate.ReadPrivateFile(filepath.Join(dir, id+".json"), localProjectRecordLimit)
	if err != nil {
		return LocalNativeOperation{}, err
	}
	var op LocalNativeOperation
	if json.Unmarshal(data, &op) != nil || op.APIVersion != localNativeJournalVersion || op.ID != id ||
		len(op.Effects) == 0 || len(op.Effects) > maxLocalNativeEffects ||
		(op.Status != LocalNativeOpen && op.Status != LocalNativeFailed && op.Status != LocalNativeCompleted) {
		return LocalNativeOperation{}, errors.New("invalid local native operation journal")
	}
	// Local journal records are always written in canonical form. This also
	// refuses trailing data, unknown, duplicate, or case-aliased fields.
	if canonical, err := json.Marshal(op); err != nil || string(canonical) != string(data) {
		return LocalNativeOperation{}, errors.New("noncanonical local native operation journal")
	}
	revision, candidate, err := s.ReadLocalRevision(ctx, enrolled, op.Revision.ID)
	if err != nil || revision != op.Revision {
		return LocalNativeOperation{}, errors.New("local operation revision is not in published history")
	}
	if op.Execution != nil && (!op.Execution.valid(enrolled.Binding) ||
		op.Execution.RevisionID != op.Revision.ID || op.Execution.EnvironmentID != enrolled.ID ||
		!op.Execution.writer() || !op.Execution.domainSelected(candidate)) {
		return LocalNativeOperation{}, errors.New("invalid journal execution scope")
	}
	for i, effect := range op.Effects {
		if !effect.valid() || (op.Execution != nil && !op.Execution.permits(effect)) ||
			(effect.DaemonID != "" && (op.Execution == nil || !daemonOperationID.MatchString(effect.DaemonID))) ||
			((i+1 < len(op.Effects) || op.Status == LocalNativeCompleted) && effect.Outcome != LocalNativeApplied) {
			return LocalNativeOperation{}, errors.New("invalid or out-of-order local native effect evidence")
		}
	}
	return op, ctx.Err()
}

// ListLocalNativeOperationIDs is bounded, fixture-only recovery discovery.
// Use ReadLocalNativeOperation to validate any returned ID against history.
func (s *LocalProjectStateStore) ListLocalNativeOperationIDs(ctx context.Context, enrolled Descriptor) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("local native operation discovery requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.namespace(enrolled)
	if err != nil {
		return nil, err
	}
	if err := checkPrivateLocalDir(path); err != nil {
		return nil, err
	}
	dir := filepath.Join(path, "native-operations")
	names, err := localNativeJournalNames(dir)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, name := range names {
		if id, ok := strings.CutSuffix(name, ".json"); ok && localEnvironmentAttemptID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, ctx.Err()
}

func localNativeJournalNames(dir string) ([]string, error) {
	if err := checkPrivateLocalDir(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(4097)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > 4096 {
		return nil, errors.New("local native operation inventory exceeds inspection bound")
	}
	return names, nil
}

func (op LocalNativeOperation) unresolved() bool {
	return op.Status == LocalNativeOpen || op.Effects[len(op.Effects)-1].Outcome == LocalNativeUncertain
}

type localNativeRecovery struct {
	APIVersion    string `json:"apiVersion"`
	OperationID   string `json:"operationId"`
	JournalDigest string `json:"journalDigest"`
}

func localNativeRecoveryBytes(op LocalNativeOperation) ([]byte, error) {
	journal, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(journal)
	return json.Marshal(localNativeRecovery{localNativeRecoveryVersion, op.ID, hex.EncodeToString(sum[:])})
}

// requireResolvedLocalNativeOperations runs only while holding the existing
// environment writer lock. Never use the inspection list alone as admission:
// invalid or orphaned records must not silently disappear from the decision.
func (s *LocalProjectStateStore) requireResolvedLocalNativeOperations(ctx context.Context, enrolled Descriptor, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dir := filepath.Join(path, "native-operations")
	names, err := localNativeJournalNames(dir)
	if err != nil {
		return ErrLocalNativeUnresolved
	}
	entries := make(map[string]bool, len(names))
	for _, name := range names {
		entries[name] = true
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if id, ok := strings.CutSuffix(name, ".recovered"); ok && localEnvironmentAttemptID.MatchString(id) {
			if !entries[id+".json"] {
				return ErrLocalNativeUnresolved
			}
			continue
		}
		if strings.HasPrefix(name, ".pending-") || strings.HasPrefix(name, ".") && strings.Contains(name, ".json-") {
			continue // Unpublished scratch is never a journal or recovery receipt.
		}
		id, ok := strings.CutSuffix(name, ".json")
		if !ok || !localEnvironmentAttemptID.MatchString(id) {
			return ErrLocalNativeUnresolved
		}
		op, err := s.ReadLocalNativeOperation(ctx, enrolled, id)
		if err != nil {
			return fmt.Errorf("%w: invalid journal %s", ErrLocalNativeUnresolved, id)
		}
		if !op.unresolved() && !entries[id+".recovered"] {
			continue
		}
		want, err := localNativeRecoveryBytes(op)
		got, readErr := filestate.ReadPrivateFile(filepath.Join(dir, id+".recovered"), 1024)
		if !op.unresolved() || err != nil || readErr != nil || string(got) != string(want) {
			return fmt.Errorf("%w: operation %s", ErrLocalNativeUnresolved, id)
		}
	}
	// A terminal journal or recovery receipt may have become visible before
	// its directory sync returned. Reconfirm durability before admitting writes.
	if names != nil && syncLocalProjectDir(dir) != nil {
		return ErrLocalNativeUnresolved
	}
	return ctx.Err()
}

type LocalNativeRecoveryRequest struct {
	Environment     Descriptor
	CurrentRevision AcceptedRevision
	Operation       LocalNativeOperation
}

// ResolveLocalNativeOperation is fixture-only and never submits or replays an
// effect. A separately trusted recovery capability must freshly verify actor,
// scope, exact journal, and that old work is terminal or proven not submitted.
// A resource snapshot, elapsed time, or process exit alone is insufficient.
// The callback runs under the writer lock and must be read-only and bounded.
func (s *LocalProjectStateStore) ResolveLocalNativeOperation(ctx context.Context, enrolled Descriptor, expected AcceptedRevision, id string,
	verify func(context.Context, LocalNativeRecoveryRequest) (Binding, error),
) error {
	if ctx == nil || verify == nil || !localEnvironmentAttemptID.MatchString(id) || expected.ValidateIdentity() != nil {
		return errors.New("explicit local recovery scope and verification required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.namespace(enrolled)
	if err != nil {
		return err
	}
	if err := checkPrivateLocalDir(path); err != nil {
		return err
	}
	lock, err := filestate.LockRegistry(filepath.Join(path, "active.json"))
	if err != nil {
		return err
	}
	defer lock.Close()
	p := LocalEnvironmentPin{store: s, lock: lock, enrolled: enrolled, revision: expected}
	if err := p.check(ctx); err != nil {
		return err
	}
	op, err := s.ReadLocalNativeOperation(ctx, enrolled, id)
	if err != nil || !op.unresolved() {
		return ErrLocalNativeUnresolved
	}
	data, err := localNativeRecoveryBytes(op)
	if err != nil {
		return err
	}
	request := LocalNativeRecoveryRequest{enrolled, expected, op}
	request.Operation.Effects = slices.Clone(op.Effects)
	if op.Execution != nil {
		grant := *op.Execution
		request.Operation.Execution = &grant
	}
	binding, err := verify(ctx, request)
	if err != nil || enrolled.VerifyBinding(binding) != nil {
		return errors.New("local native recovery verification denied")
	}
	if err := p.check(ctx); err != nil {
		return err
	}
	latest, err := s.ReadLocalNativeOperation(ctx, enrolled, id)
	latestData, digestErr := localNativeRecoveryBytes(latest)
	if err != nil || digestErr != nil || string(latestData) != string(data) {
		return ErrLocalNativeUnresolved
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := createImmutableLocalRecord(filepath.Join(path, "native-operations"), id+".recovered", data); err != nil {
		return errors.New("local native recovery receipt unavailable; inspect before retry")
	}
	return nil
}
