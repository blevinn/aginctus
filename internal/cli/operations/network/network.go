package network

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/managementnetwork"
)

type Options struct {
	DryRun bool
	Force  bool
}

// Execute reconciles one configured management network using the injected
// mutating capability only after syntax and config checks at the CLI boundary.
func Execute(ctx context.Context, operation string, options Options, stdout, stderr io.Writer, effective *config.Config, execute func(context.Context, managementnetwork.Spec, managementnetwork.Options) error) int {
	spec, err := managementnetwork.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "management network configuration: %v\n", err)
		return 1
	}

	err = execute(ctx, spec, managementnetwork.Options{
		DryRun:    options.DryRun,
		Force:     options.Force,
		Operation: operation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "management network: %v\n", err)
		return 1
	}

	switch {
	case operation == "upsert" && options.DryRun:
		fmt.Fprintf(stdout, "management network %q: would reconcile\n", spec.Name)
	case operation == "upsert":
		fmt.Fprintf(stdout, "management network %q: ready\n", spec.Name)
	case options.DryRun:
		fmt.Fprintf(stdout, "management network %q: would delete if present\n", spec.Name)
	default:
		fmt.Fprintf(stdout, "management network %q: absent\n", spec.Name)
	}
	return 0
}
