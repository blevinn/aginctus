package native

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeInstanceOperation struct {
	err   error
	waits int
}

func (o *fakeInstanceOperation) WaitContext(context.Context) error {
	o.waits++
	return o.err
}

type fakeInstanceClient struct {
	scope     incusscope.ProjectScope
	instance  *api.Instance
	etag      string
	mode      string
	reads     int
	transfers int
	requests  []api.InstanceStatePut
	operation *fakeInstanceOperation
}

func (f *fakeInstanceClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	info := &incus.ConnectionInfo{Protocol: "incus", Project: f.scope.Name, SocketPath: "/test/incus.sock"}
	switch f.mode {
	case "remote":
		info.SocketPath = ""
	case "wrong project":
		info.Project = "default"
	}
	return info, nil
}

func (f *fakeInstanceClient) GetInstance(name string) (*api.Instance, string, error) {
	f.reads++
	switch f.mode {
	case "denied":
		return nil, "", api.StatusErrorf(http.StatusForbidden, "sensitive denial")
	case "lookup failure":
		return nil, "", errors.New("sensitive transport failure")
	case "boundary race":
		if f.reads == 1 {
			return f.instance, f.etag, nil
		}
		changed := *f.instance
		changed.Status = "Stopped"
		return &changed, "etag-2", nil
	}
	if f.instance == nil || f.instance.Name != name {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
	}
	return f.instance, f.etag, nil
}

func (f *fakeInstanceClient) UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (InstanceOperation, error) {
	f.transfers++
	f.requests = append(f.requests, state)
	if f.mode == "transition failure" {
		return nil, errors.New("sensitive transport failure")
	}
	if f.mode == "nil operation" {
		return nil, nil
	}
	if f.mode == "ownership after transition" {
		delete(f.instance.Config, "user.aginctus.workload")
	}
	if f.instance != nil {
		switch state.Action {
		case "start":
			f.instance.Status = "Running"
		case "stop":
			f.instance.Status = "Stopped"
		}
	}
	f.etag = "etag-transitioned"
	if f.operation == nil {
		f.operation = &fakeInstanceOperation{}
	}
	return f.operation, nil
}

func workloadLifecycleIntent(t *testing.T, state InstanceState) InstanceLifecycleIntent {
	t.Helper()
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	return InstanceLifecycleIntent{
		Scope: scope, Name: "aginctus-dev", Resource: "workload",
		Role: "agent-workload", WorkloadID: "dev", State: state,
	}
}

func ownedInstance(t *testing.T, intent InstanceLifecycleIntent, status string) *api.Instance {
	t.Helper()
	return &api.Instance{
		Name: intent.Name, Project: intent.Scope.Name, Status: status,
		InstancePut: api.InstancePut{Config: map[string]string{
			"user.aginctus.managed":  "true",
			"user.aginctus.resource": intent.Resource,
			"user.aginctus.role":     intent.Role,
			"user.aginctus.workload": intent.WorkloadID,
		}},
	}
}

func TestNativeInstanceLifecycleStartsOwnedInstance(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	fake := &fakeInstanceClient{scope: intent.Scope, instance: ownedInstance(t, intent, "Stopped"), etag: "etag-1"}
	result, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureInstanceStateWithClient() error = %v", err)
	}
	if !result.Changed || !result.Verified || result.State != InstanceRunning {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fake.transfers != 1 || len(fake.requests) != 1 {
		t.Fatalf("unexpected transitions: %+v", fake)
	}
	if fake.requests[0].Action != "start" || fake.requests[0].Force {
		t.Fatalf("unexpected transition request: %+v", fake.requests[0])
	}
	if fake.operation == nil || fake.operation.waits != 1 {
		t.Fatal("transition operation was not awaited")
	}
}

