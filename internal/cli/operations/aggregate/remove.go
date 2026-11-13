package aggregateops

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/workload"
)

// Remove preflights every configured spec before any destructive operation.
// Capabilities are bound at the CLI root so failure injections are preserved.
func Remove(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config,
	ids func(*config.Config) []string,
	executeWorkload func(context.Context, workload.Spec, workload.Options) error,
	executeHerdrClient func(context.Context, herdrclient.Spec, herdrclient.Options) error,
	removeAggregateGateway func(context.Context, gateway.Spec) error,
	executeManagementNetwork func(context.Context, managementnetwork.Spec, managementnetwork.Options) error,
) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] remove")
		return 2
	}
	// Resolve every configured resource before executing destructive steps.
	// Removal runs opposite to deployment dependencies: workloads, Herdr,
	// gateway and finally the shared network.
	workloads := make([]workload.Spec, 0, len(ids(cfg)))
	for _, id := range ids(cfg) {
		spec, err := workload.FromConfig(cfg, id)
		if err != nil {
			fmt.Fprintf(stderr, "workload %q configuration: %v\n", id, err)
			return 1
		}
		workloads = append(workloads, spec)
	}
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
		return 1
	}
	gatewaySpec, err := gateway.FromConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
		return 1
	}
	network, err := managementnetwork.FromConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "management network configuration: %v\n", err)
		return 1
	}

	for i := len(workloads) - 1; i >= 0; i-- {
		spec := workloads[i]
		if err := executeWorkload(ctx, spec, workload.Options{Operation: "delete"}); err != nil {
			fmt.Fprintf(stderr, "remove workload %q: %v\n", spec.ID, err)
			return 1
		}
		fmt.Fprintf(out, "workload %q: removed\n", spec.ID)
	}
	if err := executeHerdrClient(ctx, client, herdrclient.Options{Operation: "delete"}); err != nil {
		fmt.Fprintf(stderr, "remove Herdr client: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "Herdr client %q: removed\n", client.Name)
	if err := removeAggregateGateway(ctx, gatewaySpec); err != nil {
		fmt.Fprintf(stderr, "remove gateway: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "gateway %q: instances removed (volumes retained)\n", gatewaySpec.ID)
	if err := executeManagementNetwork(ctx, network, managementnetwork.Options{Operation: "delete"}); err != nil {
		fmt.Fprintf(stderr, "remove management network: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "management network %q: removed\n", network.Name)
	return 0
}
