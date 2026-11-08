package cli

import (
	"context"
	"fmt"
	"io"

	egresscmd "github.com/blevinn/aginctus/internal/cli/commands/gateway/egress"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/spf13/cobra"
)

// Egress actions receive only prevalidated parameters. Confirmation flags are
// deliberately leaf-local, never inherited or inferred by another command.
func executeGatewayEgress(ctx context.Context, action, zone, ranges string,
	stdout, stderr io.Writer, effective *config.Config,
) int {
	switch action {
	case "firewall-audit":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress firewall configuration: %v\n", err)
			return 1
		}
		audit, err := spec.AuditEgressFirewalldActivation(ctx, zone, gateway.SystemFirewalld)
		if err != nil {
			fmt.Fprintf(stderr, "egress firewall audit blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(audit))
		return 0
	case "firewall-plan":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress firewall configuration: %v\n", err)
			return 1
		}
		plan, err := spec.PlanEgressFirewalld(zone)
		if err != nil {
			fmt.Fprintf(stderr, "egress firewall plan blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(plan))
		return 0
	case "firewall-reload":
		if err := gateway.CheckFirewalldReloadParity(ctx, gateway.SystemFirewalld); err != nil {
			fmt.Fprintf(stderr, "firewalld reload parity blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "selected runtime/permanent firewalld state matches; global reload NOT approved or executed")
		return 0
	case "dhcp-apply":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress DHCP configuration: %v\n", err)
			return 1
		}
		if err := gateway.ApplyEgressDHCPMigration(ctx, spec, ranges, gatewayEgressQuery); err != nil {
			fmt.Fprintf(stderr, "egress DHCP migration blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "owned egress bridge DHCP range updated; LiteLLM networking NOT reconciled")
		return 0
	case "dhcp-plan":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress DHCP migration configuration: %v\n", err)
			return 1
		}
		plan, err := gateway.PlanEgressDHCPMigration(ctx, spec, ranges, gatewayEgressQuery)
		if err != nil {
			fmt.Fprintf(stderr, "egress DHCP migration blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, plan)
		return 0
	case "reconcile-write":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress reconciliation configuration: %v\n", err)
			return 1
		}
		if err := gateway.ReconcileStoppedEgressNICs(ctx, spec, gatewayEgressQuery); err != nil {
			fmt.Fprintf(stderr, "egress reconciliation blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "LiteLLM NIC settings updated; instance remains stopped; inspect and start separately")
		return 0
	case "reconcile-dry":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress reconciliation configuration: %v\n", err)
			return 1
		}
		plan, err := gateway.PlanEgressReconciliation(ctx, spec, gatewayEgressQuery)
		if err != nil {
			fmt.Fprintf(stderr, "egress reconciliation blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, plan)
		return 0
	case "audit":
		spec, err := gateway.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "egress audit configuration: %v\n", err)
			return 1
		}
		if err := gateway.AuditEgressRuntime(ctx, spec, gatewayEgressQuery); err != nil {
			fmt.Fprintf(stderr, "egress runtime audit blocked: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "LiteLLM NIC declarations verified (in-guest routes and upstream connectivity NOT verified)")
		return 0
	case "preflight":
		name, err := effective.String("gateway.egress.network.name")
		if err != nil {
			fmt.Fprintf(stderr, "egress configuration: %v\n", err)
			return 1
		}
		candidate, err := gateway.EgressAddressPreflight(ctx, name)
		//lint:ignore SA4023 The preflight must return a denial for an unreserved candidate; retain the error gate for any future atomic allocator.
		if err != nil {
			fmt.Fprintf(stderr, "egress preflight blocked: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "egress candidate: %s (advisory only; NOT RESERVED, do not deploy without full inventory and atomic allocation)\n", candidate)
		return 0
	default:
		fmt.Fprintln(stderr, "unknown gateway egress operation")
		return 2
	}
}


func addGatewayEgressCommands(
	gatewayCmd *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	egresscmd.Register(gatewayCmd, stdout, stderr, load, status, egresscmd.Handlers{
		Egress: executeGatewayEgress,
		Verify: verifyGatewayResponsesStream,
	})
}