func TestNativeInstanceLifecycleStopsOwnedInstance(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceStopped)
	fake := &fakeInstanceClient{scope: intent.Scope, instance: ownedInstance(t, intent, "Running"), etag: "etag-1"}
	result, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureInstanceStateWithClient() error = %v", err)
	}
	if !result.Changed || !result.Verified {
		t.Fatalf("unexpected result: %+v", result)
	}
	if fake.requests[0].Action != "stop" || fake.requests[0].Force || fake.requests[0].Stateful {
		t.Fatalf("unexpected transition request: %+v", fake.requests[0])
	}
}

func TestNativeInstanceLifecycleKeepsIncusCLIStopSemantics(t *testing.T) {
	// The Incus CLI sends no client-side timeout, so the daemon applies
	// its own high shutdown timeout. A short timeout would force the
	// instance down where the CLI waited for a graceful shutdown.
	for _, state := range []InstanceState{InstanceStopped, InstanceRunning} {
		t.Run(string(state), func(t *testing.T) {
			intent := workloadLifecycleIntent(t, state)
			request := intent.stateRequest()
			if request.Timeout != instanceNoTimeout {
				t.Fatalf("transition imposed a client-side timeout: %+v", request)
			}
			if request.Force || request.Stateful {
				t.Fatalf("transition forced the instance or stored runtime state: %+v", request)
			}
		})
	}
}

func TestNativeInstanceLifecycleIsIdempotent(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	fake := &fakeInstanceClient{scope: intent.Scope, instance: ownedInstance(t, intent, "Running"), etag: "etag-1"}
	result, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureInstanceStateWithClient() error = %v", err)
	}
	if result.Changed || !result.Verified || fake.transfers != 0 {
		t.Fatalf("converged instance was transitioned: %+v, %+v", result, fake)
	}
}

func TestLifecycleIdentityAndPostflightBoundaries(t *testing.T) {
	for _, mode := range []string{"identity", "deployment", "empty ETag", "nil operation", "ownership after transition", "noop race"} {
		t.Run(mode, func(t *testing.T) {
			intent := workloadLifecycleIntent(t, InstanceRunning)
			f := &fakeInstanceClient{scope: intent.Scope, instance: ownedInstance(t, intent, "Stopped"), etag: "etag-1", mode: mode}
			switch mode {
			case "identity":
				f.instance.Project = "foreign"
			case "deployment":
				f.instance.Config["user.aginctus.deployment"] = "foreign"
			case "empty ETag":
				f.etag = ""
			case "noop race":
				f.instance.Status = "Running"
				f.mode = "boundary race"
			}
			result, err := EnsureInstanceStateWithClient(context.Background(), intent, f)
			if err == nil || result.Verified {
				t.Fatalf("unsafe success: %+v %v", result, err)
			}
			if mode == "nil operation" || mode == "ownership after transition" {
				var failure *ReconcileError
				if !errors.As(err, &failure) || !failure.Uncertain || f.transfers != 1 {
					t.Fatalf("lost uncertainty: %v", err)
				}
			} else if f.transfers != 0 {
				t.Fatal("unsafe transition submitted")
			}
		})
	}
}

func TestNativeInstanceLifecycleRefusesForeignInstance(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	foreign := ownedInstance(t, intent, "Stopped")
	delete(foreign.Config, "user.aginctus.workload")
	fake := &fakeInstanceClient{scope: intent.Scope, instance: foreign, etag: "etag-1"}
	_, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("foreign instance was transitioned")
	}
	if fake.transfers != 0 {
		t.Fatalf("foreign instance was transitioned: %+v", fake)
	}
	var failure *ReconcileError
	if !errors.As(err, &failure) || failure.Stage != "ownership" {
		t.Fatalf("unexpected failure report: %v", err)
	}
}

func TestNativeInstanceLifecycleRefusesAbsentInstance(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	fake := &fakeInstanceClient{scope: intent.Scope, etag: "etag-1"}
	_, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("absent instance was transitioned")
	}
	if fake.transfers != 0 {
		t.Fatalf("absent instance was transitioned: %+v", fake)
	}
}

