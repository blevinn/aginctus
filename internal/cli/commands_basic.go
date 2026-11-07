package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// runBasicCobra is the first vertical slice of the declarative command tree.
// It deliberately constructs fresh commands and flags for every Run invocation.
// Legacy command families remain on their existing dispatch path until migrated.
func runBasicCobra(
	ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, loader ConfigLoader, options config.Options,
) int {
	root := &cobra.Command{Use: "aginctus", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(normalizeGatewayRegistryLegacyArgs(normalizeWorkloadLegacyArgs(args)))
	root.CompletionOptions.DisableDefaultCmd = true

	status := 0
	load := func() (*config.Config, error) {
		effective, err := loader.Load(options)
		if err != nil {
			fmt.Fprintf(stderr, "configuration: %v\n", err)
			status = 1
		}
		return effective, err
	}

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Read effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
	}
	configCmd.AddCommand(&cobra.Command{
		Use: "show", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			effective, err := load()
			if err == nil {
				status = runConfig([]string{"show"}, stdout, stderr, effective)
			}
			return nil
		},
	})
	configCmd.AddCommand(&cobra.Command{
		Use: "get PATH", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			effective, err := load()
			if err == nil {
				status = runConfig(append([]string{"get"}, args...), stdout, stderr, effective)
			}
			return nil
		},
	})
	root.AddCommand(configCmd)

	var fix bool
	doctor := &cobra.Command{
		Use: "doctor", Short: "Check Incus health", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := load(); err != nil {
				return nil
			}
			doctorArgs := []string{}
			if fix {
				doctorArgs = append(doctorArgs, "--fix")
			}
			status = runDoctor(ctx, doctorArgs, stdout, stderr, incusClient)
			return nil
		},
	}
	doctor.Flags().BoolVar(&fix, "fix", false, "Configure missing loopback HTTPS listener")
	root.AddCommand(doctor)

	addAggregateCommands(root, stdout, stderr, load, &status)
	addNetworkCommands(root, stdout, stderr, load, &status)
	addHerdrCommands(root, stdout, stderr, load, &status)
	addWorkloadCommands(root, stdout, stderr, load, &status)
	gatewayCmd := addGatewayCoreCommands(root, stdout, stderr, load, &status)
	addGatewayRegistryCommands(gatewayCmd, stdout, stderr, load, &status)
	addGatewayEgressCommands(gatewayCmd, stdout, stderr, load, &status)
	addOperatorCommands(root, stdout, stderr, load, &status, options)
	addTestenvCommands(root, stdout, stderr, &status, options)
	addImageCommands(root, stdout, stderr, load, &status)

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "%v\nUsage: aginctus [global options] <command> [flags]\n", err)
		return 2
	}
	return status
}
