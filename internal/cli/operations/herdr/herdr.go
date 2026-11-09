package herdr

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
)

type Options struct {
	DryRun bool
	Force bool
}

// Transition and Execute retain the established CLI result/exit contract.
// The mutation capabilities are supplied by root composition, never global.
// transitionHerdrClientCommand executes a validated lifecycle action.
func Transition(ctx context.Context, action string, stdout, stderr io.Writer, effective *config.Config, transition func(context.Context, herdrclient.Spec, string) error) int {
		spec, err := herdrclient.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
			return 1
		}
		if err := transition(ctx, spec, action); err != nil {
			fmt.Fprintf(stderr, "Herdr client %s: %v\n", action, err)
			return 1
		}
		verb := "started"
		if action == "stop" {
			verb = "stopped"
		}
		fmt.Fprintf(stdout, "Herdr client %q: %s\n", spec.Name, verb)
		return 0
}

// executeHerdrClientOperation is called only from parsed Cobra leaves.
func Execute(ctx context.Context, operation string, options Options, stdout, stderr io.Writer, effective *config.Config, execute func(context.Context, herdrclient.Spec, herdrclient.Options) error) int {
	spec, err := herdrclient.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
		return 1
	}

	err = execute(ctx, spec, herdrclient.Options{
		DryRun:    options.DryRun,
		Force:     options.Force,
		Operation: operation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client: %v\n", err)
		return 1
	}

	switch {
	case operation == "upsert" && options.DryRun:
		fmt.Fprintf(stdout, "Herdr client %q: would reconcile\n", spec.Name)
	case operation == "upsert":
		fmt.Fprintf(stdout, "Herdr client %q: ready\n", spec.Name)
	case options.DryRun:
		fmt.Fprintf(stdout, "Herdr client %q: would delete if present\n", spec.Name)
	default:
		fmt.Fprintf(stdout, "Herdr client %q: absent\n", spec.Name)
	}
	return 0
}

