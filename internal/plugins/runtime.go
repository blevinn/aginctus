package plugins

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	collection "github.com/blevinn/aginctus/internal/observability"
	engine "github.com/blevinn/aginctus/internal/orchestration"
	contract "github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/blevinn/aginctus/pkg/plugin"
)

// ContainerInspection is a trusted host boundary. It must resolve the logical key
// and local container into an exact authorized backend resource and independently
// recheck principal/project/ownership/read authority on EVERY call. Neither a
// graph key nor engine check authorization grants infrastructure access.
type ContainerInspection func(context.Context, TargetKey, string) (contract.ContainerObservation, error)

// BindCheck adapts a selected pre/postcondition to the existing engine. The host
// supplies assembled declarations, dependency-first ordering, complete bindings,
// preflight selection and applicable incarnation/revision expectations. Binding
// performs no I/O. This initial service family supports offline computation and
// Incus lifecycle inspection, not guest/network/credential/model/tool services.
func (r *Registry) BindCheck(target ScopedTarget, checkID string, inspect ContainerInspection) (engine.BoundCheck, error) {
	if !localName.MatchString(target.Key.Deployment) || !localName.MatchString(target.Key.Instance) || target.Key.Target != target.Definition.ID {
		return engine.BoundCheck{}, fmt.Errorf("invalid runtime target scope")
	}
	if err := target.Definition.Validate(); err != nil {
		return engine.BoundCheck{}, err
	}
	entry, ok := r.entries[target.Plugin]
	if !ok {
		return engine.BoundCheck{}, fmt.Errorf("runtime target references an unregistered plugin")
	}
	evaluator, ok := entry.implementation.(plugin.CheckEvaluator)
	if !ok {
		return engine.BoundCheck{}, fmt.Errorf("plugin does not implement runtime checks")
	}
	if !slices.ContainsFunc(entry.descriptor.Capabilities, func(c plugin.Capability) bool { return c.Name == target.Definition.Capability }) {
		return engine.BoundCheck{}, fmt.Errorf("runtime target references an unadvertised capability")
	}
	definition := cloneTarget(target.Definition)
	var selected *contract.Check
	for _, checks := range [][]contract.Check{definition.Preconditions, definition.Postconditions} {
		for _, check := range checks {
			if check.ID == checkID {
				selected = &check
			}
		}
	}
	if selected == nil {
		return engine.BoundCheck{}, fmt.Errorf("runtime check is not a declared pre/postcondition")
	}
	for _, access := range selected.Access {
		if access != contract.IncusAccess || inspect == nil {
			return engine.BoundCheck{}, fmt.Errorf("runtime check requires an unavailable service")
		}
	}
	check := *selected
	bound := engine.BoundCheck{Check: check}
	// Keep the evaluator's declaration independent from the returned binding.
	bound.Check.Access, bound.Check.OptIns = slices.Clone(check.Access), slices.Clone(check.OptIns)
	bound.Evaluate = func(ctx context.Context) (contract.Finding, error) {
		if err := runtimeContextErr(ctx); err != nil {
			return contract.Finding{}, err
		}
		invocation, cancel := context.WithCancel(ctx)
		defer cancel()
		reader := &containerReader{ctx: invocation, key: target.Key, target: definition, check: check, inspect: inspect}
		services := plugin.CheckServices{Progress: collection.Progress(invocation)}
		if slices.Contains(check.Access, contract.IncusAccess) {
			services.Containers = reader
		}
		request := plugin.CheckRequest{Target: cloneTarget(definition), Check: check}
		request.Check.Access, request.Check.OptIns = slices.Clone(check.Access), slices.Clone(check.OptIns)
		finding, err := evaluator.EvaluateCheck(invocation, request, services)
		return finding, errors.Join(err, reader.failure(), runtimeContextErr(invocation))
	}
	return bound, nil
}

type containerReader struct {
	ctx     context.Context
	key     TargetKey
	target  contract.Target
	check   contract.Check
	inspect ContainerInspection
	mu      sync.Mutex
	err     error
}

func (r *containerReader) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *containerReader) Inspect(id string) (observation contract.ContainerObservation, err error) {
	defer func() {
		if err != nil {
			// A plugin cannot turn an ignored service failure into passed evidence.
			observation = contract.ContainerObservation{}
			r.mu.Lock()
			if r.err == nil {
				r.err = err
			}
			r.mu.Unlock()
		}
	}()
	if err = runtimeContextErr(r.ctx); err != nil {
		return observation, err
	}
	if !slices.ContainsFunc(r.target.Containers, func(c contract.Container) bool { return c.ID == id }) {
		return observation, fmt.Errorf("container is outside the bound target")
	}
	started := time.Now()
	observation, err = r.inspect(r.ctx, r.key, id)
	if err = errors.Join(err, runtimeContextErr(r.ctx)); err != nil {
		return observation, err
	}
	if err = observation.Validate(); err != nil {
		return observation, err
	}
	now := time.Now()
	if observation.ObservedAt.After(now) || (r.check.MaxAge == 0 && observation.ObservedAt.Before(started)) ||
		(r.check.MaxAge > 0 && now.Sub(observation.ObservedAt) > r.check.MaxAge) {
		return observation, fmt.Errorf("container observation is stale or future-dated")
	}
	return observation, nil
}

func runtimeContextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
