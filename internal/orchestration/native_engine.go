package orchestration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

type NativeAction = native.Action

const (
	NativeStart = native.Start
	NativeStop  = native.Stop
)

// NativeEffect identifies one action on an exact domain-resolved target.
// A composite resource (e.g. a gateway pair) must distinguish each target so a
// grant for one member cannot be reused for another. Target is a safe logical
// ID resolved by trusted composition, not a capability or arbitrary SDK arg.
type NativeEffect struct {
	Action NativeAction `json:"action"`
	Target string       `json:"target"`
}

// NativePreview is current, read-only plan evidence. Deferred means part of
// planning requires a dependency's effect and has not yet been verified.
type NativePreview struct {
	Actions  []NativeEffect `json:"actions"`
	Deferred bool           `json:"deferred"`
}

type NativePreviewResult struct {
	ResourceID string         `json:"resourceId"`
	Phase      ConditionPhase `json:"phase"`
	NativePreview
}

type NativeEffectOutcome string

const (
	NativeEffectBlocked   NativeEffectOutcome = "blocked"
	NativeEffectApplied   NativeEffectOutcome = "applied"
	NativeEffectUncertain NativeEffectOutcome = "uncertain"
)

type NativeEffectResult struct {
	ResourceID string              `json:"resourceId"`
	Effect     NativeEffect        `json:"effect"`
	Outcome    NativeEffectOutcome `json:"outcome"`
}

type NativeResult struct {
	Revision environment.AcceptedRevision `json:"revision"`
	LocalOperationID string `json:"localOperationId,omitempty"`
	Previews []NativePreviewResult        `json:"previews"`
	Effects  []NativeEffectResult         `json:"effects"`
}

// RunNativeEffect is a synchronous, non-reentrant, invocation-local capability.
// It wraps exactly one mutation and its operation wait. An error after invoking
// the mutation is conservatively uncertain; callers must re-observe to recover.
type RunNativeEffect func(NativeEffect, func(context.Context) error) error

// NativeResourceBinding is trusted composition, not plan input. Validate must
// be pure, Preview must only observe (globally and again before Apply), and every write in Apply must use the
// supplied capability. Domain-specific preflight and submission-time ownership,
// preconditions and ETag checks remain mandatory. Return only safe errors.
type NativeResourceBinding struct {
	Validate   func() error
	Preview    func(context.Context) (NativePreview, error)
	Apply      func(context.Context, RunNativeEffect) error
	Conditions StepConditions
}

// NativeBindings connects the exact selection to independent authenticated
// authority. VerifyPlan verifies the whole graph against pinned intent, including
// selected-only membership and domain-required edges. BeforeEffect checks the
// revision, principal, scope and live writer exclusion at each submission. The
// host holds the writer fence for the entire apply. Neither check is a fallback
// for the other, and neither is supplied by a serialized NativePlan.
type NativeBindings struct {
	Resources    map[string]NativeResourceBinding
	VerifyPlan   func(context.Context, NativePlan) error
	BeforeEffect func(context.Context, environment.AcceptedRevision, NativeResource, NativeEffect) error
	CheckPolicy  CheckPolicy
}

