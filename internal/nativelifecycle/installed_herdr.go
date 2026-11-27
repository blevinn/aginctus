package nativelifecycle

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/incus/statevolume"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

var errHerdrAuthority = errors.New("selected Herdr authority or attachments unverified")

type trackedHerdrClient struct {
	trackedWorkloadClient
	sdk          incus.InstanceServer
	guard        func() error
	pool, volume string
	bootstrap    bool
	operation native.ProjectInstanceOperation
}

func (c trackedHerdrClient) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	return c.sdk.GetStoragePoolVolume(pool, kind, name)
}
func (c trackedHerdrClient) CreateStoragePoolVolume(pool string, post api.StorageVolumesPost) error {
	if !c.bootstrap || pool != c.pool || post.Name != c.volume || c.guard() != nil {
		return errHerdrAuthority
	}
	return c.sdk.CreateStoragePoolVolume(pool, post)
}
func (c trackedHerdrClient) CreateInstance(p api.InstancesPost) (incus.Operation, error) {
	if c.operation != native.ReconcileInstance || p.Name != c.instance || c.guard() != nil {
		return nil, errHerdrAuthority
	}
	return c.trackedWorkloadClient.CreateInstance(p)
}
func (c trackedHerdrClient) UpdateInstanceState(n string, p api.InstanceStatePut, etag string) (incus.Operation, error) {
	if n != c.instance || p.Force || !(c.operation == native.ReconcileInstance && p.Action == "start" || c.operation == native.StopInstance && p.Action == "stop") || c.guard() != nil {
		return nil, errHerdrAuthority
	}
	return c.trackedWorkloadClient.UpdateInstanceState(n, p, etag)
}
func (c trackedHerdrClient) UpdateInstance(string, api.InstancePut, string) (incus.Operation, error) {
	return nil, errHerdrAuthority
}
func (c trackedHerdrClient) DeleteInstance(name string) (incus.Operation, error) {
	if c.operation != native.RemoveInstance || name != c.instance || c.guard() != nil {
		return nil, errHerdrAuthority
	}
	return c.trackedWorkloadClient.DeleteInstance(name)
}

// ExecuteInstalledHerdr composes the accepted Herdr container and persistent
// identity volume through the same installed native engine and writer service.
// This establishes resource lifecycle, not SSH/application/workspace readiness.
func ExecuteInstalledHerdr(ctx context.Context, configPath, grantPath, project, id, revision string, bootstrap, planning bool) (result orchestration.Result, operationID string, err error) {
	return executeInstalledHerdr(ctx,configPath,grantPath,project,id,revision,bootstrap,planning,native.ReconcileInstance,time.Time{},time.Time{})
}

// ExecuteInstalledHerdrTeardown selects only the existing Herdr incarnation
// and its retained system volume. Stop and root-disk removal are distinct.
func ExecuteInstalledHerdrTeardown(ctx context.Context,configPath,grantPath,project,id,revision string,operation native.ProjectInstanceOperation,instanceAt,volumeAt time.Time)(orchestration.Result,string,error){
	if operation!=native.StopInstance && operation!=native.RemoveInstance || instanceAt.IsZero() || volumeAt.IsZero(){
		return orchestration.Result{},"",errHerdrAuthority
	}
	return executeInstalledHerdr(ctx,configPath,grantPath,project,id,revision,false,false,operation,instanceAt,volumeAt)
}

