package orchestration

import (
	"context"
	"errors"
	"slices"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

const (
	GatewayImageStage          = "gateway-image"
	GatewayVolumeStage         = "gateway-volume"
	GatewayInstanceStage       = "gateway-instance"
	GatewayRuntimeStage        = "gateway-runtime"
	GatewayPostgresTarget      = "postgres"
	GatewayLiteLLMTarget       = "litellm"
	GatewayDatabaseTarget      = "database"
	GatewayPostgresAliasTarget = "postgres-alias"
	GatewayLiteLLMAliasTarget  = "litellm-alias"
)

type LocalNativePermission string

const (
	LocalNativeInspect LocalNativePermission = "inspect"
	LocalNativeExecute LocalNativePermission = "execute"
	LocalNativeSubmit  LocalNativePermission = "submit"
)

// LocalNativeAuthorityRequest is passed only to an independently actor-bound
// policy callback. A candidate, accepted revision or requested action does not
// prove a grant. Authorizers must verify current actor and scope on every call.
type LocalNativeAuthorityRequest struct {
	Environment    environment.Descriptor
	Permission     LocalNativePermission
	Plan           NativePlan
	Resource       NativeResource
	Effect         NativeEffect
	BootstrapHerdr bool
}

type LocalNativeExecution struct {
	Store       *environment.LocalProjectStateStore
	Environment environment.Descriptor
	Authorize   func(context.Context, LocalNativeAuthorityRequest) (environment.Binding, error)
	// Bind statically composes gateway adapters from the verified candidate, not
	// ambient config or scope supplied by the plan. It must only assemble
	// closures; domain observations and writes belong in the engine phases.
	Bind func(environment.Candidate, NativePlan) (map[string]NativeResourceBinding, error)
	// BindLifecycle builds C's reconcile-only fragment from the accepted body.
	// It returns the graph it actually built for an exact comparison before any
	// observation. BootstrapHerdr is a separate, explicit invocation choice.
	BindLifecycle         func(environment.Candidate, NativePlan, bool) (NativePlan, map[string]NativeResourceBinding, error)
	BootstrapHerdr        bool
	CheckPolicy           CheckPolicy
	AuthorizeTeardown     func(context.Context, LocalLifecycleTeardownRequest) (environment.Binding, error)
	BindLifecycleTeardown func(environment.Candidate, NativePlan, LocalLifecycleTeardown) (NativePlan, map[string]NativeResourceBinding, error)
}

// ExecuteLocalNative is the same-UID fixture join to Stack A. Only the trusted
// fixture composer may supply the binding and actor policy; this is not a route
// for the installed CLI, HTTP requests, or production SDK writes. Cooperative
// file locking cannot fence an independent Incus actor or an untrusted UID.
func (f LocalNativeExecution) ExecuteLocalNative(ctx context.Context, engine *Engine, plan NativePlan, opts ExecuteOptions) (result Result, err error) {
	return f.executeLocalNative(ctx, engine, plan, opts, nil)
}

func (f LocalNativeExecution) executeLocalNative(ctx context.Context, engine *Engine, plan NativePlan, opts ExecuteOptions, teardown *LocalLifecycleTeardown) (result Result, err error) {
	if err := conditionContextErr(ctx); err != nil {
		return Result{}, err
	}
	if engine == nil || f.Store == nil || f.Authorize == nil || f.Environment.Validate() != nil {
		return Result{}, errors.New("local native execution requires explicit fixture store, selection and authority")
	}
	ordered, err := plan.ordered()
	if err != nil {
		return Result{}, err
	}
	plan.Resources = ordered
	if teardown != nil {
		if err := teardown.validate(plan); err != nil {
			return Result{}, err
		}
	}
	if plan.Revision.EnvironmentID != f.Environment.ID || plan.Revision.Project != f.Environment.Binding.Project ||
		plan.Revision.ServerID != f.Environment.Binding.ServerID {
		return Result{}, environment.ErrAcceptedConflict
	}
	authorize := func(ctx context.Context, permission LocalNativePermission, r NativeResource, effect NativeEffect) error {
		if err := conditionContextErr(ctx); err != nil {
			return err
		}
		request := LocalNativeAuthorityRequest{Environment: f.Environment, Permission: permission, Plan: detachLocalPlan(plan), Resource: detachedResource(r), Effect: effect, BootstrapHerdr: f.BootstrapHerdr}
		binding, err := f.Authorize(ctx, request)
		if err != nil || f.Environment.VerifyBinding(binding) != nil {
			return errors.New("local native operation denied")
		}
		if teardown != nil {
			if err := conditionContextErr(ctx); err != nil {
				return err
			}
			request.Plan, request.Resource = detachLocalPlan(plan), detachedResource(r)
			binding, err = f.AuthorizeTeardown(ctx, LocalLifecycleTeardownRequest{request, teardown.detached()})
			if err != nil || f.Environment.VerifyBinding(binding) != nil {
				return errors.New("local teardown operation denied")
			}
		}
		return conditionContextErr(ctx)
	}
	permission := LocalNativeExecute
	if opts.DryRun {
		permission = LocalNativeInspect
	}
	if err := authorize(ctx, permission, NativeResource{}, NativeEffect{}); err != nil {
		return Result{}, err
	}
	var candidate environment.Candidate
	var pin *environment.LocalEnvironmentPin
	var journal *environment.LocalNativeJournal
	checkRevision := func(ctx context.Context) error {
		if pin != nil {
			return pin.Check(ctx)
		}
		revision, _, err := f.Store.ReadLocalAccepted(ctx, f.Environment)
		if err != nil {
			return err
		}
		if revision == nil || *revision != plan.Revision {
			return environment.ErrAcceptedConflict
		}
		return nil
	}
	if opts.DryRun {
		revision, draft, err := f.Store.ReadLocalAccepted(ctx, f.Environment)
		if err != nil {
			return Result{}, err
		}
		if revision == nil || draft == nil || *revision != plan.Revision {
			return Result{}, environment.ErrAcceptedConflict
		}
		candidate = *draft
	} else {
		pin, err = f.Store.PinLocalRevision(ctx, f.Environment, plan.Revision)
		if err != nil {
			return Result{}, err
		}
		defer func() { err = errors.Join(err, pin.Close()) }()
		candidate, err = pin.SnapshotCandidate(ctx)
		if err != nil {
			return Result{}, err
		}
		journal, err = pin.NewLocalNativeJournal()
		if err != nil {
			return Result{}, err
		}
	}
	if teardown != nil {
		err = teardown.validateAccepted(candidate, plan)
	} else {
		err = verifyLocalNativeSelection(candidate, plan.Resources, f.BootstrapHerdr)
	}
	if err != nil {
		return Result{}, err
	}
	// Give the trusted composer a detached copy; its projections do not become
	// authority inputs merely because it builds the SDK closures.
	data, err := candidate.Canonical()
	if err != nil {
		return Result{}, err
	}
	resources, err := f.bindLocalDomains(data, plan, teardown)
	if err != nil {
		return Result{}, err
	}
	permitsEffect := func(r NativeResource, effect NativeEffect) bool {
		if teardown != nil {
			return teardown.permits(r, effect)
		}
		return localEffectSelected(candidate, r, effect, f.BootstrapHerdr)
	}
	beforeEffect := func(ctx context.Context, rev environment.AcceptedRevision, r NativeResource, effect NativeEffect) error {
		if rev != plan.Revision || !permitsEffect(r, effect) {
			return errors.New("effect is outside accepted native intent")
		}
		if err := authorize(ctx, LocalNativeSubmit, r, effect); err != nil {
			return err
		}
		return checkRevision(ctx)
	}
	var operationID string
	bound := make(map[string]NativeResourceBinding, len(resources))
	for id, b := range resources {
		bound[id] = b
	}
	for _, r := range ordered {
		b, present := bound[r.ID]
		if !present {
			continue
		}
		if b.Preview != nil {
			preview := b.Preview
			b.Preview = func(ctx context.Context) (NativePreview, error) {
				evidence, err := preview(ctx)
				if err != nil {
					return NativePreview{}, err
				}
				for _, effect := range evidence.Actions {
					if !localTargetSelected(r, effect.Target) || effect.Action != native.Unchanged && !permitsEffect(r, effect) {
						return NativePreview{}, errors.New("preview evidence is outside accepted native intent")
					}
				}
				return evidence, nil
			}
		}
		if journal != nil && b.Apply != nil {
			apply := b.Apply
			b.Apply = func(ctx context.Context, run RunNativeEffect) error {
				return apply(ctx, func(effect NativeEffect, submit func(context.Context) error) error {
					if submit == nil {
						return run(effect, nil)
					}
					return run(effect, func(ctx context.Context) error {
						operationID = journal.ID()
						if err := journal.BeginEffect(ctx, r.ID, effect.Target, string(effect.Action)); err != nil {
							return err
						}
						// Revalidate after the durable intent: fsync can take time,
						// and observation is required if authority is now lost.
						if err := beforeEffect(ctx, plan.Revision, r, effect); err != nil {
							return err
						}
						if err := errors.Join(submit(ctx), ctx.Err()); err != nil {
							return err
						}
						return journal.MarkApplied()
					})
				})
			}
		}
		bound[r.ID] = b
	}
	result, err = engine.ExecuteNative(ctx, plan, NativeBindings{
		Resources: bound, CheckPolicy: f.CheckPolicy, BeforeEffect: beforeEffect,
		VerifyPlan: func(ctx context.Context, supplied NativePlan) error {
			if !sameLocalPlan(plan, supplied) {
				return errors.New("native selection changed after validation")
			}
			if err := authorize(ctx, permission, NativeResource{}, NativeEffect{}); err != nil {
				return err
			}
			return checkRevision(ctx)
		},
	}, opts)
	if result.Native != nil {
		result.Native.LocalOperationID = operationID
	}
	if journal != nil {
		err = errors.Join(err, journal.Finish(err == nil))
	}
	if opts.DryRun {
		if finalCheck := errors.Join(authorize(ctx, permission, NativeResource{}, NativeEffect{}), checkRevision(ctx)); finalCheck != nil {
			return Result{}, finalCheck
		}
	}
	return result, err
}

func detachLocalPlan(p NativePlan) NativePlan {
	p.Resources = slices.Clone(p.Resources)
	for i := range p.Resources {
		p.Resources[i] = detachedResource(p.Resources[i])
	}
	return p
}

func sameLocalPlan(a, b NativePlan) bool {
	return a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Revision == b.Revision &&
		slices.EqualFunc(a.Resources, b.Resources, func(a, b NativeResource) bool {
			return a.ID == b.ID && a.Kind == b.Kind && slices.Equal(a.DependsOn, b.DependsOn)
		})
}

func verifyLocalNativeSelection(c environment.Candidate, resources []NativeResource, bootstrapHerdr bool) error {
	if err := verifyLocalLifecycleSelection(c, resources, bootstrapHerdr); err != nil {
		return err
	}
	for _, r := range resources {
		if isLocalLifecycle(r.Kind) {
			continue
		}
		var id, prerequisite string
		switch r.Kind {
		case NativeGatewayImage:
			id = GatewayImageStage
		case NativeGatewayVolume:
			id, prerequisite = GatewayVolumeStage, GatewayImageStage
		case NativeGatewayInstance:
			id, prerequisite = GatewayInstanceStage, GatewayVolumeStage
		case NativeGatewayRuntime:
			id, prerequisite = GatewayRuntimeStage, GatewayInstanceStage
		}
		if c.Gateway == nil || r.ID != id || prerequisite != "" && !slices.Contains(r.DependsOn, prerequisite) {
			return errors.New("native gateway scope or required prerequisite missing from accepted selection")
		}
	}
	return nil
}

func localEffectSelected(c environment.Candidate, r NativeResource, e NativeEffect, bootstrapHerdr bool) bool {
	if !allowedNativeEffect(r.Kind, e.Action) || !localTargetSelected(r, e.Target) {
		return false
	}
	if isLocalLifecycle(r.Kind) {
		return localLifecycleEffectSelected(c, r, e, bootstrapHerdr)
	}
	if c.Gateway == nil {
		return false
	}
	switch r.Kind {
	case NativeGatewayImage, NativeGatewayInstance, NativeGatewayVolume:
		return true
	case NativeGatewayRuntime:
		return e.Action == NativeStop && !c.Gateway.EnsureRunning ||
			c.Gateway.EnsureRunning && (e.Action == NativeStart || e.Action == native.Update && e.Target == GatewayLiteLLMTarget)
	}
	return false
}

func localTargetSelected(r NativeResource, target string) bool {
	switch r.Kind {
	case NativeWorkload, NativeManagementNetwork, NativeHerdrVolume, NativeHerdrInstance:
		return r.ID == target
	case NativeGatewayVolume:
		return target == GatewayDatabaseTarget
	case NativeGatewayImage:
		return target == GatewayPostgresTarget || target == GatewayLiteLLMTarget ||
			target == GatewayPostgresAliasTarget || target == GatewayLiteLLMAliasTarget
	case NativeGatewayInstance, NativeGatewayRuntime:
		return target == GatewayPostgresTarget || target == GatewayLiteLLMTarget
	}
	return false
}
