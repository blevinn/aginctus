package orchestration

import (
	"context"
	"errors"
	"slices"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	contract "github.com/blevinn/aginctus/pkg/orchestration"
)

const (
	managementNetworkStage = "management-network"
	herdrVolumeStage       = "herdr-system-state"
	herdrInstanceStage     = "herdr-client"
)

func isLocalLifecycle(kind NativeResourceKind) bool {
	switch kind {
	case NativeWorkload, NativeManagementNetwork, NativeHerdrVolume, NativeHerdrInstance:
		return true
	}
	return false
}

func localWorkload(c environment.Candidate, id string) (acceptedstate.WorkloadSpec, bool) {
	for _, w := range c.Workloads {
		if w.ID == id && w.Native != nil && len(w.Profiles) == 0 {
			return w, true
		}
	}
	return acceptedstate.WorkloadSpec{}, false
}

func verifyLocalLifecycleSelection(c environment.Candidate, resources []NativeResource, bootstrap bool) error {
	var bridge, volume, herdr bool
	var network string
	selectNetwork := func(name string) bool {
		if network == "" {
			network = name
		}
		return name != "" && network == name
	}
	for _, r := range resources {
		switch r.Kind {
		case NativeManagementNetwork:
			if r.ID != managementNetworkStage {
				return errors.New("noncanonical management capability stage")
			}
			bridge = true
		case NativeWorkload:
			switch r.ID {
			case managementNetworkStage, herdrVolumeStage, herdrInstanceStage, GatewayImageStage, GatewayVolumeStage, GatewayInstanceStage, GatewayRuntimeStage:
				return errors.New("reserved native workload ID")
			}
			w, ok := localWorkload(c, r.ID)
			if !ok || !slices.Contains(r.DependsOn, managementNetworkStage) || !selectNetwork(w.Native.ManagementNetwork) {
				return errors.New("workload requires accepted runtime and attachments, explicit empty profiles and the pinned bridge prerequisite")
			}
		case NativeHerdrVolume:
			if c.Herdr == nil || r.ID != herdrVolumeStage || !slices.Contains(r.DependsOn, managementNetworkStage) || !selectNetwork(c.Herdr.ManagementNetwork) {
				return errors.New("missing accepted Herdr state intent or pinned bridge prerequisite")
			}
			volume = true
		case NativeHerdrInstance:
			if c.Herdr == nil || r.ID != herdrInstanceStage || !slices.Contains(r.DependsOn, herdrVolumeStage) {
				return errors.New("missing accepted Herdr instance intent or retained state prerequisite")
			}
			herdr = true
		}
	}
	if volume != herdr || bridge != (network != "") || bootstrap && (!herdr || c.Herdr == nil || !c.Herdr.AllowStateBootstrap) {
		return errors.New("missing or orphaned lifecycle stage or unaccepted Herdr bootstrap")
	}
	return nil
}

func localLifecycleEffectSelected(c environment.Candidate, r NativeResource, e NativeEffect, bootstrap bool) bool {
	switch r.Kind {
	case NativeWorkload:
		w, ok := localWorkload(c, r.ID)
		return ok && (e.Action == native.Create || e.Action == native.Update || e.Action == NativeStart && w.EnsureRunning)
	case NativeHerdrVolume:
		return c.Herdr != nil && c.Herdr.AllowStateBootstrap && bootstrap && e.Action == native.Create
	case NativeHerdrInstance:
		return c.Herdr != nil && (e.Action == native.Create || e.Action == native.Update || e.Action == NativeStart)
	}
	return false
}

func localDomainPlan(plan NativePlan, lifecycle bool) NativePlan {
	plan = detachLocalPlan(plan)
	plan.Resources = slices.DeleteFunc(plan.Resources, func(r NativeResource) bool { return isLocalLifecycle(r.Kind) != lifecycle })
	// Required cross-stack edges stay on the final plan and must be
	// reviewed/authorized by the lead, not inferred by the domain builder.
	for i := range plan.Resources {
		r := &plan.Resources[i]
		r.DependsOn = slices.DeleteFunc(r.DependsOn, func(id string) bool {
			return !slices.ContainsFunc(plan.Resources, func(p NativeResource) bool { return p.ID == id })
		})
	}
	return plan
}

func (f LocalNativeExecution) bindLocalDomains(data []byte, plan NativePlan) (map[string]NativeResourceBinding, error) {
	bindings := map[string]NativeResourceBinding{}
	for _, lifecycle := range []bool{true, false} {
		fragment := localDomainPlan(plan, lifecycle)
		if len(fragment.Resources) == 0 {
			continue
		}
		candidate, err := environment.ParseCandidate(data)
		if err != nil {
			return nil, err
		}
		var supplied map[string]NativeResourceBinding
		if lifecycle {
			if f.BindLifecycle == nil {
				return nil, errors.New("pinned lifecycle composer required")
			}
			fragment.Resources, err = fragment.ordered()
			if err != nil {
				return nil, err
			}
			var built NativePlan
			built, supplied, err = f.BindLifecycle(candidate, detachLocalPlan(fragment), f.BootstrapHerdr)
			if err == nil {
				built.Resources, err = built.ordered()
			}
			if err == nil && !sameLocalPlan(fragment, built) {
				return nil, errors.New("composed lifecycle graph differs from authorized selection")
			}
		} else {
			if f.Bind == nil {
				return nil, errors.New("pinned gateway composer required")
			}
			supplied, err = f.Bind(candidate, detachLocalPlan(fragment))
		}
		if err != nil {
			return nil, err
		}
		if len(supplied) != len(fragment.Resources) {
			return nil, errors.New("incomplete or excess domain bindings")
		}
		for _, r := range fragment.Resources {
			b, ok := supplied[r.ID]
			if !ok {
				return nil, errors.New("binding outside authorized domain scope")
			}
			if lifecycle {
				for _, checks := range [][]BoundCheck{b.Conditions.Preconditions, b.Conditions.Postconditions} {
					for _, check := range checks {
						if check.Check.Effect != contract.ReadOnly && check.Check.Effect != contract.Offline {
							return nil, errors.New("native lifecycle checks must only observe")
						}
					}
				}
			}
			if r.Kind == NativeManagementNetwork {
				// The host stage is observe-only. Do not invoke a caller's Apply,
				// even if it was accidentally assembled as a host writer.
				b.Apply = func(ctx context.Context, _ RunNativeEffect) error { return ctx.Err() }
			}
			bindings[r.ID] = b
		}
	}
	return bindings, nil
}
