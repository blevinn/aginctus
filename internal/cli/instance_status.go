package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
)

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