// ExecuteNative uses the same engine phases, condition evaluation and reporting
// as Execute. No legacy driver, ambient client or writer is resolved by a plan.
func (e *Engine) ExecuteNative(ctx context.Context, plan NativePlan, bindings NativeBindings, options ExecuteOptions) (Result, error) {
	if err := conditionContextErr(ctx); err != nil {
		return Result{}, err
	}
	ordered, err := plan.ordered()
	if err != nil {
		return Result{}, err
	}
	projection := Plan{APIVersion: APIVersion, Kind: Kind, Metadata: Metadata{Name: "native-resources"}}
	conditions := make(map[string]StepConditions, len(ordered))
	resources := make(map[string]NativeResource, len(ordered))
	bound := make(map[string]NativeResourceBinding, len(ordered))
	for _, resource := range ordered {
		b, ok := bindings.Resources[resource.ID]
		if !ok || b.Validate == nil || b.Preview == nil || !options.DryRun && b.Apply == nil {
			return Result{}, fmt.Errorf("resource %q has an incomplete native binding", resource.ID)
		}
		c := b.Conditions
		if c.Scope == (TargetScope{}) {
			c.Scope = TargetScope{Deployment: plan.Revision.EnvironmentID, Instance: resource.ID, Target: resource.ID}
		}
		if c.Scope.Deployment != plan.Revision.EnvironmentID {
			return Result{}, fmt.Errorf("resource %q condition scope differs from its environment", resource.ID)
		}
		c.Preconditions = cloneBoundChecks(c.Preconditions)
		c.Postconditions = cloneBoundChecks(c.Postconditions)
		for _, checks := range [][]BoundCheck{c.Preconditions, c.Postconditions} {
			for i := range checks {
				if checks[i].AcceptedRevision != "" && checks[i].AcceptedRevision != plan.Revision.ID {
					return Result{}, fmt.Errorf("resource %q condition is bound to a different revision", resource.ID)
				}
				checks[i].AcceptedRevision = plan.Revision.ID
			}
		}
		projection.Steps = append(projection.Steps, Step{ID: resource.ID, Driver: string(resource.Kind)})
		conditions[resource.ID], resources[resource.ID], bound[resource.ID] = c, resource, b
	}
	if len(bound) != len(bindings.Resources) || bindings.VerifyPlan == nil || !options.DryRun && bindings.BeforeEffect == nil {
		return Result{}, errors.New("native plan requires exact resource bindings and independent authority")
	}
	for _, resource := range ordered {
		for _, prerequisite := range resource.DependsOn {
			if !slices.ContainsFunc(conditions[prerequisite].Postconditions, func(c BoundCheck) bool { return c.Check.Required }) {
				return Result{}, fmt.Errorf("dependency %q requires a bound required postcondition", prerequisite)
			}
		}
	}
	// Native conditions are complete and per invocation. No driver or optional
	// legacy binding on the Engine receiver is inherited into this projection.
	if e.conditions != nil {
		return Result{}, errors.New("native execution requires explicit native condition bindings")
	}
	engine := (&Engine{}).WithConditions(conditions, bindings.CheckPolicy)
	nativeResult := &NativeResult{Revision: plan.Revision}
	preview := func(ctx context.Context, id string, phase ConditionPhase) (NativePreview, error) {
		if err := conditionContextErr(ctx); err != nil {
			return NativePreview{}, err
		}
		r := resources[id]
		p, err := bound[id].Preview(ctx)
		if err := errors.Join(err, conditionContextErr(ctx)); err != nil {
			return NativePreview{}, fmt.Errorf("observe native resource %q: %w", id, err)
		}
		if err := validateNativePreview(r, p); err != nil {
			return NativePreview{}, fmt.Errorf("preview native resource %q: %w", id, err)
		}
		p.Actions = slices.Clone(p.Actions)
		nativeResult.Previews = append(nativeResult.Previews, NativePreviewResult{ResourceID: id, Phase: phase, NativePreview: p})
		if phase == PreconditionPhase && p.Deferred {
			return NativePreview{}, fmt.Errorf("resource %q still has unverified dependency-produced prerequisites", id)
		}
		return p, nil
	}
	return engine.execute(ctx, projection, options, Result{Native: nativeResult}, executionHooks{
		native: true,
		validate: func(ctx context.Context) error {
			if err := engine.validateConditions(projection, options, true); err != nil {
				return err
			}
			for _, resource := range ordered {
				if err := conditionContextErr(ctx); err != nil {
					return err
				}
				if err := errors.Join(bound[resource.ID].Validate(), conditionContextErr(ctx)); err != nil {
					return fmt.Errorf("validate native resource %q: %w", resource.ID, err)
				}
			}
			return nil
		},
		authorize: func(ctx context.Context) error {
			// Detach authority input from the execution graph.
			selection := plan
			selection.Resources = make([]NativeResource, len(ordered))
			for i, resource := range ordered {
				selection.Resources[i] = detachedResource(resource)
			}
			if err := errors.Join(bindings.VerifyPlan(ctx, selection), conditionContextErr(ctx)); err != nil {
				return fmt.Errorf("verify native plan selection: %w", err)
			}
			return nil
		},
		preflight: func(ctx context.Context, step Step) error {
			_, err := preview(ctx, step.ID, PreflightPhase)
			return err
		},
		operation: func(ctx context.Context, step Step, options ExecuteOptions) error {
			if options.DryRun {
				return nil
			}
			p, err := preview(ctx, step.ID, PreconditionPhase)
			if err != nil {
				return err
			}
			return applyNativeResource(ctx, plan.Revision, resources[step.ID], p.Actions, bound[step.ID], bindings, nativeResult)
		},
	})
}

