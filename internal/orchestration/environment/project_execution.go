package environment

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/blevinn/aginctus/internal/filestate"
)

// projectExecution holds the accepted pin across observation, submissions and
// terminal reporting. Only the authenticated service composer may construct
// it. The journal survives service loss; the advisory lock does not.
type projectExecution struct {
	grant       projectExecutionGrant
	pin         *LocalEnvironmentPin
	journal     *LocalNativeJournal
	projectLock *os.File
}

// beginProjectExecution conservatively serializes this workload path per
// project: different environments can select the same Incus instance name.
// Never clear an old submission record to obtain this lock. Promotion still
// uses the narrower environment lock and is not an Incus writer.
func (s *LocalProjectStateStore) beginProjectExecution(ctx context.Context, enrolled Descriptor, revision AcceptedRevision, grant projectExecutionGrant) (*projectExecution, error) {
	if ctx == nil || s == nil || !grant.valid(s.binding) || !grant.writer() ||
		grant.EnvironmentID != enrolled.ID || grant.RevisionID != revision.ID || enrolled.Binding != s.binding {
		return nil, errProjectExecutionDenied
	}
	if _, err := s.namespace(enrolled); err != nil {
		return nil, err
	}
	lock, err := filestate.LockRegistry(filepath.Join(s.root, "native-execution"))
	if err != nil {
		return nil, err
	}
	var pin *LocalEnvironmentPin
	owned := false
	defer func() {
		if !owned {
			_ = pin.Close()
			_ = lock.Close()
		}
	}()
	if err := s.requireResolvedProjectExecutions(ctx); err != nil {
		return nil, err
	}
	pin, err = s.PinLocalAccepted(ctx, enrolled, revision, grant.WorkloadID)
	if err != nil {
		return nil, err
	}
	journal, err := pin.NewLocalNativeJournal()
	if err != nil {
		return nil, err
	}
	journal.op.Execution = &grant
	owned = true
	return &projectExecution{grant: grant, pin: pin, journal: journal, projectLock: lock}, nil
}

func (s *LocalProjectStateStore) requireResolvedProjectExecutions(ctx context.Context) error {
	root := filepath.Join(s.root, "environments")
	dir, err := os.Open(root)
	if err != nil {
		return ErrLocalNativeUnresolved
	}
	defer dir.Close()
	entries, err := dir.Readdirnames(1025)
	if (err != nil && !errors.Is(err, io.EOF)) || len(entries) > 1024 {
		return ErrLocalNativeUnresolved
	}
	for _, id := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(root, id)
		if !idPattern.MatchString(id) || checkPrivateLocalDir(path) != nil {
			return ErrLocalNativeUnresolved
		}
		// The namespace UUID and immutable binding, not its display name,
		// identify the accepted history used by journal validation.
		if err := s.requireResolvedLocalNativeOperations(ctx, Descriptor{ID: id, Name: "project-execution", Binding: s.binding}, path); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (e *projectExecution) beginEffect(ctx context.Context, action string, currentGrant projectExecutionGrant) error {
	if e.projectLock == nil || currentGrant != e.grant || !e.grant.permits(LocalNativeEffectRecord{
		Resource: e.grant.WorkloadID, Target: e.grant.WorkloadID, Action: action,
	}) {
		return errProjectExecutionDenied
	}
	held, err := e.projectLock.Stat()
	if err != nil {
		return errProjectExecutionDenied
	}
	named, err := os.Lstat(filepath.Join(e.pin.store.root, "native-execution.flock"))
	if err != nil || !os.SameFile(held, named) {
		return errProjectExecutionDenied
	}
	return e.journal.BeginEffect(ctx, e.grant.WorkloadID, e.grant.WorkloadID, action)
}

func (e *projectExecution) close() error {
	return errors.Join(e.pin.Close(), e.projectLock.Close())
}
