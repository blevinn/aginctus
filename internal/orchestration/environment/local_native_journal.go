package environment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/blevinn/aginctus/internal/filestate"
)

const localNativeJournalVersion = "aginctus.dev/local-native-operation/v1alpha1"
const maxLocalNativeEffects = 4096

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
}

type LocalNativeOperation struct {
	APIVersion string                    `json:"apiVersion"`
	ID         string                    `json:"id"`
	Revision   AcceptedRevision          `json:"revision"`
	Status     LocalNativeStatus         `json:"status"`
	Effects    []LocalNativeEffectRecord `json:"effects"`
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
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	return &LocalNativeJournal{pin: p, op: LocalNativeOperation{
		APIVersion: localNativeJournalVersion, ID: hex.EncodeToString(nonce[:]), Revision: p.revision, Status: LocalNativeOpen,
	}}, nil
}

func (j *LocalNativeJournal) ID() string { return j.op.ID }

// BeginEffect durably records uncertainty before submission. A crash at any
// later point (even before SDK submission) requires observation, never replay.
func (j *LocalNativeJournal) BeginEffect(ctx context.Context, resource, target, action string) error {
	effect := LocalNativeEffectRecord{resource, target, action, LocalNativeUncertain}
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
	case "create", "update", "start", "stop", "delete":
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
	revision, _, err := s.ReadLocalRevision(ctx, enrolled, op.Revision.ID)
	if err != nil || revision != op.Revision {
		return LocalNativeOperation{}, errors.New("local operation revision is not in published history")
	}
	for i, effect := range op.Effects {
		if !effect.valid() || ((i+1 < len(op.Effects) || op.Status == LocalNativeCompleted) && effect.Outcome != LocalNativeApplied) {
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
	if err := checkPrivateLocalDir(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
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
	ids := []string{}
	for _, name := range names {
		if id, ok := strings.CutSuffix(name, ".json"); ok && localEnvironmentAttemptID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, ctx.Err()
}
