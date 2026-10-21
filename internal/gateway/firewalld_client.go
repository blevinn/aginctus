package gateway

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/firewall"
)

type FirewalldExecutor func(context.Context, ...string) (string, error)

func SystemFirewalld(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "firewall-cmd", args...).CombinedOutput()
	return interpretFirewalldResult(args, strings.TrimSpace(string(out)), err)
}

func interpretFirewalldResult(args []string, output string, err error) (string, error) {
	if err == nil {
		return output, nil
	}
	var exitErr *exec.ExitError
	if len(args) == 1 && strings.HasPrefix(args[0], "--get-zone-of-interface=") &&
		output == "no zone" && errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return "no zone", nil
	}
	return "", fmt.Errorf("firewall-cmd failed: %w: %s", err, output)
}

const egressFirewallPolicy = firewall.EgressHTTPS
const egressFirewallZone = firewall.EgressZone
