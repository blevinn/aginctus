package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// runBasicCobra is the first vertical slice of the declarative command tree.
// It deliberately constructs fresh commands and flags for every Run invocation.
// Legacy command families remain on their existing dispatch path until migrated.
func runBasicCobra(
	ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, loader ConfigLoader,
) int {
	var options config.Options
	root := &cobra.Command{Use: "aginctus", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(normalizeLegacyCLIArgs(args))
	root.CompletionOptions.DisableDefaultCmd = true

	root.Version = Version
	root.SetVersionTemplate(fmt.Sprintf("aginctus %s (revision %s)\n", Version, buildRevision()))
	root.AddCommand(&cobra.Command{
		Use: "version", Short: "Print build provenance", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprintf(stdout, "aginctus %s (revision %s)\n", Version, buildRevision())
			return nil
		},
	})
	overrides := &configOverrideFlag{values: &options.Overrides}
	root.PersistentFlags().Var(overrides, "config", "Override one path=value (repeatable)")
	root.PersistentFlags().Var(overrides, "configure", "Alias for --config")
	root.PersistentFlags().StringVar(&options.ConfigurationFile, "configuration-file", "", "Explicit JSON configuration file")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if root.PersistentFlags().Changed("configuration-file") && options.ConfigurationFile == "" {
			return fmt.Errorf("--configuration-file requires a path")
		}
		return nil
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if cmd == root {
			printUsage(stdout)
			return
		}
		family := cmd
		for family.Parent() != nil && family.Parent() != root {
			family = family.Parent()
		}
		if code, handled := runHelp([]string{"help", family.Name()}, stdout, stderr); !handled || code != 0 {
			fmt.Fprint(stdout, cmd.UsageString())
		}
	})

	status := 0
	load := func() (*config.Config, error) {
		effective, err := loader.Load(options)
		if err != nil {
			fmt.Fprintf(stderr, "configuration: %v\n", err)
			status = 1
		}
		return effective, err
	}

	configCmd := &cobra.Command{Use: "config", Short: "Read effective configuration"}
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
	addOperatorCommands(root, stdout, stderr, load, &status, &options)
	addTestenvCommands(root, stdout, stderr, &status, &options)
	addImageCommands(root, stdout, stderr, load, &status)

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "%v\nUsage: aginctus [global options] <command> [flags]\n", err)
		return 2
	}
	return status
}

// configOverrideFlag stores repeated root overrides in arrival order, even
// when operators interleave --config and the --configure alias.
type configOverrideFlag struct {
	values *[]string
}

func (f *configOverrideFlag) String() string { return strings.Join(*f.values, ",") }
func (f *configOverrideFlag) Type() string { return "path=value" }
func (f *configOverrideFlag) Set(value string) error {
	if value == "" {
		return fmt.Errorf("configuration override requires path=value")
	}
	*f.values = append(*f.values, value)
	return nil
}

// Legacy syntax translation needs only the location of the first command,
// not a second parser. Cobra remains authoritative for flag validation.
func normalizeLegacyCLIArgs(args []string) []string {
	idx := 0
	for idx < len(args) {
		switch {
		case args[idx] == "--config", args[idx] == "--configure", args[idx] == "--configuration-file":
			if idx+1 >= len(args) {
				return args
			}
			idx += 2
		case strings.HasPrefix(args[idx], "--config="),
			strings.HasPrefix(args[idx], "--configure="),
			strings.HasPrefix(args[idx], "--configuration-file="):
			idx++
		default:
			out := append([]string(nil), args[:idx]...)
			return append(out, normalizeGatewayRegistryLegacyArgs(normalizeWorkloadLegacyArgs(args[idx:]))...)
		}
	}
	return args
}
