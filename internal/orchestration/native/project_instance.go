package native

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// ProjectInstanceClient is the scoped SDK surface for accepted instance lifecycle.
// Its connection must be authenticated and exclusively bound by the caller.
type ProjectInstanceClient interface {
	environment.RestrictedIncusProjectObserver
	GetInstance(string) (*api.Instance, string, error)
	GetImageAlias(string) (*api.ImageAliasesEntry, string, error)
	GetImage(string) (*api.Image, string, error)
	CreateInstance(api.InstancesPost) (incus.Operation, error)
	UpdateInstance(string, api.InstancePut, string) (incus.Operation, error)
	UpdateInstanceState(string, api.InstanceStatePut, string) (incus.Operation, error)
	GetInstanceSnapshotNames(string) ([]string, error)
	GetInstanceBackupNames(string) ([]string, error)
	DeleteInstance(string) (incus.Operation, error)
}

var _ ProjectInstanceClient = (incus.InstanceServer)(nil)

// VerifyProjectTransport checks a separately provisioned server pin against the
// enrolled binding. This is not the independent authority or writer fence.
func VerifyProjectTransport(ctx context.Context, reader environment.RestrictedIncusProjectObserver, binding environment.Binding, trustedPin string) error {
	if reader == nil || binding.ServerID != "sha256:"+trustedPin {
		return errors.New("independently pinned Incus binding required")
	}
	info, err := reader.GetConnectionInfo()
	if err != nil || info == nil {
		return errors.New("unqualified project connection required")
	}
	observer := &standaloneProjectObserver{RestrictedIncusProjectObserver: reader, initialTarget: info.Target}
	v := environment.RestrictedIncusProjectVerifier{Observer: observer, PinnedServerCertificateSHA256: trustedPin}
	actual, err := v.VerifyProject(ctx, binding.Project)
	if err != nil || actual != binding || ctx.Err() != nil {
		return observationFailure(ctx, "restricted Incus mutation scope unverified")
	}
	after, err := reader.GetConnectionInfo()
	if err != nil || after == nil || after.Target != observer.serverName || after.URL != info.URL || after.Project != binding.Project || after.Certificate != info.Certificate || after.SocketPath != "" || after.Protocol != "incus" {
		return observationFailure(ctx, "project transport changed during verification")
	}
	return nil
}

type standaloneProjectObserver struct {
	environment.RestrictedIncusProjectObserver
	initialTarget, serverName string
}

func (o *standaloneProjectObserver) GetServer() (*api.Server, string, error) {
	// The SDK reports the server's advertised name as Target even without
	// UseTarget. Allow that value only on a verified standalone server.
	server, etag, err := o.RestrictedIncusProjectObserver.GetServer()
	if err != nil || server == nil || server.Environment.ServerClustered || (o.initialTarget != "" && o.initialTarget != server.Environment.ServerName) {
		return nil, "", errors.New("standalone Incus project connection required")
	}
	o.serverName = server.Environment.ServerName
	return server, etag, nil
}

type instanceConnection interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
}

func verifyInstanceTransport(ctx context.Context, c instanceConnection, project string) error {
	if scoped, ok := c.(projectInstanceSDK); ok {
		if scoped.binding.Project != project {
			return errors.New("instance project differs from verified scope")
		}
		return VerifyProjectTransport(ctx, scoped.ProjectInstanceClient, scoped.binding, scoped.pin)
	}
	info, err := c.GetConnectionInfo()
	if err != nil {
		return errors.New("local transport unavailable")
	}
	return localTransport(info, project)
}

type projectInstanceSDK struct {
	ProjectInstanceClient
	binding     environment.Binding
	pin         string
	incarnation time.Time
}

func (c projectInstanceSDK) GetInstance(name string) (*api.Instance, string, error) {
	i, etag, err := c.ProjectInstanceClient.GetInstance(name)
	if err == nil && !c.incarnation.IsZero() && (i == nil || !i.CreatedAt.Equal(c.incarnation) || i.Ephemeral) {
		return nil, "", errors.New("instance incarnation changed")
	}
	return i, etag, err
}

func (c projectInstanceSDK) CreateInstance(p api.InstancesPost) (InstanceCreateOperation, error) {
	return c.ProjectInstanceClient.CreateInstance(p)
}

func (c projectInstanceSDK) UpdateInstance(n string, p api.InstancePut, etag string) (InstanceOperation, error) {
	return c.ProjectInstanceClient.UpdateInstance(n, p, etag)
}

func (c projectInstanceSDK) UpdateInstanceState(n string, p api.InstanceStatePut, etag string) (InstanceOperation, error) {
	return c.ProjectInstanceClient.UpdateInstanceState(n, p, etag)
}

// ProjectInstanceOperation selects exactly one lifecycle operation. Removal
// never stops an instance; stop is graceful and never deletes an ephemeral one.
type ProjectInstanceOperation string

