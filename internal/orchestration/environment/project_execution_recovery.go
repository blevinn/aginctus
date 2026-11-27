package environment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/lxc/incus/v7/shared/api"
)

// projectRecoveryObserver is an independently opened, read-only service
// capability. Recovery never uses the executor's SDK or an admin socket.
type projectRecoveryObserver interface {
	RestrictedIncusProjectObserver
	GetOperation(string) (*api.Operation, string, error)
	GetInstance(string) (*api.Instance, string, error)
}

var errRecoveryUnverified = errors.New("native workload recovery evidence unavailable")

// recoverProjectExecution admits only positively observed successful reconcile
// or stop tasks. Missing correlation, expired operation history, failed tasks
// and removal remain unrecoverable. The active-session
// check belongs to the service handler; both persistent locks are held here.
func (s *LocalProjectStateStore) recoverProjectExecution(ctx context.Context, desc Descriptor, revision AcceptedRevision, grant projectExecutionGrant, id string, observer projectRecoveryObserver, authorize func() bool) error {
	if ctx == nil || observer == nil || authorize == nil || !authorize() ||
		grant.Operation != "recover" || !grant.valid(s.binding) ||
		grant.RevisionID != revision.ID || grant.EnvironmentID != desc.ID || desc.Binding != s.binding {
		return errRecoveryUnverified
	}
	lock, err := filestate.LockRegistry(filepath.Join(s.root, "native-execution"))
	if err != nil {
		return err
	}
	defer lock.Close()
	return s.ResolveLocalNativeOperation(ctx, desc, revision, id, func(ctx context.Context, req LocalNativeRecoveryRequest) (Binding, error) {
		op := req.Operation
		if len(op.Effects) == 0 || op.Execution == nil || (op.Execution.Operation != "reconcile" && op.Execution.Operation != "stop") ||
			op.Execution.WorkloadID != grant.WorkloadID || op.Revision != revision || !authorize() {
			return Binding{}, errRecoveryUnverified
		}
		_, candidate, err := s.ReadLocalRevision(ctx, desc, revision.ID)
		if err != nil || !slices.Contains(candidate.SelectedWorkloads, grant.WorkloadID) {
			return Binding{}, errRecoveryUnverified
		}
		verifier := RestrictedIncusProjectVerifier{Observer: observer, PinnedServerCertificateSHA256: strings.TrimPrefix(s.binding.ServerID, "sha256:")}
		if binding, err := verifier.VerifyProject(ctx, s.binding.Project); err != nil || binding != s.binding {
			return Binding{}, errRecoveryUnverified
		}
		for _, w := range candidate.Workloads {
			if w.ID != grant.WorkloadID || w.Native == nil {
				continue
			}
			resource := api.NewURL().Path("1.0", "instances", w.InstanceName).Project(s.binding.Project).String()
			stopping := op.Execution.Operation == "stop"
			if stopping && len(op.Effects) != 1 {
				return Binding{}, errRecoveryUnverified
			}
			started := false
			var firstTask time.Time
			var lastTask time.Time
			seen := map[string]bool{}
			for i, effect := range op.Effects {
				if ctx.Err() != nil || effect.Outcome != LocalNativeApplied || !daemonOperationID.MatchString(effect.DaemonID) || seen[effect.DaemonID] {
					return Binding{}, errRecoveryUnverified
				}
				seen[effect.DaemonID] = true
				if (stopping && effect.Action != "stop") || (!stopping && (effect.Action == "create" && i != 0 || effect.Action == "start" && started ||
					(effect.Action != "create" && effect.Action != "start"))) {
					return Binding{}, errRecoveryUnverified
				}
				started = started || effect.Action == "start"
				task, _, err := observer.GetOperation(effect.DaemonID)
				if err != nil || !successfulRecoveryTask(task, effect, resource) {
					return Binding{}, errRecoveryUnverified
				}
				if i == 0 {
					firstTask = task.CreatedAt
				}
				if !lastTask.IsZero() && task.CreatedAt.Before(lastTask) {
					return Binding{}, errRecoveryUnverified
				}
				lastTask = task.UpdatedAt
			}
			// Successful task correlation does not imply ongoing readiness.
			// Also observe the exact owned instance, image and stable incarnation.
			instance, etag, err := observer.GetInstance(w.InstanceName)
			wantStatus := "Stopped"
			if started {
				wantStatus = "Running"
			}
			if err != nil || instance == nil || etag == "" ||
				instance.Name != w.InstanceName || instance.Project != s.binding.Project ||
				instance.Type != w.InstanceType || instance.Status != wantStatus || instance.CreatedAt.IsZero() ||
				instance.CreatedAt.After(lastTask) || (stopping && instance.CreatedAt.After(firstTask)) || (op.Effects[0].Action == "create" && instance.CreatedAt.Before(firstTask)) ||
				instance.Config["user.aginctus.managed"] != "true" || instance.Config["user.aginctus.resource"] != "workload" ||
				instance.Config["user.aginctus.workload"] != grant.WorkloadID ||
				instance.Config["user.aginctus.environment-id"] != desc.ID ||
				instance.Config["volatile.base_image"] != w.ImageFingerprint ||
				!slices.Equal(instance.Profiles, w.Profiles) {
				return Binding{}, errRecoveryUnverified
			}
			again, againETag, err := observer.GetInstance(w.InstanceName)
			if err != nil || again == nil || againETag != etag || !again.CreatedAt.Equal(instance.CreatedAt) || again.Status != wantStatus {
				return Binding{}, errRecoveryUnverified
			}
			held, err := lock.Stat()
			named, nameErr := os.Lstat(filepath.Join(s.root, "native-execution.flock"))
			binding, verifyErr := verifier.VerifyProject(ctx, s.binding.Project)
			if err != nil || nameErr != nil || !os.SameFile(held, named) ||
				verifyErr != nil || binding != s.binding || !authorize() {
				return Binding{}, errRecoveryUnverified
			}
			return binding, nil
		}
		return Binding{}, errRecoveryUnverified
	})
}

// A correlation hint must resolve to a successful exact instance task. The
// operation description is pinned to the supported Incus task vocabulary;
// unknown versions or unavailable history leave the journal blocked.
func successfulRecoveryTask(task *api.Operation, effect LocalNativeEffectRecord, resource string) bool {
	if task == nil || task.ID != effect.DaemonID || task.Class != api.OperationClassTask ||
		task.StatusCode != api.Success || task.CreatedAt.IsZero() || task.UpdatedAt.Before(task.CreatedAt) ||
		len(task.Resources) != 1 || !slices.Equal(task.Resources["instances"], []string{resource}) {
		return false
	}
	switch effect.Action {
	case "create":
		return task.Description == "Creating instance"
	case "start":
		return task.Description == "Starting instance"
	case "stop":
		return task.Description == "Stopping instance"
	default:
		return false
	}
}
