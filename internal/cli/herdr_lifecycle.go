package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/blevinn/aginctus/internal/herdrclient"
)

// Transition only an Aginctus-owned Herdr client in the default project.
// Reuse the injectable Incus command runner from workload lifecycle.
var transitionHerdrClient = func(ctx context.Context, spec herdrclient.Spec, action string) error {
	if action != "start" && action != "stop" {
		return fmt.Errorf("unsupported Herdr lifecycle action %q", action)
	}
	instances, err := statusInstances(ctx)
	if err != nil {
		return err
	}
	state, err := findManagedInstance(instances, spec.Name, "infrastructure", "herdr-client", "")
	if err != nil {
		return err
	}
	if state == "" {
		if action == "stop" {
			return nil
		}
		return fmt.Errorf("Herdr client %q is absent; run herdr client apply first", spec.Name)
	}
	running := strings.EqualFold(state, "running")
	if (action == "start" && running) || (action == "stop" && !running) {
		return nil
	}
	return runWorkloadIncus(ctx, action, spec.Name, "--project", "default")
}
