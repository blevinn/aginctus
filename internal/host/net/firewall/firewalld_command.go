package firewall

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// RunFirewalld executes the existing host firewalld query surface. Callers
// remain responsible for selecting read-only arguments; this helper grants no
// privilege and does not authorize firewall activation or reload.
func RunFirewalld(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "firewall-cmd", args...).CombinedOutput()
	return interpretFirewalldResult(args, strings.TrimSpace(string(out)), err)
}

// interpretFirewalldResult preserves the firewalld CLI's special "no zone"
// reply for a missing interface binding, without swallowing other failures.
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