const (
	ReconcileInstance ProjectInstanceOperation = "reconcile"
	StartInstance     ProjectInstanceOperation = "start"
	StopInstance      ProjectInstanceOperation = "stop"
	RemoveInstance    ProjectInstanceOperation = "remove"
	Start             Action                   = "start"
	Stop              Action                   = "stop"
)

// ProjectInstanceStage holds a detached typed intent, not an authority grant.
type ProjectInstanceStage struct {
	intent InstanceIntent
	op     ProjectInstanceOperation
	client projectInstanceSDK
}

func NewProjectInstanceStage(intent InstanceIntent, op ProjectInstanceOperation, binding environment.Binding, trustedPin string, client ProjectInstanceClient) (ProjectInstanceStage, error) {
	if intent.Validate() != nil || intent.EnvironmentID == "" || intent.ImageFingerprint == "" ||
		binding.Project != intent.Identity.Project || !fingerprintPattern.MatchString(trustedPin) ||
		binding.ServerID != "sha256:"+trustedPin || client == nil {
		return ProjectInstanceStage{}, errors.New("invalid pinned lifecycle instance binding")
	}
	switch op {
	case ReconcileInstance, StartInstance, StopInstance, RemoveInstance:
	default:
		return ProjectInstanceStage{}, errors.New("unsupported instance lifecycle operation")
	}
	intent.Config = maps.Clone(intent.Config)
	intent.Devices = cloneDevices(intent.Devices)
	intent.Profiles = slices.Clone(intent.Profiles)
	return ProjectInstanceStage{intent, op, projectInstanceSDK{ProjectInstanceClient: client, binding: binding, pin: trustedPin}}, nil
}

// Observe returns redacted feasible actions and a stable incarnation for
// verification. A zero incarnation represents verified absence only.
func (s ProjectInstanceStage) Observe(ctx context.Context) ([]Action, time.Time, error) {
	if s.client.ProjectInstanceClient == nil {
		return nil, time.Time{}, errors.New("missing project instance binding")
	}
	i, err := s.snapshot(ctx, s.client)
	if err != nil {
		return nil, time.Time{}, err
	}
	if i == nil {
		switch s.op {
		case RemoveInstance:
			return []Action{Unchanged}, time.Time{}, nil
		case ReconcileInstance:
			// Creation can only use explicit devices. Inherited policy is not
			// inspected or approved by this create-if-absent operation.
			if len(s.intent.Profiles) != 0 {
				return nil, time.Time{}, errors.New("native creation requires no inherited profiles")
			}
			plan, err := PlanInstanceWithReader(ctx, s.intent, s.client)
			if err != nil || plan.Action != Create {
				return nil, time.Time{}, observationFailure(ctx, "instance creation prerequisites unverified")
			}
			actions := []Action{Create}
			if s.intent.EnsureRunning {
				actions = append(actions, Start)
			}
			return actions, time.Time{}, nil
		default:
			return nil, time.Time{}, errors.New("selected lifecycle instance is absent")
		}
	}
	var actions []Action
	switch s.op {
	case RemoveInstance:
		if err := s.removable(ctx, i); err != nil {
			return nil, time.Time{}, err
		}
		actions = append(actions, Delete)
	case StopInstance:
		if i.Status == "Running" {
			actions = append(actions, Stop)
		}
	case StartInstance, ReconcileInstance:
		intent, c := s.intent, s.client
		intent.EnsureRunning, c.incarnation = false, i.CreatedAt
		p, err := PlanWithInstanceReader(ctx, intent, c)
		if err != nil || p.Action == ReplacementRequired || s.op == StartInstance && p.Action != Unchanged {
			return nil, time.Time{}, observationFailure(ctx, "selected instance drift blocks lifecycle")
		}
		if p.Action == Update {
			actions = append(actions, Update)
		}
		if (s.op == StartInstance || s.intent.EnsureRunning) && i.Status == "Stopped" {
			actions = append(actions, Start)
		}
	}
	if len(actions) == 0 {
		actions = []Action{Unchanged}
	}
	return actions, i.CreatedAt, nil
}

