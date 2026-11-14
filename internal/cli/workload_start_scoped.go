package cli

import (
	"context"
	"fmt"
	"io"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

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