func TestNativeInstanceLifecycleRequiresLocalProjectScope(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	for _, mode := range []string{"remote", "wrong project"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeInstanceClient{scope: intent.Scope, mode: mode, instance: ownedInstance(t, intent, "Stopped"), etag: "etag-1"}
			_, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
			if err == nil {
				t.Fatalf("%s transport was not refused", mode)
			}
			if fake.transfers != 0 {
				t.Fatalf("%s transport caused a transition: %+v", mode, fake)
			}
		})
	}
}

func TestNativeInstanceLifecycleRefusesDeniedObservation(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	for _, mode := range []string{"denied", "lookup failure"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeInstanceClient{scope: intent.Scope, mode: mode}
			_, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
			if err == nil {
				t.Fatalf("%s observation was not refused", mode)
			}
			if fake.transfers != 0 {
				t.Fatalf("%s observation caused a transition: %+v", mode, fake)
			}
		})
	}
}

func TestNativeInstanceLifecycleFailsClosedOnUnexpectedChange(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	fake := &fakeInstanceClient{
		scope: intent.Scope, instance: ownedInstance(t, intent, "Stopped"),
		etag: "etag-1", mode: "boundary race",
	}
	_, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("unexpected change between planning and execution was not detected")
	}
	if fake.transfers != 0 {
		t.Fatalf("unexpected change caused a transition: %+v", fake)
	}
	var failure *ReconcileError
	if !errors.As(err, &failure) || failure.Stage != "mutation-boundary" {
		t.Fatalf("unexpected failure report: %v", err)
	}
}

func TestNativeInstanceLifecycleReportsUncertainCompletion(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	fake := &fakeInstanceClient{
		scope: intent.Scope, instance: ownedInstance(t, intent, "Stopped"),
		etag: "etag-1", mode: "transition failure",
	}
	result, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	var failure *ReconcileError
	if !errors.As(err, &failure) {
		t.Fatalf("unexpected error type: %v", err)
	}
	if !failure.Mutated || !failure.Uncertain || result.Verified {
		t.Fatalf("uncertain completion was not reported: %+v, %+v", failure, result)
	}
}

func TestNativeInstanceLifecycleHonorsCancellation(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeInstanceClient{scope: intent.Scope, instance: ownedInstance(t, intent, "Stopped"), etag: "etag-1"}
	_, err := EnsureInstanceStateWithClient(ctx, intent, fake)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not reported: %v", err)
	}
	if fake.transfers != 0 {
		t.Fatalf("cancellation before mutation caused a transition: %+v", fake)
	}
}

func TestNativeInstanceLifecycleIntentValidation(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	if err := intent.Validate(); err != nil {
		t.Fatalf("valid intent rejected: %v", err)
	}
	renamed := workloadLifecycleIntent(t, InstanceRunning)
	renamed.Name = "../other"
	if err := renamed.Validate(); err == nil {
		t.Fatal("intent with an unsafe name was accepted")
	}
	scoped := workloadLifecycleIntent(t, InstanceRunning)
	scoped.Scope = incusscope.ProjectScope{Name: "default"}
	if err := scoped.Validate(); err == nil {
		t.Fatal("intent with the default project was accepted")
	}
	unknown := workloadLifecycleIntent(t, InstanceRunning)
	unknown.State = InstanceState("frozen")
	if err := unknown.Validate(); err == nil {
		t.Fatal("intent with an unsupported state was accepted")
	}
	anonymous := workloadLifecycleIntent(t, InstanceRunning)
	anonymous.Resource = ""
	if err := anonymous.Validate(); err == nil {
		t.Fatal("intent without an ownership resource was accepted")
	}
}

func TestNativeInstanceLifecycleNeverLeaksUpstreamDetail(t *testing.T) {
	intent := workloadLifecycleIntent(t, InstanceRunning)
	fake := &fakeInstanceClient{
		scope: intent.Scope, instance: ownedInstance(t, intent, "Stopped"),
		etag: "etag-1", mode: "transition failure",
	}
	_, err := EnsureInstanceStateWithClient(context.Background(), intent, fake)
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("upstream failure detail leaked: %v", err)
	}
}
