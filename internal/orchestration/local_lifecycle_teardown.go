package orchestration

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// LocalLifecycleTeardown is a nonserialized, same-UID fixture selection. Each
// incarnation comes from independently verified live evidence. It is not a
// grant, does not override accepted identities, and cannot stop then delete.
type LocalLifecycleTeardown struct {
	Operation  native.ProjectInstanceOperation
	Instances  map[string]time.Time
	HerdrState time.Time
}

type LocalLifecycleTeardownRequest struct {
	LocalNativeAuthorityRequest
	Selection LocalLifecycleTeardown
}

// ExecuteLocalTeardown shares the accepted pin and durable effect boundary with
// reconcile, but requires its own explicitly bound actor policy and composer.
func (f LocalNativeExecution) ExecuteLocalTeardown(ctx context.Context, engine *Engine, plan NativePlan, selection LocalLifecycleTeardown, opts ExecuteOptions) (Result, error) {
	if f.AuthorizeTeardown == nil || f.BindLifecycleTeardown == nil || f.BootstrapHerdr {
		return Result{}, errors.New("explicit teardown policy and composition required; bootstrap is incompatible")
	}
	selection.Instances = maps.Clone(selection.Instances)
	return f.executeLocalNative(ctx, engine, plan, opts, &selection)
}

func (s LocalLifecycleTeardown) detached() LocalLifecycleTeardown {
	s.Instances = maps.Clone(s.Instances)
	return s
}

func (s LocalLifecycleTeardown) action() native.Action {
	if s.Operation == native.StopInstance {
		return native.Stop
	}
	if s.Operation == native.RemoveInstance {
		return native.Delete
	}
	return ""
}

func (s LocalLifecycleTeardown) validate(plan NativePlan) error {
	if s.action() == "" || len(s.Instances) == 0 {
		return errors.New("explicit stop or removal with live instance pins required")
	}
	var volume, herdr bool
	instances := 0
	for _, r := range plan.Resources {
		switch r.Kind {
		case NativeWorkload:
			if len(r.DependsOn) != 0 || slices.Contains([]string{managementNetworkStage, herdrVolumeStage, herdrInstanceStage, GatewayImageStage, GatewayVolumeStage, GatewayInstanceStage, GatewayRuntimeStage}, r.ID) {
				return errors.New("invalid teardown workload stage")
			}
		case NativeHerdrInstance:
			if r.ID != herdrInstanceStage || !slices.Equal(r.DependsOn, []string{herdrVolumeStage}) {
				return errors.New("teardown requires the retained Herdr state prerequisite")
			}
			herdr = true
		case NativeHerdrVolume:
			if r.ID != herdrVolumeStage || len(r.DependsOn) != 0 {
				return errors.New("invalid retained volume stage")
			}
			volume = true
			continue
		default:
			return errors.New("teardown selects only workload and Herdr instances with retained Herdr state")
		}
		instances++
		if s.Instances[r.ID].IsZero() {
			return errors.New("each teardown instance requires an explicitly pinned incarnation")
		}
	}
	if instances != len(s.Instances) || volume != herdr || herdr != !s.HerdrState.IsZero() {
		return errors.New("teardown pins and retained state must match the exact selection")
	}
	return nil
}

func (s LocalLifecycleTeardown) validateAccepted(c environment.Candidate, plan NativePlan) error {
	for _, r := range plan.Resources {
		if r.Kind == NativeWorkload {
			if _, ok := localWorkload(c, r.ID); !ok {
				return errors.New("teardown target has no accepted native workload intent")
			}
		}
	}
	if !s.HerdrState.IsZero() && c.Herdr == nil {
		return errors.New("teardown target has no accepted Herdr intent")
	}
	return nil
}

func (s LocalLifecycleTeardown) permits(r NativeResource, e NativeEffect) bool {
	return (r.Kind == NativeWorkload || r.Kind == NativeHerdrInstance) && e.Target == r.ID && !s.Instances[r.ID].IsZero() && e.Action == s.action()
}
