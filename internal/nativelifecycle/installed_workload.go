package nativelifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

// ReconcileInstalledWorkload composes one selected workload with a read-only
// management bridge prerequisite. No controller, fixture store, ambient SDK
// client, or host mutation transport participates.
func ReconcileInstalledWorkload(ctx context.Context, configPath, hostGrantPath, project, id, revision, workloadID string) (result orchestration.Result, operationID string, err error) {
	grant, err := environment.ReadWorkloadHostGrant(hostGrantPath)
	if err != nil {
		return result, "", err
	}
	operator, err := environment.ReadRestrictedOperatorReadConfig(configPath)
	if err != nil {
		return result, "", err
	}
	if project != operator.Project || grant.Project != project || grant.EnvironmentID != id || grant.WorkloadID != workloadID ||
		grant.ServerID != "sha256:"+operator.TrustedServerSHA256 {
		return result, "", errors.New("independent workload host grant does not match requested scope")
	}
	sdk, err := environment.OpenExplicitRestrictedIncusProject(ctx, operator.Connection())
	if err != nil {
		return result, "", err
	}
	defer sdk.Disconnect()
	session, candidate, closeReads, err := environment.OpenProjectExecution(ctx, operator, project, id, revision, workloadID, "reconcile")
	if err != nil {
		return result, "", err
	}
	defer closeReads()
	operationID = session.ID()
	defer func() {
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err = errors.Join(err, session.Finish(finish, err == nil))
	}()
	accepted := session.Revision()
	if err := accepted.ValidateAgainst(candidate); err != nil {
		return result, operationID, err
	}
	preview, err := orchestration.PreviewAcceptedIntent(accepted, candidate, orchestration.AcceptedIntentSelection{Workloads: []string{workloadID}})
	if err != nil {
		return result, operationID, err
	}
	bridge := managementnetwork.Spec{Name: grant.Bridge, IPv4Address: grant.IPv4Address, IPv4NAT: grant.IPv4NAT,
		IPv4Routing: grant.IPv4Routing, IPv4DHCPGateway: grant.IPv4DHCPGateway, IPv6Address: grant.IPv6Address}
	verifyHost := func(ctx context.Context) error {
		current, err := environment.ReadWorkloadHostGrant(hostGrantPath)
		if err != nil || current != grant {
			return errors.New("independent workload host grant unavailable or changed")
		}
		return native.VerifyProjectTransport(ctx, sdk, candidate.Environment.Binding, operator.TrustedServerSHA256)
	}
	composer := LocalComposer{Environment: candidate.Environment, TrustedServerPin: operator.TrustedServerSHA256, Client: sdk,
		Bridge: ManagementNetwork{Spec: bridge, VerifyObserved: func(ctx context.Context) error {
			intent, err := bridge.NativeIntent()
			if err != nil {
				return err
			}
			observed, err := native.PlanProjectNetworkPrerequisite(ctx, intent, sdk, candidate.Environment.Binding, operator.TrustedServerSHA256)
			if err != nil || observed.Action != native.Unchanged {
				return errors.New("approved bridge unavailable or not converged")
			}
			return nil
		}, VerifyBinding: func(ctx context.Context, binding environment.Binding, spec managementnetwork.Spec) error {
			if binding != candidate.Environment.Binding || spec != bridge {
				return errors.New("management bridge outside grant")
			}
			return verifyHost(ctx)
		}}, VerifyAttachments: func(ctx context.Context, desc environment.Descriptor, instance, pool, network string) error {
			if !desc.SameEnvironment(candidate.Environment) || instance != grant.Instance || pool != grant.Pool || network != grant.Bridge {
				return errors.New("workload attachments outside grant")
			}
			p, _, err := sdk.GetProject(project)
			if err != nil || p == nil || p.Name != project || p.Config["restricted"] != "true" ||
				p.Config["restricted.networks.access"] != network || p.Config["restricted.storage-pools.access"] != pool {
				return errors.New("exact restricted project attachment policy required")
			}
			return verifyHost(ctx)
		}}
	plan, resources, err := composer.BindLifecycle(candidate, preview.Selection, false)
	if err != nil {
		return result, operationID, err
	}
	bound, ok := resources[workloadID]
	if !ok || bound.Apply == nil || len(resources) != 2 {
		return result, operationID, errors.New("unexpected native workload graph")
	}
	apply := bound.Apply
	bound.Apply = func(ctx context.Context, run orchestration.RunNativeEffect) error {
		return apply(ctx, func(effect orchestration.NativeEffect, submit func(context.Context) error) error {
			if submit == nil {
				return run(effect, nil)
			}
			return run(effect, func(ctx context.Context) error {
				if err := session.BeginEffect(ctx, string(effect.Action)); err != nil {
					return err
				}
				if err := verifyHost(ctx); err != nil {
					return err
				}
				if err := errors.Join(submit(ctx), ctx.Err()); err != nil {
					return err
				}
				return session.MarkApplied(ctx)
			})
		})
	}
	resources[workloadID] = bound
	result, err = orchestration.NewEngine(nil).ExecuteNative(ctx, plan, orchestration.NativeBindings{
		Resources: resources, CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks,
			Authorize: func(ctx context.Context, scope orchestration.TargetScope, c check.Check) error {
				if scope.Deployment != id || c.Effect != check.ReadOnly {
					return errors.New("out-of-scope workload condition")
				}
				return ctx.Err()
			}},
		VerifyPlan: func(ctx context.Context, verified orchestration.NativePlan) error {
			if verified.Revision != accepted || len(verified.Resources) != 2 || verified.Resources[0].ID != ManagementNetworkID ||
				verified.Resources[1].ID != workloadID {
				return errors.New("native workload selection changed")
			}
			return verifyHost(ctx)
		}, BeforeEffect: func(ctx context.Context, rev environment.AcceptedRevision, resource orchestration.NativeResource, effect orchestration.NativeEffect) error {
			if rev != accepted || resource.Kind != orchestration.NativeWorkload || resource.ID != workloadID || effect.Target != workloadID ||
				(effect.Action != native.Create && effect.Action != native.Start) {
				return errors.New("out-of-scope native workload effect")
			}
			return verifyHost(ctx)
		}}, orchestration.ExecuteOptions{})
	return result, operationID, err
}
