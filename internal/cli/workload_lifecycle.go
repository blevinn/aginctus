package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/workload"
)

// Lifecycle operations inspect ownership before invoking the Incus CLI.
var runWorkloadIncus = func(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "incus", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

var transitionWorkload = func(ctx context.Context, spec workload.Spec, action string) error {
	instances, err := statusInstances(ctx)
	if err != nil {
		return err
	}
	state, err := findManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil {
		return err
	}
	if state == "" {
		if action == "stop" {
			return nil
		}
		return fmt.Errorf("workload %q does not exist; run workload apply first", spec.ID)
	}
	running := strings.EqualFold(state, "running")
	if action == "stop" && !running {
		return nil
	}
	if action == "start" && running {
		return nil
	}
	if action != "stop" && action != "start" {
		return fmt.Errorf("unknown workload lifecycle action %q", action)
	}
	return runWorkloadIncus(ctx, action, spec.Name, "--project", "default")
}
