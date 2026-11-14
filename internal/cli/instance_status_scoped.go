package cli

import (
	"context"
	"io"
	"os/exec"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Local project enumerations stay injectable for CLI-level fail-closed tests.
var inspectScopedInstances = func(ctx context.Context, scope incusscope.ProjectScope) ([]byte, error) {
	return exec.CommandContext(ctx, "incus", scopedInstanceListArgs(scope)...).Output()
}
func scopedInstanceListArgs(scope incusscope.ProjectScope) []string {
	return workloadops.ScopedInstanceListArgs(scope)
}
func parseScopedInstanceStatusArgs(args []string) (string, []string, error) {
	return workloadops.ParseScopedInstanceStatusArgs(args)
}
func readScopedManagedInstances(ctx context.Context, cfg *config.Config, name string) (incusscope.ProjectScope, []inspectedInstance, error) {
	return workloadops.ReadScopedManagedInstances(ctx, cfg, name, verifyScopedImageProject, inspectScopedInstances, maxProjectListBytes)
}
func findScopedManagedInstance(instances []inspectedInstance, name, resource, role, id string) (string, error) {
	return workloadops.FindScopedManagedInstance(instances, name, resource, role, id)
}
func scopedStatusActions() workloadops.ScopedStatusActions {
	return workloadops.ScopedStatusActions{
		Parse: parseScopedInstanceStatusArgs,
		Read: readScopedManagedInstances,
		Find: findScopedManagedInstance,
	}
}
func runScopedHerdrClientStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunScopedHerdrStatus(ctx, args, out, stderr, cfg, scopedStatusActions())
}
func runScopedWorkloadStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, listOnly bool) int {
	return workloadops.RunScopedWorkloadStatus(ctx, args, out, stderr, cfg, listOnly, scopedStatusActions())
}
