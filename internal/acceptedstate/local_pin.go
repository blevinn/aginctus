package acceptedstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/blevinn/aginctus/internal/filestate"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

var ErrLocalPinScope = errors.New("selected workload scope is absent from accepted revision")

// LocalRevisionPin serializes one same-UID operation against CommitLocal by
// holding the SAME private active-pointer lock through its lifetime. It is
// NOT a cross-principal owner lease, an Incus writer lock, a credential, a
// durable operation journal or authority to execute resource mutations.
//
// In particular, no untrusted Home Manager user may treat this object as
// authorization to invoke the privileged native local Incus socket.
// A pin must not be copied. Check and Close are safe to call concurrently,
// but the caller must keep Close ordered after its entire downstream operation:
// a successful Check does not prevent another goroutine from closing the pin.
// Cancellation makes Check fail; it does not release the lock. Always Close.
type LocalRevisionPin struct {
	mu         sync.Mutex
	store      *LocalStore
	lock       *os.File
	revision   Revision
	deployment string
	project    string
	workload   string
	closed     bool
}

// PinLocalRevision requires an existing active revision, a complete expected
// revision identity and an explicitly selected workload. The private
// active-pointer lock is the SAME lock used by CommitLocal, so a cooperating
// same-UID promotion cannot change the revision while this pin is held.
func (s *LocalStore) PinLocalRevision(
	ctx context.Context, expected Revision, deployment, project, workload string,
) (*LocalRevisionPin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || expected.validateIdentity() != nil || !localID.MatchString(deployment) ||
		!localID.MatchString(workload) {
		return nil, ErrLocalPinScope
	}
	if _, err := incusscope.NewProjectScope(project); err != nil {
		return nil, ErrLocalPinScope
	}
	if err := privateStoreDirectory(s.root); err != nil {
		return nil, err
	}
	if err := privateStoreDirectory(filepath.Join(s.root, "revisions")); err != nil {
		return nil, err
	}
	lock, err := filestate.LockRegistry(filepath.Join(s.root, "active.json"))
	if err != nil {
		return nil, err
	}
	p := &LocalRevisionPin{
		store: s, lock: lock, revision: expected,
		deployment: deployment, project: project, workload: workload,
	}
	if err := p.check(ctx); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return p, nil
}

// Check refuses cancellation, a released pin, corruption, changed revision,
// project drift, or a workload absent from the pinned accepted selection.
// Call it immediately before any downstream observe/execute boundary;
// callers still need independent authenticated principal/resource fencing.
func (p *LocalRevisionPin) Check(ctx context.Context) error {
	if p == nil {
		return ErrLocalPinScope
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrLocalPinScope
	}
	return p.check(ctx)
}

func (p *LocalRevisionPin) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, spec, err := p.store.ReadActive()
	if err != nil {
		return err
	}
	if current == nil || spec == nil || *current != p.revision {
		return ErrRevisionConflict
	}
	selection := spec.Selection
	if selection.DeploymentID != p.deployment || selection.IncusProject != p.project ||
		!slices.Contains(selection.SelectedWorkloads, p.workload) {
		return ErrLocalPinScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Close releases the local advisory lock; no caller may continue relying on
// its serialization once closed. It never claims infrastructure rollback.
func (p *LocalRevisionPin) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.lock.Close()
}
