package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	"github.com/blevinn/aginctus/internal/config"
)

// The legacy read-only Incus query remains individually injectable.
var inspectIncusNetworks = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", "network", "list", "--format=json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus network list: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
type inspectedNetwork = networkops.InspectedNetwork
func runNetworkStatus(ctx context.Context, args []string, stdout, stderr io.Writer, effective *config.Config) int {
	return networkops.RunNetworkStatus(ctx, args, stdout, stderr, effective, inspectIncusNetworks)
}
func managementNetworkStatusSpec(effective *config.Config) (string, error) {
	return networkops.ManagementNetworkStatusSpec(effective)
}
