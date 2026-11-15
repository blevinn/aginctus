package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
)

// Ambient v0.1 network status is a read-only observation of Incus metadata.
// The caller injects the exact existing list transport.
type InspectedNetwork struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Status string            `json:"status"`
	Config map[string]string `json:"config"`
}

func RunNetworkStatus(ctx context.Context, args []string, stdout, stderr io.Writer, effective *config.Config, inspect func(context.Context) ([]byte, error)) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] network status")
		return 2
	}
	// A configured name alone does not establish ownership.
	spec, err := ManagementNetworkStatusSpec(effective)
	if err != nil {
		fmt.Fprintf(stderr, "management network configuration: %v\n", err)
		return 1
	}
	data, err := inspect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "management network status: %v\n", err)
		return 1
	}
	var networks []InspectedNetwork
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

func ManagementNetworkStatusSpec(effective *config.Config) (string, error) {
	name, err := effective.String("incus.management.network.name")
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("management network name is empty")
	}
	return name, nil
}
