package cli

import (
	"context"
	"fmt"
	"os/exec"
)

var gatewayEgressQuery = func(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "incus", args...)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect LiteLLM networking: %w", err)
	}
	return output, nil
}