// Apply submits each exact feasible operation through run. The guard invokes
// the closure before the domain repeats its ownership/incarnation checks and
// calls Incus. Neither preparation nor a completed effect grants a later one.
func (s ProjectInstanceStage) Apply(ctx context.Context, expected time.Time, run func(Action, func(context.Context) error) error) (time.Time, error) {
	if run == nil {
		return time.Time{}, errors.New("instance effect capability required")
	}
	actions, incarnation, err := s.Observe(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if !incarnation.Equal(expected) {
		return time.Time{}, errors.New("instance incarnation changed since preview")
	}
	for _, action := range actions {
		if action == Unchanged {
			continue
		}
		if err := run(action, func(ctx context.Context) error {
			c := s.client
			c.incarnation = incarnation
			current, err := s.snapshot(ctx, c)
			if err != nil || (current == nil) != incarnation.IsZero() {
				return observationFailure(ctx, "instance changed before submission")
			}
			intent := s.intent
			intent.EnsureRunning = false
			switch action {
			case Create:
				created, err := CreateInstanceWithClient(ctx, intent, c)
				if err != nil {
					return err
				}
				c.incarnation = created.incarnation
				i, err := s.snapshot(ctx, c)
				if err != nil || i == nil || i.Status != "Stopped" {
					return observationFailure(ctx, "created stopped instance incarnation unverified")
				}
				incarnation = i.CreatedAt
			case Update:
				_, err := UpdateInstanceWithClient(ctx, intent, c)
				return err
			case Start:
				p, err := PlanWithInstanceReader(ctx, intent, c)
				if err != nil || p.Action != Unchanged {
					return observationFailure(ctx, "instance changed before start")
				}
				_, err = EnsureInstanceStateWithClient(ctx, intent.lifecycle(InstanceRunning), c)
				return err
			case Stop:
				_, err := EnsureInstanceStateWithClient(ctx, intent.lifecycle(InstanceStopped), c)
				return err
			case Delete:
				if err := s.remove(ctx, c, current); err != nil {
					return err
				}
				incarnation = time.Time{}
			}
			return nil
		}); err != nil {
			return time.Time{}, err
		}
	}
	return incarnation, nil
}

func (s ProjectInstanceStage) snapshot(ctx context.Context, c projectInstanceSDK) (*api.Instance, error) {
	if err := verifyInstanceTransport(ctx, c, s.intent.Identity.Project); err != nil {
		return nil, err
	}
	scope, _ := incusscope.NewProjectScope(c.binding.Project)
	project, etag, err := c.GetProject(scope.Name)
	if err != nil || project == nil || etag == "" || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return nil, observationFailure(ctx, "instance project policy unverified")
	}
	i, etag, err := c.GetInstance(s.intent.Identity.Name)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if i == nil && etag == "" && api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, nil
	}
	if err != nil || i == nil || etag == "" || i.Name != s.intent.Identity.Name || i.Project != c.binding.Project ||
		i.Type != s.intent.Type || i.CreatedAt.IsZero() || i.Ephemeral || (i.Status != "Stopped" && i.Status != "Running") ||
		!environmentOwnerMatches(i.Config, s.intent.EnvironmentID) {
		return nil, errors.New("selected instance identity or lifecycle unverified")
	}
	for key, want := range s.intent.ownership() {
		if i.Config[key] != want {
			return nil, errors.New("selected instance ownership unverified")
		}
	}
	if s.intent.HerdrClient {
		// Guard persistent identity even for stop and removal, which deliberately
		// do not require the old image to remain in the project.
		state := i.ExpandedDevices["state_system"]
		for key, want := range s.intent.Devices["state_system"] {
			if state[key] != want {
				return nil, errors.New("unverified Herdr identity volume attachment")
			}
		}
		if state["readonly"] != "" && state["readonly"] != "false" {
			return nil, errors.New("cannot verify a writable Herdr identity volume")
		}
	}
	if deployment := i.Config["user.aginctus.deployment"]; deployment != "" && deployment != c.binding.Project {
		return nil, errors.New("selected instance deployment conflicts with project")
	}
	return i, nil
}

func (s ProjectInstanceStage) removable(ctx context.Context, i *api.Instance) error {
	if i.Status != "Stopped" || i.Stateful || i.Config["security.protection.delete"] != "" || i.ExpandedConfig["security.protection.delete"] != "" {
		return errors.New("remove requires a stopped, stateless, unprotected instance")
	}
	snaps, err := s.client.GetInstanceSnapshotNames(i.Name)
	if err != nil || len(snaps) != 0 {
		return observationFailure(ctx, "remove requires verified absence of instance snapshots")
	}
	backups, err := s.client.GetInstanceBackupNames(i.Name)
	if err != nil || len(backups) != 0 {
		return observationFailure(ctx, "remove requires verified absence of instance backups")
	}
	return ctx.Err()
}

func (s ProjectInstanceStage) remove(ctx context.Context, c projectInstanceSDK, i *api.Instance) error {
	if err := s.removable(ctx, i); err != nil {
		return err
	}
	again, err := s.snapshot(ctx, c)
	if err != nil || again == nil || again.Status != "Stopped" || again.Stateful || again.Config["security.protection.delete"] != "" || again.ExpandedConfig["security.protection.delete"] != "" {
		return observationFailure(ctx, "instance changed before removal")
	}
	// Incus DELETE does not take an ETag. The caller's live writer fence must
	// exclude cooperating writers; an unrelated admin is not fenced by this read.
	op, err := c.DeleteInstance(i.Name)
	if err != nil || op == nil {
		return observationFailure(ctx, "instance deletion submission unverified")
	}
	if err := op.WaitContext(ctx); err != nil {
		return observationFailure(ctx, "instance deletion completion unverified")
	}
	after, err := s.snapshot(ctx, s.client)
	if err != nil || after != nil {
		return observationFailure(ctx, "instance absence unverified after removal")
	}
	return nil
}
