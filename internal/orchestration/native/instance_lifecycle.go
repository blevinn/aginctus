package native

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// InstanceState is the running state a managed instance must reach.
type InstanceState string

const (
	// InstanceRunning is the running state.
	InstanceRunning InstanceState = "running"
	// InstanceStopped is the stopped state.
	InstanceStopped InstanceState = "stopped"
)

// instanceNoTimeout requests no client-side timeout. It matches the Incus
// CLI stop default, so the SDK transition keeps the user-visible behaviour
// of the subprocess it replaces: the daemon applies its own high shutdown
// timeout instead of forcing the instance after a short grace period.
const instanceNoTimeout = -1

// InstanceLifecycleIntent describes the required running state of one
// Aginctus-owned instance. It never creates, replaces or removes an
// instance, and it never adopts a foreign instance.
type InstanceLifecycleIntent struct {
	Scope      incusscope.ProjectScope
	Name       string
	Resource   string
	Role       string
	WorkloadID string
	State      InstanceState
}

// Validate refuses an intent that does not identify one owned instance in
// an explicit, non-default project.
func (intent InstanceLifecycleIntent) Validate() error {
	if _, err := incusscope.NewProjectScope(intent.Scope.Name); err != nil {
		return fmt.Errorf("instance lifecycle requires an explicit, non-default project")
	}
	if !localName(intent.Name) {
		return fmt.Errorf("instance name must be an explicit local name")
	}
	if intent.Resource == "" {
		return fmt.Errorf("instance lifecycle requires an ownership resource")
	}
	switch intent.State {
	case InstanceRunning, InstanceStopped:
	default:
		return fmt.Errorf("unsupported instance lifecycle state %q", intent.State)
	}
	return nil
}

// markers are the ownership markers an instance must carry. A missing or
// differing marker makes the instance foreign.
func (intent InstanceLifecycleIntent) markers() map[string]string {
	markers := map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": intent.Resource,
	}
	if intent.Role != "" {
		markers["user.aginctus.role"] = intent.Role
	}
	if intent.WorkloadID != "" {
		markers["user.aginctus.workload"] = intent.WorkloadID
	}
	return markers
}

// InstanceLifecycleObservation is the observed state of one instance. It
// carries no SDK types and confers no authority to write.
type InstanceLifecycleObservation struct {
	Name      string
	Project   string
	Status    string
	Config    map[string]string
	CreatedAt time.Time
}

// reaches reports whether the observed instance already satisfies the
// required state.
func (observed InstanceLifecycleObservation) reaches(state InstanceState) bool {
	switch state {
	case InstanceRunning:
		return strings.EqualFold(observed.Status, "Running")
	case InstanceStopped:
		return strings.EqualFold(observed.Status, "Stopped")
	default:
		return false
	}
}

// InstanceLifecycleResult reports the reached state and what was verified.
type InstanceLifecycleResult struct {
	Instance string        `json:"instance"`
	Project  string        `json:"project"`
	State    InstanceState `json:"state"`
	Changed  bool          `json:"changed"`
	Verified bool          `json:"verified"`
}

// InstanceOperation is the bounded wait for one Incus instance operation.
type InstanceOperation interface {
	WaitContext(context.Context) error
}

// InstanceLifecycleClient is the narrow Incus surface used to read and
// transition one instance. It cannot reach images, volumes or other
// projects.
type InstanceLifecycleClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
	GetInstance(name string) (*api.Instance, string, error)
	UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (InstanceOperation, error)
}

// sdkInstanceClient adapts the Incus SDK to the narrow client contract.
type sdkInstanceClient struct {
	server incus.InstanceServer
}

func (c sdkInstanceClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return c.server.GetConnectionInfo()
}

func (c sdkInstanceClient) GetInstance(name string) (*api.Instance, string, error) {
	return c.server.GetInstance(name)
}

func (c sdkInstanceClient) UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (InstanceOperation, error) {
	operation, err := c.server.UpdateInstanceState(name, state, etag)
	if err != nil {
		return nil, err
	}
	return operation, nil
}

// EnsureLocalInstanceState transitions one owned instance of the selected
// local project. It never spawns the incus CLI.
func EnsureLocalInstanceState(ctx context.Context, intent InstanceLifecycleIntent) (InstanceLifecycleResult, error) {
	if err := intent.Validate(); err != nil {
		return InstanceLifecycleResult{}, err
	}
	server, cancel, err := connectLocal(ctx, intent.Scope.Name)
	if err != nil {
		return InstanceLifecycleResult{}, err
	}
	defer cancel()
	defer server.Disconnect()
	return EnsureInstanceStateWithClient(ctx, intent, sdkInstanceClient{server: server})
}

