package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/config"
)

var inspectGatewayProjects = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", "project", "list", "--format=json")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus project list: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return data, nil
}

var inspectGatewayInstances = func(ctx context.Context, project string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", "list", "--project", project, "--format=json")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus list gateway instances: %w: %s", err, strings.TrimSpace(string(data)))
	}
	return data, nil
}

type statusProject = gatewayops.StatusProject
type gatewayStatusInstance = gatewayops.StatusInstance

func gatewayStatusService(name string) bool { return gatewayops.StatusService(name) }

func runGatewayStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return gatewayops.Status(ctx, args, out, stderr, cfg, inspectGatewayProjects, inspectGatewayInstances)
}
