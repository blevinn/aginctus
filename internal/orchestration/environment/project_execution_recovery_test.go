package environment

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/lxc/incus/v7/shared/api"
)

func TestSuccessfulRecoveryTaskRequiresPositiveExactEvidence(t *testing.T) {
	const id = "6916c8a6-9b7d-4abd-90b3-aedfec7ec7da"
	const resource = "/1.0/instances/selected-worker?project=workers"
	now := time.Now()
	effect := LocalNativeEffectRecord{Action: "create", DaemonID: id}
	fresh := func() api.Operation {
		return api.Operation{ID: id, Class: api.OperationClassTask, StatusCode: api.Success,
			Description: "Creating instance", CreatedAt: now, UpdatedAt: now.Add(time.Second),
			Resources: map[string][]string{"instances": {resource}}}
	}
	task := fresh()
	if !successfulRecoveryTask(&task, effect, resource) {
		t.Fatal("exact successful task rejected")
	}
	for _, alter := range []func(*api.Operation){
		func(op *api.Operation) { op.ID = "foreign" },
		func(op *api.Operation) { op.StatusCode = api.Running },
		func(op *api.Operation) { op.Description = "Deleting instance" },
		func(op *api.Operation) { op.Resources["instances"] = []string{"/1.0/instances/foreign"} },
		func(op *api.Operation) { op.Resources["volumes"] = []string{"/foreign"} },
		func(op *api.Operation) { op.UpdatedAt = time.Time{} },
	} {
		task := fresh()
		alter(&task)
		if successfulRecoveryTask(&task, effect, resource) {
			t.Fatal("unverified task accepted")
		}
	}
	if successfulRecoveryTask(nil, effect, resource) {
		t.Fatal("missing task accepted")
	}
}

type recoveryObserverFixture struct {
	*fixtureRestrictedIncusObserver
	task    *api.Operation
	lookups int
}

func (f *recoveryObserverFixture) GetOperation(string) (*api.Operation, string, error) {
	f.lookups++
	if f.task == nil {
		return nil, "", api.StatusErrorf(404, "task not found")
	}
	return f.task, "task-etag", nil
}

func (recoveryObserverFixture) GetInstance(string) (*api.Instance, string, error) {
	return nil, "", errors.New("unexpected instance lookup after unverified task")
}

func TestDeniedTerminalRecoveryKeepsWriterFence(t *testing.T) {
	const taskID = "6916c8a6-9b7d-4abd-90b3-aedfec7ec7da"
	for _, test := range []struct {
		name, taskResource              string
		submitted, acknowledged, active bool
	}{
		{name: "missing correlation"},
		{name: "expired task", submitted: true, acknowledged: true},
		{name: "wrong task resource", submitted: true, acknowledged: true, taskResource: "foreign"},
		{name: "unacknowledged task", submitted: true, taskResource: "beta-instance"},
		{name: "active writer", submitted: true, acknowledged: true, active: true, taskResource: "beta-instance"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			verifier, restricted := restrictedIncusVerifierFixture(t)
			c := candidateFixture()
			c.Environment.Binding = Binding{ServerID: "sha256:" + verifier.PinnedServerCertificateSHA256, Project: "team"}
			c.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "fixture", StoragePool: "default", ManagementNetwork: "test-mgmt"}
			store, err := InitializeLocalProjectStateStore(filepath.Join(t.TempDir(), "project"), c.Environment.Binding)
			if err != nil {
				t.Fatal(err)
			}
			rev, err := store.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c)
			if err != nil {
				t.Fatal(err)
			}
			writer := projectExecutionGrant{PeerSHA256: strings.Repeat("a", 64), ServerID: rev.ServerID, Project: rev.Project,
				EnvironmentID: rev.EnvironmentID, RevisionID: rev.ID, WorkloadID: "beta", Operation: "reconcile"}
			e, err := store.beginProjectExecution(ctx, c.Environment, rev, writer)
			if err != nil {
				t.Fatal(err)
			}
			defer e.close()
			if err := e.beginEffect(ctx, "start", writer); err != nil {
				t.Fatal(err)
			}
			if test.submitted {
				if err := e.journal.recordDaemonOperation(ctx, taskID); err != nil {
					t.Fatal(err)
				}
			}
			if test.acknowledged {
				if err := e.journal.MarkApplied(); err != nil {
					t.Fatal(err)
				}
			}
			id := e.journal.ID()
			if !test.active {
				if err := e.close(); err != nil {
					t.Fatal(err)
				}
			}
			obs := recoveryObserverFixture{fixtureRestrictedIncusObserver: restricted}
			if test.taskResource != "" {
				now := time.Now()
				obs.task = &api.Operation{ID: taskID, Class: api.OperationClassTask, StatusCode: api.Success, Description: "Starting instance",
					CreatedAt: now, UpdatedAt: now, Resources: map[string][]string{"instances": {"/1.0/instances/" + test.taskResource + "?project=team"}}}
			}
			recoverGrant := writer
			recoverGrant.PeerSHA256, recoverGrant.Operation = strings.Repeat("b", 64), "recover"
			err = store.recoverProjectExecution(ctx, c.Environment, rev, recoverGrant, id, &obs, func() bool { return true })
			if err == nil || test.active != errors.Is(err, filestate.ErrRegistryBusy) {
				t.Fatalf("unexpected recovery result: %v", err)
			}
			if (obs.lookups == 1) != (test.acknowledged && !test.active) {
				t.Fatalf("unexpected daemon task lookup count: %d", obs.lookups)
			}
			if test.active {
				if err := e.close(); err != nil {
					t.Fatal(err)
				}
			}
			if next, err := store.beginProjectExecution(ctx, c.Environment, rev, writer); !errors.Is(err, ErrLocalNativeUnresolved) {
				if next != nil {
					_ = next.close()
				}
				t.Fatalf("denied recovery did not retain writer fence: %v", err)
			}
		})
	}
}
