package networkcmd

import (
	"context"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// Register defines one parser for each network operation. In
// particular, the firewall mutations never inherit --dry-run or --force.
// Handlers keeps the network leaf grammar independent of the management backend.
type Handlers struct {
	Operation func(context.Context, string, bool, bool, io.Writer, io.Writer, *config.Config) int
	Status func(context.Context, io.Writer, io.Writer, *config.Config) int
	FirewallPolicy func(context.Context, io.Writer, io.Writer, *config.Config) int
	FirewallReconcile func(context.Context, io.Writer, io.Writer, *config.Config) int
	FirewallEgress func(context.Context, io.Writer, io.Writer, *config.Config, bool) int
}

func Register(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int, handlers Handlers,
) {
	network := &cobra.Command{Use: "network", Short: "Inspect and reconcile the management network"}
	root.AddCommand(network)

	add := func(parent *cobra.Command, name string, aliases []string,
		run func(*cobra.Command, *config.Config) int,
	) *cobra.Command {
		cmd := &cobra.Command{
			Use: name, Aliases: aliases, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				cfg, err := load()
				if err == nil {
					*status = run(cmd, cfg)
				}
				return nil
			},
		}
		parent.AddCommand(cmd)
		return cmd
	}

	for _, item := range []struct {
		name, operation string
		aliases []string
	}{
		{"apply", "upsert", []string{"ensure"}},
		{"remove", "delete", []string{"teardown"}},
	} {
		operation := item.operation
		var dryRun, force bool
		cmd := add(network, item.name, item.aliases, func(cmd *cobra.Command, cfg *config.Config) int {
			return handlers.Operation(cmd.Context(), operation, dryRun, force, stdout, stderr, cfg)
		})
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the intended change without mutation")
		cmd.Flags().BoolVar(&force, "force", false, "Explicitly permit supported replacement operations")
	}

	add(network, "status", nil, func(cmd *cobra.Command, cfg *config.Config) int {
		return handlers.Status(cmd.Context(), stdout, stderr, cfg)
	})

	firewall := &cobra.Command{Use: "firewall", Short: "Inspect or reconcile firewall rules"}
	network.AddCommand(firewall)
	add(firewall, "policy", nil, func(cmd *cobra.Command, cfg *config.Config) int {
		return handlers.FirewallPolicy(cmd.Context(), stdout, stderr, cfg)
	})
	add(firewall, "reconcile", nil, func(cmd *cobra.Command, cfg *config.Config) int {
		return handlers.FirewallReconcile(cmd.Context(), stdout, stderr, cfg)
	})
	egress := &cobra.Command{Use: "egress", Short: "Manage scoped egress firewall grants"}
	firewall.AddCommand(egress)
	add(egress, "reconcile", nil, func(cmd *cobra.Command, cfg *config.Config) int {
		return handlers.FirewallEgress(cmd.Context(), stdout, stderr, cfg, false)
	})
	add(egress, "revoke", nil, func(cmd *cobra.Command, cfg *config.Config) int {
		return handlers.FirewallEgress(cmd.Context(), stdout, stderr, cfg, true)
	})
}