func executeInstalledHerdr(ctx context.Context, configPath, grantPath, project, id, revision string, bootstrap, planning bool, operation native.ProjectInstanceOperation,instanceAt,volumeAt time.Time) (result orchestration.Result, operationID string, err error) {
	grant, err := environment.ReadWorkloadHostGrant(grantPath)
	if err != nil {
		return result, "", err
	}
	operator, err := environment.ReadRestrictedOperatorReadConfig(configPath)
	if err != nil {
		return result, "", err
	}
	if project != operator.Project || grant.Project != project || grant.EnvironmentID != id || grant.WorkloadID != herdrclient.NativeInstanceID ||
		grant.ServerID != "sha256:"+operator.TrustedServerSHA256 {
		return result, "", errHerdrAuthority
	}
	sdk, err := environment.OpenExplicitRestrictedIncusProject(ctx, operator.Connection())
	if err != nil {
		return result, "", err
	}
	defer sdk.Disconnect()
	var accepted environment.AcceptedRevision
	var candidate environment.Candidate
	var session *environment.ProjectExecutionSession
	var snapshot *environment.AcceptedReadModel
	if planning {
		reads, closeReads, err := environment.OpenProductionOperatorReads(ctx, configPath)
		if err != nil {
			return result, "", err
		}
		defer closeReads()
		snapshot = reads.AcceptedSnapshot
		if snapshot == nil {
			return result, "", errHerdrAuthority
		}
		accepted, candidate, err = snapshot.ReadAcceptedSnapshot(ctx, project, id)
		if err != nil || accepted.ID != revision {
			return result, "", environment.ErrAcceptedConflict
		}
	} else {
		op := "herdr-"+string(operation)
		if bootstrap {
			op = "herdr-bootstrap"
		}
		var closeReads func()
		session, candidate, closeReads, err = environment.OpenProjectExecution(ctx, operator, project, id, revision, herdrclient.NativeInstanceID, op)
		if err != nil {
			return result, "", err
		}
		defer closeReads()
		operationID = session.ID()
		accepted = session.Revision()
		defer func() {
			finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			err = errors.Join(err, session.Finish(finish, err == nil))
		}()
	}
	h := candidate.Herdr
	if accepted.ValidateAgainst(candidate) != nil || h == nil || h.InstanceName != grant.Instance || h.StoragePool != grant.Pool || h.ManagementNetwork != grant.Bridge ||
		bootstrap && !h.AllowStateBootstrap {
		return result, operationID, errHerdrAuthority
	}
	verifyHost := func(ctx context.Context) error {
		current, err := environment.ReadWorkloadHostGrant(grantPath)
		if err != nil || current != grant || native.VerifyProjectTransport(ctx, sdk, candidate.Environment.Binding, operator.TrustedServerSHA256) != nil {
			return errHerdrAuthority
		}
		p, _, err := sdk.GetProject(project)
		if err != nil || p == nil || p.Name != project || p.Config["restricted"] != "true" || p.Config["restricted.networks.access"] != grant.Bridge ||
			p.Config["restricted.storage-pools.access"] != grant.Pool || p.Config["features.storage.volumes"] != "true" {
			return errHerdrAuthority
		}
		if session != nil {
			return session.Check(ctx)
		}
		actual, _, err := snapshot.ReadAcceptedSnapshot(ctx, project, id)
		if err != nil || actual != accepted {
			return environment.ErrAcceptedConflict
		}
		return ctx.Err()
	}
	var client native.ProjectInstanceClient = sdk
	if !planning {
		volume := statevolume.Volume{Pool: h.StoragePool, Instance: h.InstanceName, Class: "system", Project: project, EnvironmentID: id}
		client = trackedHerdrClient{
			trackedWorkloadClient: trackedWorkloadClient{ProjectInstanceClient: sdk, record: session.RecordDaemonOperation, project: project, instance: grant.Instance},
			sdk:                   sdk, guard: func() error { return verifyHost(ctx) }, pool: grant.Pool, volume: volume.Name(), bootstrap: bootstrap, operation:operation,
		}
	}
	bridge := workloadBridge(grant)
	composer := LocalComposer{Environment: candidate.Environment, TrustedServerPin: operator.TrustedServerSHA256, Client: client,
		Bridge: ManagementNetwork{Spec: bridge, VerifyObserved: func(ctx context.Context) error { return verifyWorkloadBridge(ctx, grant) },
			VerifyBinding: func(ctx context.Context, b environment.Binding, s managementnetwork.Spec) error {
				if b != candidate.Environment.Binding || s != bridge {
					return errHerdrAuthority
				}
				return verifyHost(ctx)
			}},
		VerifyAttachments: func(ctx context.Context, d environment.Descriptor, instance, pool, network string) error {
			if !d.SameEnvironment(candidate.Environment) || instance != grant.Instance || pool != grant.Pool || network != grant.Bridge {
				return errHerdrAuthority
			}
			return verifyHost(ctx)
		},
	}
	preview, err := orchestration.PreviewAcceptedIntent(accepted, candidate, orchestration.AcceptedIntentSelection{Herdr: true})
	if err != nil {
		return result, operationID, err
	}
	var plan orchestration.NativePlan
	var resources map[string]orchestration.NativeResourceBinding
	if operation==native.ReconcileInstance {
		plan,resources,err=composer.BindLifecycle(candidate,preview.Selection,bootstrap)
	} else {
		preview.Selection.Resources=[]orchestration.NativeResource{
			{ID:herdrclient.NativeVolumeID,Kind:orchestration.NativeHerdrVolume},
			{ID:herdrclient.NativeInstanceID,Kind:orchestration.NativeHerdrInstance,DependsOn:[]string{herdrclient.NativeVolumeID}},
		}
		plan,resources,err=composer.BindTeardown(candidate,preview.Selection,orchestration.LocalLifecycleTeardown{
			Operation:operation,Instances:map[string]time.Time{herdrclient.NativeInstanceID:instanceAt},HerdrState:volumeAt,
		})
	}
	if err != nil {
		return result, operationID, err
	}
	for name, b := range resources {
		if planning {
			b.Apply = nil
		} else {
			apply := b.Apply
			b.Apply = func(ctx context.Context, run orchestration.RunNativeEffect) error {
				return apply(ctx, func(effect orchestration.NativeEffect, submit func(context.Context) error) error {
					if submit == nil {
						return run(effect, nil)
					}
					return run(effect, func(ctx context.Context) error {
						action := installedHerdrEffectAction(name, effect, bootstrap, operation)
						if action == "" || verifyHost(ctx) != nil || session.BeginEffect(ctx, action) != nil {
							return errHerdrAuthority
						}
						if submit(ctx) != nil || verifyHost(ctx) != nil || session.MarkApplied(ctx) != nil {
							return errHerdrAuthority
						}
						return nil
					})
				})
			}
		}
		resources[name] = b
	}
	result, err = orchestration.NewEngine(nil).ExecuteNative(ctx, plan, orchestration.NativeBindings{
		Resources: resources, CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks,
			Authorize: func(ctx context.Context, scope orchestration.TargetScope, c check.Check) error {
				if scope.Deployment != id || c.Effect != check.ReadOnly {
					return errHerdrAuthority
				}
				return ctx.Err()
			}},
		VerifyPlan: func(ctx context.Context, p orchestration.NativePlan) error {
			if !reflect.DeepEqual(p, preview.Selection) {
				return errHerdrAuthority
			}
			return verifyHost(ctx)
		},
		BeforeEffect: func(ctx context.Context, r environment.AcceptedRevision, resource orchestration.NativeResource, effect orchestration.NativeEffect) error {
			if r != accepted || installedHerdrEffectAction(resource.ID, effect, bootstrap, operation) == "" {
				return errHerdrAuthority
			}
			return verifyHost(ctx)
		},
	}, orchestration.ExecuteOptions{DryRun: planning})
	return result, operationID, err
}

func herdrEffectAction(resource string, e orchestration.NativeEffect, bootstrap bool) string {
	if resource != e.Target {
		return ""
	}
	if resource == herdrclient.NativeVolumeID && bootstrap && e.Action == native.Create {
		return "create-state"
	}
	if resource == herdrclient.NativeInstanceID && (e.Action == native.Create || e.Action == native.Start) {
		return string(e.Action)
	}
	return ""
}

func installedHerdrEffectAction(resource string,e orchestration.NativeEffect,bootstrap bool,operation native.ProjectInstanceOperation)string{
	if operation==native.ReconcileInstance{return herdrEffectAction(resource,e,bootstrap)}
	if bootstrap || resource!=herdrclient.NativeInstanceID || resource!=e.Target{return ""}
	if operation==native.StopInstance && e.Action==native.Stop || operation==native.RemoveInstance && e.Action==native.Delete{return string(e.Action)}
	return ""
}
