package cli

import (
	"context"
	"io"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

type scopedWorkloadApplyPreview = workloadops.ScopedApplyPreview
type scopedWorkloadApplyDiff = workloadops.ScopedApplyDiff

// Preserve the existing injected planner for CLI and security regression tests.
var planScopedExistingWorkloadDiff = native.PlanLocalExistingInstance

func classifyScopedWorkloadDiff(plan native.InstancePlan, identity native.InstanceIdentity) (string, int, error) {
	return workloadops.ClassifyScopedDiff(plan, identity)
}

func observeScopedWorkload(ctx context.Context, cfg *config.Config, project, name, resource, role, id string) workloadops.ScopedObservation {
	_, instances, err := readScopedManagedInstances(ctx, cfg, project)
	if err != nil { return workloadops.ScopedObservation{InventoryErr: err} }
	state, err := findScopedManagedInstance(instances, name, resource, role, id)
	return workloadops.ScopedObservation{State: state, LookupErr: err}
}

func scopedWorkloadActions() workloadops.ScopedActions {
	return workloadops.ScopedActions{
		Parse: parseScopedInstanceStatusArgs,
		Observe: observeScopedWorkload,
		Lease: acquireScopedImageMutationLease,
		GuardControllers: requireNoKnownLifecycleUnits,
		Plan: planScopedExistingWorkloadDiff,
		Ensure: ensureScopedExistingWorkload,
		Transition: transitionScopedWorkloadState,
	}
}

func runScopedWorkloadApplyRender(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedApplyRender(ctx, args, stdout, stderr, cfg, scopedWorkloadActions())
}