// EnsureInstanceStateWithClient transitions one owned instance to the
// required running state. It observes, verifies ownership, rechecks the
// instance at the mutation boundary, applies the transition, waits for the
// operation and verifies the reached state.
func EnsureInstanceStateWithClient(
	ctx context.Context,
	intent InstanceLifecycleIntent,
	client InstanceLifecycleClient,
) (InstanceLifecycleResult, error) {
	if err := intent.Validate(); err != nil {
		return InstanceLifecycleResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstanceLifecycleResult{}, err
	}
	info, err := client.GetConnectionInfo()
	if err != nil || localTransport(info, intent.Scope.Name) != nil {
		return InstanceLifecycleResult{}, &ReconcileError{
			Stage: "transport", cause: fmt.Errorf("native instance lifecycle requires the local Incus socket and the selected project"),
		}
	}
	observed, etag, err := observeInstance(ctx, client, intent.Name)
	if err != nil {
		return InstanceLifecycleResult{}, &ReconcileError{Stage: "observation", cause: err}
	}
	if observed == nil {
		return InstanceLifecycleResult{}, &ReconcileError{
			Stage: "ownership", cause: fmt.Errorf("instance is absent"),
		}
	}
	if observed.Name != intent.Name || observed.Project != intent.Scope.Name || etag == "" ||
		(observed.Status != "Running" && observed.Status != "Stopped") ||
		(observed.Config["user.aginctus.deployment"] != "" && observed.Config["user.aginctus.deployment"] != intent.Scope.Name) {
		return InstanceLifecycleResult{}, &ReconcileError{Stage: "ownership", cause: fmt.Errorf("instance identity or lifecycle unverified")}
	}
	for key, want := range intent.markers() {
		if observed.Config[key] != want {
			return InstanceLifecycleResult{}, &ReconcileError{
				Stage: "ownership", cause: fmt.Errorf("instance ownership unverified"),
			}
		}
	}
	result := InstanceLifecycleResult{Instance: intent.Name, Project: intent.Scope.Name, State: intent.State}
	// Recheck at the mutation boundary: the instance may have been changed
	// or removed between the observation and the transition.
	current, currentETag, err := observeInstance(ctx, client, intent.Name)
	if err != nil {
		return result, &ReconcileError{Stage: "mutation-boundary", cause: err}
	}
	if !sameInstanceObservation(observed, current) || etag != currentETag {
		return result, &ReconcileError{
			Stage: "mutation-boundary", cause: fmt.Errorf("instance changed during planning"),
		}
	}
	if err := ctx.Err(); err != nil {
		return result, &ReconcileError{Stage: "cancellation", cause: err}
	}
	if observed.reaches(intent.State) {
		result.Verified = true
		return result, nil
	}
	operation, err := client.UpdateInstanceState(intent.Name, intent.stateRequest(), currentETag)
	if err != nil {
		return result, &ReconcileError{Stage: "transition", Mutated: true, Uncertain: true, cause: err}
	}
	if operation == nil {
		return result, &ReconcileError{Stage: "transition", Mutated: true, Uncertain: true, cause: fmt.Errorf("transition operation unavailable")}
	}
	if err := operation.WaitContext(ctx); err != nil {
		return result, &ReconcileError{Stage: "transition", Mutated: true, Uncertain: true, cause: err}
	}
	result.Changed = true
	after, _, err := observeInstance(ctx, client, intent.Name)
	if err != nil {
		return result, &ReconcileError{Stage: "verification", Mutated: true, Uncertain: true, cause: err}
	}
	if after == nil || !after.reaches(intent.State) || after.Name != intent.Name || after.Project != intent.Scope.Name || !after.CreatedAt.Equal(observed.CreatedAt) || after.Config["user.aginctus.deployment"] != observed.Config["user.aginctus.deployment"] {
		return result, &ReconcileError{
			Stage: "verification", Mutated: true, Uncertain: true, cause: fmt.Errorf("instance did not reach the required state"),
		}
	}
	for key, want := range intent.markers() {
		if after.Config[key] != want {
			return result, &ReconcileError{Stage: "verification", Mutated: true, Uncertain: true, cause: fmt.Errorf("instance ownership changed during transition")}
		}
	}
	result.Verified = true
	return result, nil
}

// stateRequest is the Incus state transition request for the required
// state. The transition never forces a stop, never stores runtime state and
// never imposes a client-side timeout, so the daemon applies the same
// shutdown grace period as the Incus CLI it replaces.
func (intent InstanceLifecycleIntent) stateRequest() api.InstanceStatePut {
	if intent.State == InstanceStopped {
		return api.InstanceStatePut{Action: "stop", Timeout: instanceNoTimeout, Force: false}
	}
	return api.InstanceStatePut{Action: "start", Timeout: instanceNoTimeout, Force: false}
}

// observeInstance reads one instance. Absence is proven by an explicit
// not-found status; any other lookup failure is not an empty observation.
func observeInstance(ctx context.Context, client InstanceLifecycleClient, name string) (*InstanceLifecycleObservation, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	instance, etag, err := client.GetInstance(name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, "", nil
	}
	if err != nil || instance == nil {
		return nil, "", fmt.Errorf("instance observation unavailable")
	}
	return &InstanceLifecycleObservation{
		Name:      instance.Name,
		Project:   instance.Project,
		Status:    instance.Status,
		Config:    maps.Clone(instance.Config),
		CreatedAt: instance.CreatedAt,
	}, etag, nil
}

// sameInstanceObservation compares two read-only snapshots strictly.
func sameInstanceObservation(a, b *InstanceLifecycleObservation) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Name != b.Name || a.Project != b.Project || a.Status != b.Status || !a.CreatedAt.Equal(b.CreatedAt) {
		return false
	}
	return maps.Equal(a.Config, b.Config)
}