func detachedResource(r NativeResource) NativeResource {
	r.DependsOn = slices.Clone(r.DependsOn)
	return r
}

func allowedNativeEffect(kind NativeResourceKind, action NativeAction) bool {
	switch kind {
	case NativeWorkload, NativeHerdrInstance:
		return action == native.Create || action == native.Update || action == NativeStart || action == NativeStop || action == native.Delete
	case NativeHerdrVolume, NativeGatewayImage, NativeGatewayVolume, NativeGatewayInstance:
		return action == native.Create
	case NativeGatewayRuntime:
		return action == native.Update || action == NativeStart || action == NativeStop
	default:
		return false
	}
}

func validateNativePreview(r NativeResource, preview NativePreview) error {
	if len(preview.Actions) > 16 || !preview.Deferred && len(preview.Actions) == 0 || preview.Deferred && len(r.DependsOn) == 0 {
		return errors.New("missing, excessive or inapplicably deferred plan evidence")
	}
	seen := make(map[NativeEffect]bool, len(preview.Actions))
	for _, action := range preview.Actions {
		if !scopeIdentifier.MatchString(action.Target) {
			return errors.New("native plan evidence requires an explicit effect target")
		}
		if seen[action] {
			return errors.New("duplicate native effect in preview")
		}
		seen[action] = true
		if action.Action == native.Unchanged && len(preview.Actions) == 1 && !preview.Deferred {
			continue
		}
		if !allowedNativeEffect(r.Kind, action.Action) {
			return errors.New("unsupported native change or immutable drift; inspect before retry")
		}
	}
	return nil
}

func applyNativeResource(ctx context.Context, revision environment.AcceptedRevision, r NativeResource, actions []NativeEffect, b NativeResourceBinding, bindings NativeBindings, result *NativeResult) error {
	var mu sync.Mutex
	var expired atomic.Bool
	var failure error
	run := func(effect NativeEffect, submit func(context.Context) error) error {
		if expired.Load() {
			return errors.New("native effect capability expired")
		}
		mu.Lock()
		defer mu.Unlock()
		if expired.Load() {
			return errors.New("native effect capability expired")
		}
		if failure != nil {
			return failure
		}
		i := slices.Index(actions, effect)
		if !allowedNativeEffect(r.Kind, effect.Action) || i < 0 || submit == nil {
			failure = errors.New("unsupported or unbound native effect")
			return failure
		}
		// A domain may skip a now-satisfied action but cannot submit effects
		// that the fresh preview did not include, repeat them, or reorder them.
		actions = actions[i+1:]
		outcome := NativeEffectResult{ResourceID: r.ID, Effect: effect, Outcome: NativeEffectBlocked}
		defer func() { result.Effects = append(result.Effects, outcome) }()
		failure = conditionContextErr(ctx)
		if failure == nil {
			failure = errors.Join(bindings.BeforeEffect(ctx, revision, detachedResource(r), effect), conditionContextErr(ctx))
		}
		if failure != nil {
			return failure
		}
		outcome.Outcome = NativeEffectUncertain
		failure = errors.Join(submit(ctx), conditionContextErr(ctx))
		if failure == nil {
			outcome.Outcome = NativeEffectApplied
		}
		return failure
	}
	applyErr := b.Apply(ctx, run)
	expired.Store(true)
	mu.Lock()
	defer mu.Unlock()
	return errors.Join(applyErr, failure, conditionContextErr(ctx))
}
