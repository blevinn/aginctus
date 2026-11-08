package gatewaycmd

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

type Handlers struct {
	Status func(context.Context, []string, io.Writer, io.Writer, *config.Config) int
	Lifecycle func(context.Context, string, bool, io.Writer, io.Writer, *config.Config) int
	Credential func(context.Context, string, io.Writer, io.Writer, *config.Config) int
}

// Register builds the public gateway root and credential-storage leaves.
func Register(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int, handlers Handlers,
) *cobra.Command {
	gatewayCmd := &cobra.Command{
		Use: "gateway", Short: "Inspect and reconcile the AI gateway", Args: cobra.NoArgs,
		Long: "Gateway validate/render and plan/audit operations inspect intent or state, not authenticated inference. Provider/model registration writes local metadata only; it does not activate inference. Gateway removal preserves persistent volumes. Egress and DHCP mutations require their own explicit confirmation.",
	}
	gatewayCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		fmt.Fprint(stderr, cmd.UsageString())
		*status = 2
		return nil
	}
	root.AddCommand(gatewayCmd)
	add := func(parent *cobra.Command, name string, aliases []string, flags func(*cobra.Command),
		run func(*cobra.Command, *config.Config) int,
	) {
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
		if flags != nil {
			flags(cmd)
		}
		parent.AddCommand(cmd)
	}
	for _, action := range []struct {
		name    string
		aliases []string
	}{
		{"status", nil}, {"apply", []string{"up"}}, {"remove", nil},
		{"validate", nil}, {"render", nil},
	} {
		verb := action.name
		var force bool
		var flags func(*cobra.Command)
		if verb == "remove" {
			flags = func(cmd *cobra.Command) {
				cmd.Flags().BoolVar(&force, "force", false, "Allow supported gateway instance cleanup")
			}
		}
		add(gatewayCmd, verb, action.aliases, flags, func(cmd *cobra.Command, cfg *config.Config) int {
			if verb == "status" {
				return handlers.Status(cmd.Context(), nil, stdout, stderr, cfg)
			}
			return handlers.Lifecycle(cmd.Context(), verb, force, stdout, stderr, cfg)
		})
	}
	credentials := &cobra.Command{Use: "credential-storage", Short: "Manage dedicated private gateway storage"}
	gatewayCmd.AddCommand(credentials)
	for _, action := range []string{"ensure", "attach", "audit"} {
		verb := action
		add(credentials, verb, nil, nil, func(cmd *cobra.Command, cfg *config.Config) int {
			return handlers.Credential(cmd.Context(), verb, stdout, stderr, cfg)
		})
	}
	return gatewayCmd
}
