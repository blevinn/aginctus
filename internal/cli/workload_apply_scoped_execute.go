package cli

import (
	"context"
	"io"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

var ensureScopedExistingWorkload = native.EnsureExistingLocalInstance

func runScopedWorkloadApplyConfirm(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedApplyConfirm(ctx, args, out, stderr, cfg, scopedWorkloadActions())
}
func runScopedWorkloadApply(ctx context.Context, project, id string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedApply(ctx, project, id, out, stderr, cfg, scopedWorkloadActions())
}
