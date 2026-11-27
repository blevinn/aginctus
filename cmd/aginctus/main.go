package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/cli"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/nativelifecycle"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/plugins/composition"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "gateway-protected-stop" {
		os.Exit(runProtectedGatewayStop(os.Args[3:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "gateway-protected-start" {
		os.Exit(runProtectedGatewayStart(os.Args[3:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "gateway-protected-publish" {
		os.Exit(runProtectedGatewayPublish(os.Args[3:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "gateway-protected-provision" {
		os.Exit(runProtectedGatewayProvision(os.Args[3:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "gateway-protected-preview" {
		os.Exit(runProtectedGatewayPreview(os.Args[3:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "workload-bridge-observe" {
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: aginctus internal workload-bridge-observe HOST_GRANT")
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err := nativelifecycle.ObserveLocalWorkloadBridge(ctx, os.Args[3], os.Stdout)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "approved bridge observation failed")
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 3 && os.Args[1] == "internal" && os.Args[2] == "project-state-attachment" {
		os.Exit(runProjectStateAttachment(os.Args[3:]))
	}
	if len(os.Args) >= 3 && os.Args[1] == "serve" && os.Args[2] == "project-state" {
		if len(os.Args) == 4 && os.Args[3] == "bootstrap" {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			status, err := environment.BootstrapMountedProjectState(ctx)
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stdout, status)
			return
		}
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: aginctus serve project-state [bootstrap]")
			os.Exit(2)
		}
		os.Exit(runProjectStateService())
	}
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
		readers, closeRead, err := environment.OpenProductionOperatorReads(ctx, path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "restricted operator project read unavailable")
			os.Exit(1)
		}
		status := cli.RunWithReadCapabilities(ctx, os.Args[1:], os.Stdout, os.Stderr,
			incus.NewClient(), config.NewLoader(), registry,
			cli.ReadCapabilities{
				Environments: readers.Environments, Configured: readers.Configured,
				Accepted: readers.Accepted, Diff: readers.Diff,
			})
		closeRead()
		os.Exit(status)
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

// This supervised role starts before ordinary CLI configuration or plugin
// composition. Its explicit restricted credentials remain independently scoped.
func runProjectStateService() int {
	path := os.Getenv("AGINCTUS_PROJECT_STATE_READ_SERVICE_CONFIG")
	serviceConfig, err := environment.ReadProjectStateReadServiceConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "project-state read service configuration unavailable")
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := environment.ServeMountedProjectStateReads(ctx, serviceConfig); err != nil {
		fmt.Fprintln(os.Stderr, "restricted project-state service unavailable")
		return 1
	}
	return 0
}
