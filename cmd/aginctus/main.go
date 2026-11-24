package main

import (
	"context"
	"fmt"
	"os"

	"github.com/blevinn/aginctus/internal/plugins/composition"
	"github.com/blevinn/aginctus/internal/cli"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

func main() {
	ctx := context.Background()
	registry, err := appplugins.New(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugins: %v\n", err)
		os.Exit(1)
	}
	// This opt-in path supplies a real restricted Incus inventory reader to
	// the ordinary installed binary. The selected config is not authority:
	// the TLS client must still pass independent Incus project checks.
	// The default CLI remains unchanged and fails closed for all
	// environment/state/workload commands without authenticated readers.
	if path, selected := os.LookupEnv("AGINCTUS_RESTRICTED_READ_CONFIG"); selected {
		reader, closeRead, err := environment.ProductionEnrollmentReads(ctx, path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "restricted operator project read unavailable")
			os.Exit(1)
		}
		defer closeRead()
		os.Exit(cli.RunWithReadCapabilities(ctx, os.Args[1:], os.Stdout, os.Stderr,
			incus.NewClient(), config.NewLoader(), registry,
			cli.ReadCapabilities{Environments: reader}))
	}
	os.Exit(cli.RunWithPlugins(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		incus.NewClient(),
		config.NewLoader(),
		registry,
	))
}
