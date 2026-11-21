package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	clihelp "github.com/blevinn/aginctus/internal/cli/help"
	configops "github.com/blevinn/aginctus/internal/cli/operations/config"
	clioutput "github.com/blevinn/aginctus/internal/cli/output"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/client"
	"github.com/spf13/cobra"
)

// runBasicCobra constructs the authoritative Cobra command tree afresh for
// every invocation. Only explicit legacy spelling translations precede this
// parser; all command families are registered here and reuse shared services.
func runBasicCobra(
	ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, loader ConfigLoader, registry *plugins.Registry, logicalReader client.LogicalWorkloadReader, environmentReader client.EnvironmentReader,
) int {
	return runBasicCobraWithGatewayInput(ctx, args, stdout, stderr, incusClient, loader, registry, logicalReader, environmentReader, os.Stdin)
}

// runBasicCobraWithGatewayInput binds the verifier's stdin to one fresh Cobra
// tree. Callers never need to replace a package-level reader.
func runBasicCobraWithGatewayInput(
	ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, loader ConfigLoader, registry *plugins.Registry,
	logicalReader client.LogicalWorkloadReader, environmentReader client.EnvironmentReader, responsesInput io.Reader,
) int {
	var options config.Options
	var outputFormat string
	var selectedEnvironmentID, selectedIncusProject string
	root := &cobra.Command{
		Use: "aginctus", Short: "Manage Aginctus environments and workloads", SilenceUsage: true, SilenceErrors: true,
		Long: "Aginctus uses explicit commands and scopes. Inspection, plans, rendering and supported dry-runs do not authorize writes. Apply/remove/start/stop, provisioning and repair can mutate resources; --confirm-* guards apply only to the commands that declare them.",
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(normalizeLegacyCLIArgs(args))
	root.CompletionOptions.DisableDefaultCmd = true

	root.Version = Version
	root.SetVersionTemplate(fmt.Sprintf("aginctus %s (revision %s)\n", Version, buildRevision()))
	root.SetHelpCommand(&cobra.Command{
		Use: "help [--full|--examples] [command]", Short: "Help about any command",
		Args: cobra.ArbitraryArgs, DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			mode := ""
			if len(args) > 0 && (args[0] == "--full" || args[0] == "--examples") {
				mode, args = args[0], args[1:]
			}
			for _, arg := range args {
				if strings.HasPrefix(arg, "-") {
					return fmt.Errorf("unknown help option %q (use --full or --examples before the command path)", arg)
				}
			}
			target := root
			if len(args) > 0 {
				var remaining []string
				var err error
				target, remaining, err = root.Find(args)
				if err != nil || target == nil || len(remaining) != 0 {
					return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
				}
			}
			switch mode {
			case "--full":
				clihelp.WriteCobraReference(target, stdout)
				return nil
			case "--examples":
				return clihelp.WriteCobraExamples(target, stdout)
			default:
				return target.Help()
			}
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "version", Short: "Print build provenance", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprintf(stdout, "aginctus %s (revision %s)\n", Version, buildRevision())
			return nil
		},
	})
	overrides := &configOverrideFlag{values: &options.Overrides}
	pluginCmd := &cobra.Command{
		Use: "plugins", Short: "Inspect compiled contract plugins", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	pluginCmd.AddCommand(&cobra.Command{
		Use: "inventory", Aliases: []string{"list"}, Short: "Inspect compiled plugin inventory", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Preserve the v0.1 unwrapped inventory document unless the user
			// explicitly selects a format. New machine output is a typed API
			// projection, not an accidental change to existing scripts.
			if !root.PersistentFlags().Changed("output") {
				return json.NewEncoder(stdout).Encode(registry.Compiled())
			}
			format, err := clioutput.ParseFormat(outputFormat)
			if err != nil {
				return err
			}
			var source client.PluginInventory
			if registry != nil {
				source = registry
			}
			inventory, err := client.New(client.Options{PluginInventory: source}).Plugins.List(root.Context(), client.ListPluginsRequest{})
			if err != nil {
				return err
			}
			switch format {
			case clioutput.JSON, clioutput.YAML:
				return clioutput.WriteStructured(stdout, format, inventory)
			case clioutput.Name:
				for _, item := range inventory.Items {
					if _, err := fmt.Fprintln(stdout, item.Name); err != nil {
						return err
					}
				}
				return nil
			case clioutput.Table, clioutput.Wide:
				w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
				if format == clioutput.Wide {
					fmt.Fprintln(w, "NAME\tVERSION\tCAPABILITIES\tDEPENDENCIES")
					for _, item := range inventory.Items {
						kinds := make([]string, 0, len(item.Capabilities))
						for _, cap := range item.Capabilities {
							kinds = append(kinds, cap.Name+":"+cap.Kind)
						}
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.Name, item.Version, strings.Join(kinds, ","), strings.Join(item.Dependencies, ","))
					}
				} else {
					fmt.Fprintln(w, "NAME\tVERSION")
					for _, item := range inventory.Items {
						fmt.Fprintf(w, "%s\t%s\n", item.Name, item.Version)
					}
				}
				return w.Flush()
			default:
				return fmt.Errorf("unsupported plugin inventory output format %q", format)
			}
		},
	})
	addPluginShowCommand(pluginCmd, root, stdout, registry, &outputFormat)
	root.AddCommand(pluginCmd)
	root.PersistentFlags().Var(overrides, "config", "Override one path=value (repeatable)")
	root.PersistentFlags().Var(overrides, "configure", "Alias for --config")
	root.PersistentFlags().StringVar(&options.ConfigurationFile, "configuration-file", "", "Explicit JSON configuration file")
	root.PersistentFlags().StringVarP(&outputFormat, "output", "o", "", "Output format: table, wide, json, yaml, name (supported commands only)")
	root.PersistentFlags().StringVar(&selectedEnvironmentID, "environment", "", "Explicit immutable environment UUID for verified workload reads")
	root.PersistentFlags().StringVar(&selectedIncusProject, "incus-project", "", "Explicit authenticated Incus project for environment workload reads")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if root.PersistentFlags().Changed("configuration-file") && options.ConfigurationFile == "" {
			return fmt.Errorf("--configuration-file requires a path")
		}
		if root.PersistentFlags().Changed("output") {
			if outputFormat == "" {
				return fmt.Errorf("--output requires a format")
			}
			if _, err := clioutput.ParseFormat(outputFormat); err != nil {
				return err
			}
			pluginInventory := cmd.Parent() == pluginCmd && (cmd.Name() == "inventory" || cmd.Name() == "show")
			logicalWorkload := cmd.Parent() != nil && (cmd.Parent().Name() == "workloads" || cmd.Parent().Name() == "agents" || cmd.Parent().Name() == "frontends" || cmd.Parent().Name() == "gateways") &&
				(cmd.Name() == "list" || cmd.Name() == "show" || cmd.Name() == "status")
			environmentRead := cmd.Parent() != nil && cmd.Parent().Name() == "environment" &&
				(cmd.Name() == "list" || cmd.Name() == "show")
			verifiedScopedWorkload := cmd.Parent() != nil && cmd.Parent().Name() == "workload" &&
				(cmd.Name() == "ls" || cmd.Name() == "status") && cmd.Flags().Changed("project")
			if !pluginInventory && !verifiedScopedWorkload && !logicalWorkload && !environmentRead {
				return fmt.Errorf("--output is not yet supported by %s", cmd.CommandPath())
			}
		}
		logicalWorkload := cmd.Parent() != nil && (cmd.Parent().Name() == "workloads" || cmd.Parent().Name() == "agents" || cmd.Parent().Name() == "frontends" || cmd.Parent().Name() == "gateways")
		if root.PersistentFlags().Changed("environment") && !logicalWorkload {
			return fmt.Errorf("environment selector requires verified workloads read")
		}
		environmentRead := cmd.Parent() != nil && cmd.Parent().Name() == "environment"
		if root.PersistentFlags().Changed("incus-project") && !logicalWorkload && !environmentRead {
			return fmt.Errorf("incus project selector requires a verified environment or workloads read")
		}
		return nil
	}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		clihelp.WriteCobraHelp(cmd, stdout)
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
				status = configops.Run([]string{"show"}, stdout, stderr, effective)
			}
			return nil
		},
	})
	configCmd.AddCommand(&cobra.Command{
		Use: "get PATH", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			effective, err := load()
			if err == nil {
				status = configops.Run(append([]string{"get"}, args...), stdout, stderr, effective)
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
	registerLogicalWorkloadReads(root, stdout, logicalReader, &selectedIncusProject, &selectedEnvironmentID, &outputFormat)
	registerEnvironmentReads(root, stdout, environmentReader, &selectedIncusProject, &outputFormat)
	gatewayCmd := addGatewayCoreCommands(root, stdout, stderr, load, &status)
	addGatewayRegistryCommands(gatewayCmd, stdout, stderr, load, &status, options)
	addGatewayEgressCommands(gatewayCmd, stdout, stderr, load, &status, responsesInput)
	addOperatorCommands(root, stdout, stderr, load, &status, &options)
	addTestenvCommands(root, stdout, stderr, &status, &options)
	addImageCommands(root, stdout, stderr, load, &status)

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "%v\nUsage: %s\n", err, root.UseLine())
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
func (f *configOverrideFlag) Type() string   { return "path=value" }
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
		case args[idx] == "--config", args[idx] == "--configure", args[idx] == "--configuration-file", args[idx] == "--output", args[idx] == "-o", args[idx] == "--environment", args[idx] == "--incus-project":
			if idx+1 >= len(args) {
				return args
			}
			idx += 2
		case strings.HasPrefix(args[idx], "--config="),
			strings.HasPrefix(args[idx], "--configure="),
			strings.HasPrefix(args[idx], "--configuration-file="),
			strings.HasPrefix(args[idx], "--output="),
			strings.HasPrefix(args[idx], "--environment="),
			strings.HasPrefix(args[idx], "--incus-project="),
			(strings.HasPrefix(args[idx], "-o") && len(args[idx]) > 2):
			idx++
		default:
			out := append([]string(nil), args[:idx]...)
			return append(out, normalizeGatewayRegistryLegacyArgs(normalizeWorkloadLegacyArgs(args[idx:]))...)
		}
	}
	return args
}
