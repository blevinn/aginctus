package egresscmd

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

type Handlers struct {
	Egress func(context.Context, string, string, string, io.Writer, io.Writer, *config.Config) int
	Verify func([]string, io.Writer, io.Writer) int
}

// Register contributes gateway egress and responses leaves with all
// confirmation and mutually-exclusive-flag validation still leaf-local.
func Register(
	gatewayCmd *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int, handlers Handlers,
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
				*status = handlers.Egress(cmd.Context(), op, z, r, stdout, stderr, cfg)
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
			*status = handlers.Egress(cmd.Context(), op, "", "", stdout, stderr, cfg)
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
			*status = handlers.Egress(cmd.Context(), "firewall-reload", "", "", stdout, stderr, cfg)
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
		*status = handlers.Verify(args, stdout, stderr)
		return nil
	}}
	verify.Flags().BoolVar(&requireFunctionCall, "require-function-call", false, "Require a completed tool/function call")
	responses.AddCommand(verify)
}
