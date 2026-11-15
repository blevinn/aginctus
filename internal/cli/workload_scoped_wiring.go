package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
)

// Exact-project native apply, preview and start/stop wiring retain explicit guards and native SDK injection.

// workload_apply_scoped.go — retained CLI adapter surface.
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

// workload_apply_scoped_execute.go — retained CLI adapter surface.
var ensureScopedExistingWorkload = native.EnsureExistingLocalInstance

func runScopedWorkloadApplyConfirm(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedApplyConfirm(ctx, args, out, stderr, cfg, scopedWorkloadActions())
}
func runScopedWorkloadApply(ctx context.Context, project, id string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedApply(ctx, project, id, out, stderr, cfg, scopedWorkloadActions())
}

// workload_start_scoped.go — retained CLI adapter surface.
// Native transition still uses the Incus SDK and never the ambient CLI project.
var transitionScopedWorkloadState = func(ctx context.Context, intent native.InstanceLifecycleIntent) error {
	_, err := native.EnsureLocalInstanceState(ctx, intent)
	return err
}

// A manual transition must not race known installed system/current-user
// Aginctus reconcilers. This is only a conservative denial gate: an absent
// allowlist is NOT evidence of exclusive control over Incus.
func requireNoKnownLifecycleUnits(ctx context.Context) error {
	for _, unit := range operatorLifecycleUnits {
		for _, manager := range []string{"system", "user"} {
			if inspectOperatorUnitEvidence(ctx, manager, unit) != unitAbsent {
				return fmt.Errorf("known lifecycle authority is present or unverified")
			}
		}
	}
	return nil
}

func runScopedWorkloadStart(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedWorkloadStart(ctx, args, out, stderr, cfg, scopedWorkloadActions())
}
func runScopedWorkloadStop(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedWorkloadStop(ctx, args, out, stderr, cfg, scopedWorkloadActions())
}
func runScopedWorkloadTransition(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, action string, target native.InstanceState) int {
	return workloadops.RunScopedWorkloadTransition(ctx, args, out, stderr, cfg, action, target, scopedWorkloadActions())
}
