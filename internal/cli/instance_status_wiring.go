package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
)

// Legacy all-projects status and exact-project status stay as distinct adapters with independent read scopes.

// instance_status.go — retained CLI adapter surface.
// The compatibility boundary intentionally observes the ambient daemon's
// all-projects inventory; ownership checks still reject non-default projects.
var inspectIncusInstances = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", "list", "--all-projects", "--format=json")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus list: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return data, nil
}
type inspectedInstance = workloadops.InspectedInstance
func findManagedInstance(instances []inspectedInstance, name, resource, role, id string) (string, error) {
	return workloadops.FindManagedInstance(instances, name, resource, role, id)
}
func statusInstances(ctx context.Context) ([]inspectedInstance, error) {
	return workloadops.StatusInstances(ctx, inspectIncusInstances)
}
func legacyStatusActions() workloadops.LegacyStatusActions {
	return workloadops.LegacyStatusActions{Inspect: statusInstances, Find: findManagedInstance}
}
func runHerdrClientStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunHerdrClientStatus(ctx, args, out, stderr, cfg, legacyStatusActions())
}
func runWorkloadStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunWorkloadStatus(ctx, args, out, stderr, cfg, legacyStatusActions())
}
func runActiveWorkloadList(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunActiveWorkloadList(ctx, args, out, stderr, cfg, legacyStatusActions())
}

// instance_status_scoped.go — retained CLI adapter surface.
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
