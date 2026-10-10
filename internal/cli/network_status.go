package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
)

// inspectIncusNetworks is replaceable for testing; status never mutates Incus.
var inspectIncusNetworks = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "incus", "network", "list", "--format=json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("incus network list: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type inspectedNetwork struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Status string            `json:"status"`
	Config map[string]string `json:"config"`
}

func runNetworkStatus(ctx context.Context, args []string, stdout, stderr io.Writer, effective *config.Config) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] network status")
		return 2
	}
	// A configured name alone does not establish ownership.
	spec, err := managementNetworkStatusSpec(effective)
	if err != nil {
		fmt.Fprintf(stderr, "management network configuration: %v\n", err)
		return 1
	}
	data, err := inspectIncusNetworks(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "management network status: %v\n", err)
		return 1
	}
	var networks []inspectedNetwork
	if err := json.Unmarshal(data, &networks); err != nil {
		fmt.Fprintf(stderr, "decode management network status: %v\n", err)
		return 1
	}
	for _, network := range networks {
		if network.Name != spec {
			continue
		}
		if network.Config["user.aginctus.managed"] != "true" || network.Config["user.aginctus.resource"] != "management-network" {
			fmt.Fprintf(stderr, "management network %q: exists but is not owned by Aginctus\n", spec)
			return 1
		}
		fmt.Fprintf(stdout, "management network %q: present (type=%s, status=%s)\n", spec, network.Type, network.Status)
		return 0
	}
	fmt.Fprintf(stdout, "management network %q: absent\n", spec)
	return 0
}

func managementNetworkStatusSpec(effective *config.Config) (string, error) {
	name, err := effective.String("incus.management.network.name")
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("management network name is empty")
	}
	return name, nil
}
