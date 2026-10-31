package cli

import (
	"context"
	"fmt"
	"io"

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
	egress := &cobra.Command{Use: "egress", Short: "Inspect or safely reconcile gateway egress"}
	gatewayCmd.AddCommand(egress)
	makeLeaf := func(parent *cobra.Command, name, op string, validate func(*cobra.Command) error,
		zone, ranges *string) *cobra.Command {
		cmd := &cobra.Command{Use: name, Args: cobra.NoArgs}
		cmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
			if validate != nil {
				return validate(cmd)
			}
			return nil
		}
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			cfg, err := load()
			if err == nil {
				z, r := "", ""
				if zone != nil {
					z = *zone
				}
				if ranges != nil {
					r = *ranges
				}
				*status = executeGatewayEgress(cmd.Context(), op, z, r, stdout, stderr, cfg)
			}
			return nil
		}
		parent.AddCommand(cmd)
		return cmd
	}
	makeLeaf(egress, "audit", "audit", nil, nil, nil)
	makeLeaf(egress, "preflight", "preflight", nil, nil, nil)
	var reconcileDry, confirmStopped bool
	checkRecon := func(*cobra.Command) error {
		if reconcileDry == confirmStopped {
			return fmt.Errorf("reconcile requires exactly one of --dry-run or --confirm-stopped-instance")
		}
		return nil
	}
	reconcile := makeLeaf(egress, "reconcile", "reconcile-dry", checkRecon, nil, nil)
	reconcile.Flags().BoolVar(&reconcileDry, "dry-run", false, "Plan only; no mutation")
	reconcile.Flags().BoolVar(&confirmStopped, "confirm-stopped-instance", false, "Confirm stopped-instance NIC reconciliation")
	reconcile.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg, err := load()
		if err == nil {
			op := "reconcile-dry"
			if confirmStopped {
				op = "reconcile-write"
			}
			*status = executeGatewayEgress(cmd.Context(), op, "", "", stdout, stderr, cfg)
		}
		return nil
	}
	firewall := &cobra.Command{Use: "firewall", Short: "Inspect firewall plan and runtime/permanent parity"}
	egress.AddCommand(firewall)
	for _, op := range []string{"audit", "plan"} {
		action := op
		var zone string
		leaf := makeLeaf(firewall, action, "firewall-"+action, func(cmd *cobra.Command) error {
			if !cmd.Flags().Changed("uplink-zone") {
				return fmt.Errorf("--uplink-zone is required")
			}
			return nil
		}, &zone, nil)
		leaf.Flags().StringVar(&zone, "uplink-zone", "", "Operator-selected upstream firewalld zone")
	}
	parity := &cobra.Command{Use: "reload-parity", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := load()
		if err == nil {
			*status = executeGatewayEgress(cmd.Context(), "firewall-reload", "", "", stdout, stderr, cfg)
		}
		return nil
	}}
	firewall.AddCommand(parity)
	// Commands retired by NixOS-owned firewall policy must never reappear as
	// writable operations through generic Cobra argument acceptance.
	for _, retired := range []string{"stage", "unstage", "verify-staged"} {
		firewall.AddCommand(&cobra.Command{
			Use: retired, Args: cobra.ArbitraryArgs, DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				fmt.Fprintln(stderr, "permanent staging commands are retired: NixOS owns egress objects; use network firewall egress reconcile or revoke for runtime grants")
				*status = 2
				return nil
			},
		})
	}
	dhcp := &cobra.Command{Use: "dhcp", Short: "Review or apply guarded egress DHCP changes"}
	egress.AddCommand(dhcp)
	var planRanges string
	plan := makeLeaf(dhcp, "plan", "dhcp-plan", func(cmd *cobra.Command) error {
		if !cmd.Flags().Changed("ranges") {
			return fmt.Errorf("--ranges is required")
		}
		return nil
	}, nil, &planRanges)
	plan.Flags().StringVar(&planRanges, "ranges", "", "Proposed new DHCP address ranges")
	var applyRanges string
	var confirmBridge bool
	apply := makeLeaf(dhcp, "apply", "dhcp-apply", func(cmd *cobra.Command) error {
		if !cmd.Flags().Changed("ranges") || !confirmBridge {
			return fmt.Errorf("dhcp apply requires --ranges and --confirm-bridge-only")
		}
		return nil
	}, nil, &applyRanges)
	apply.Flags().StringVar(&applyRanges, "ranges", "", "New DHCP address ranges")
	apply.Flags().BoolVar(&confirmBridge, "confirm-bridge-only", false, "Confirm bridge-only DHCP mutation")
	responses := &cobra.Command{Use: "responses"}
	gatewayCmd.AddCommand(responses)
	var requireFunctionCall bool
	verify := &cobra.Command{Use: "verify", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		args := []string{}
		if requireFunctionCall {
			args = append(args, "--require-function-call")
		}
		*status = verifyGatewayResponsesStream(args, stdout, stderr)
		return nil
	}}
	verify.Flags().BoolVar(&requireFunctionCall, "require-function-call", false, "Require a completed tool/function call")
	responses.AddCommand(verify)
}
