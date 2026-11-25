package nativelifecycle

import (
	"errors"

	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/blevinn/aginctus/internal/workload"
)

// LocalComposer supplies the independently provisioned capabilities for
// LocalNativeExecution.BindLifecycle. The fixture authorizer, pin, and journal
// are owned by LocalNativeExecution; this value cannot authorize an effect.
type LocalComposer struct {
	Environment       environment.Descriptor
	Bridge            ManagementNetwork
	TrustedServerPin  string
	Client            native.ProjectInstanceClient
	VerifyAttachments AttachmentVerifier
}

// BindLifecycle derives all workload and Herdr intent from the detached
// accepted body. The requested fragment selects logical stages only. Return
// Build's graph unchanged so the authority can compare the actual edges with
// the authorized graph. No SDK reads occur during this projection.
func (c LocalComposer) BindLifecycle(accepted environment.Candidate, requested orchestration.NativePlan, bootstrapHerdr bool) (orchestration.NativePlan, map[string]orchestration.NativeResourceBinding, error) {
	return c.bindLocalLifecycle(accepted, requested, bootstrapHerdr, orchestration.LocalLifecycleTeardown{})
}

// BindTeardown projects the same accepted identities and binds independent
// live incarnation pins to each instance and the retained Herdr disk.
func (c LocalComposer) BindTeardown(accepted environment.Candidate, requested orchestration.NativePlan, selection orchestration.LocalLifecycleTeardown) (orchestration.NativePlan, map[string]orchestration.NativeResourceBinding, error) {
	if selection.Operation != native.StopInstance && selection.Operation != native.RemoveInstance {
		return orchestration.NativePlan{}, nil, errors.New("explicit stop or removal required")
	}
	return c.bindLocalLifecycle(accepted, requested, false, selection)
}

func (c LocalComposer) bindLocalLifecycle(accepted environment.Candidate, requested orchestration.NativePlan, bootstrapHerdr bool, teardown orchestration.LocalLifecycleTeardown) (orchestration.NativePlan, map[string]orchestration.NativeResourceBinding, error) {
	if err := requested.Validate(); err != nil {
		return orchestration.NativePlan{}, nil, err
	}
	if !accepted.Environment.SameEnvironment(c.Environment) || requested.Revision.ValidateAgainst(accepted) != nil {
		return orchestration.NativePlan{}, nil, environment.ErrAcceptedConflict
	}
	byID := make(map[string]acceptedstate.WorkloadSpec, len(accepted.Workloads))
	for _, w := range accepted.Workloads {
		byID[w.ID] = w
	}
	selection := Selection{Bridge: &c.Bridge}
	op := native.ReconcileInstance
	if teardown.Operation != "" {
		op = teardown.Operation
		selection.Bridge = nil
	}
	for _, r := range requested.Resources {
		switch r.Kind {
		case orchestration.NativeManagementNetwork, orchestration.NativeHerdrVolume:
			// Build derives prerequisites from the pinned instance selections.
		case orchestration.NativeWorkload:
			w, ok := byID[r.ID]
			if !ok || w.Native == nil || len(w.Profiles) != 0 {
				return orchestration.NativePlan{}, nil, errors.New("selected workload requires pinned native intent and empty profiles")
			}
			if teardown.Operation != "" && teardown.Instances[r.ID].IsZero() {
				return orchestration.NativePlan{}, nil, errors.New("selected workload lacks an authorized live incarnation")
			}
			isolation := w.InstanceType
			if isolation == "virtual-machine" {
				isolation = "vm"
			}
			selection.Workloads = append(selection.Workloads, Workload{
				Spec: workload.Spec{ID: w.ID, Name: w.InstanceName, Isolation: isolation,
					Runtime: w.Native.Runtime, StoragePool: w.Native.StoragePool, ManagementNetwork: w.Native.ManagementNetwork},
				Operation: op,
			})
		case orchestration.NativeHerdrInstance:
			h := accepted.Herdr
			if h == nil || bootstrapHerdr && !h.AllowStateBootstrap {
				return orchestration.NativePlan{}, nil, errors.New("selected Herdr stage or bootstrap lacks pinned intent")
			}
			if teardown.Operation != "" && (teardown.Instances[r.ID].IsZero() || teardown.HerdrState.IsZero()) {
				return orchestration.NativePlan{}, nil, errors.New("selected Herdr instance or retained state lacks an authorized live incarnation")
			}
			selection.Herdr = &Herdr{
				Spec:      herdrclient.Spec{Name: h.InstanceName, StoragePool: h.StoragePool, ManagementNetwork: h.ManagementNetwork},
				Operation: op, ImageFingerprint: h.ImageFingerprint, CreateState: bootstrapHerdr,
			}
		default:
			return orchestration.NativePlan{}, nil, errors.New("unsupported stage in local lifecycle fragment")
		}
	}
	if bootstrapHerdr && selection.Herdr == nil {
		return orchestration.NativePlan{}, nil, errors.New("Herdr bootstrap requires the selected Herdr instance")
	}
	return build(c.Environment, requested.Revision, accepted, selection, c.TrustedServerPin, c.Client, c.VerifyAttachments, teardown)
}
