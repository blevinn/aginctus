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

// The key travels only over stdin to the fixed helper in the default project.
// Project extensibility requires a separate audited change to both boundaries.
var provisionGatewayGuest = func(ctx context.Context, instance string, payload []byte) error {
	cmd := exec.CommandContext(ctx, "incus", "exec", instance, "--project", "default", "--", "/run/current-system/sw/bin/aginctus-opencode-gateway-provision")
	cmd.Stdin = strings.NewReader(string(payload))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("guest provisioning failed (verify image and permissions)")
	}
	return nil
}
type gatewayDiscoveryInstance = workloadops.GatewayDiscoveryInstance

func discoverGatewayIPv4(data []byte, project, network string) (string, error) {
	return workloadops.DiscoverGatewayIPv4(data, project, network, gatewayStatusService)
}
func readWorkloadKey(path string) (string, error) { return workloadops.ReadWorkloadKey(path) }

// A failed inventory and a foreign/stopped target retain distinct errors.
func checkRunningGatewayWorkload(ctx context.Context, name, id string) (bool, error) {
	instances, err := statusInstances(ctx)
	if err != nil { return false, err }
	if err := requireRunningOwnedInstance(instances, name, "workload", "agent-workload", id); err != nil {
		return false, nil
	}
	return true, nil
}

func runWorkloadGatewayProvision(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return workloadops.RunGatewayProvision(ctx, args, stdout, stderr, cfg, workloadops.GatewayProvisionActions{
		CheckWorkload: checkRunningGatewayWorkload,
		InspectProjects: inspectGatewayProjects,
		InspectInstances: inspectGatewayInstances,
		Service: gatewayStatusService,
		Provision: provisionGatewayGuest,
	})
}
