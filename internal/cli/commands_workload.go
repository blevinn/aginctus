package cli

import (
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

// normalizeWorkloadLegacyArgs supports the old "workload ID ensure|teardown"
// grammar without falling back into the old positional dispatcher.
func normalizeWorkloadLegacyArgs(args []string) []string {
	if len(args) < 3 || args[0] != "workload" {
		return args
	}
	switch args[1] {
	case "apply", "remove", "start", "stop", "status", "ls", "list", "gateway":
		return args
	}
	var action string
	switch args[2] {
	case "ensure":
		action = "apply"
	case "teardown":
		action = "remove"
	default:
		return args
	}
	mapped := append([]string{"workload", action, args[1]}, args[3:]...)
	return mapped
}

func addWorkloadCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	workloads := &cobra.Command{Use: "workload", Short: "Manage configured workloads"}
	root.AddCommand(workloads)
	add := func(parent *cobra.Command, use string, aliases []string, argsRule cobra.PositionalArgs,
		validate func(*cobra.Command, []string) error,
		action func(*cobra.Command, []string, *config.Config) int,
	) *cobra.Command {
		command := &cobra.Command{Use: use, Aliases: aliases, Args: argsRule, PreRunE: validate}
		command.RunE = func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err == nil {
				*status = action(cmd, args, cfg)
			}
			return nil
		}
		parent.AddCommand(command)
		return command
	}
	for _, verb := range []string{"apply", "remove", "start", "stop"} {
		action := verb
		var dryRun, force, render, confirm bool
		var project string
		validate := func(cmd *cobra.Command, args []string) error {
			if action == "apply" && cmd.Flags().Changed("project") {
				if len(args) != 1 || force || (render == dryRun) {
					return fmt.Errorf("scoped apply requires one ID and exactly one of --render or --dry-run, without --force")
				}
			} else if (action == "start" || action == "stop") && cmd.Flags().Changed("project") {
				if len(args) != 1 || !confirm {
					return fmt.Errorf("scoped %s requires one ID and --confirm-%s", action, action)
				}
			} else if render || confirm {
				return fmt.Errorf("scoped options require --project")
			}
			return nil
		}
		cmd := add(workloads, action+" [NAME]", nil, cobra.MaximumNArgs(1), validate,
			func(cmd *cobra.Command, args []string, cfg *config.Config) int {
				if cmd.Flags().Changed("project") {
					switch action {
					case "apply":
						preview := "--render"
						if dryRun {
							preview = "--dry-run"
						}
						return runScopedWorkloadApplyRender(cmd.Context(), []string{"--project", project, args[0], preview}, stdout, stderr, cfg)
					case "start", "stop":
						choice := []string{"--project", project, args[0], "--confirm-" + action}
						if action == "start" {
							return runScopedWorkloadStart(cmd.Context(), choice, stdout, stderr, cfg)
						}
						return runScopedWorkloadStop(cmd.Context(), choice, stdout, stderr, cfg)
					}
				}
				return executeConfiguredWorkloads(cmd.Context(), action, args,
					mutationOptions{DryRun: dryRun, Force: force}, stdout, stderr, cfg)
			})
		switch action {
		case "apply":
			cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Inspect without mutation")
			cmd.Flags().BoolVar(&force, "force", false, "Permit supported replacement")
			cmd.Flags().StringVar(&project, "project", "", "Select an owned local project for an existing-workload preview")
			cmd.Flags().BoolVar(&render, "render", false, "Render existing-workload plan without executing")
		case "remove":
			cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Inspect without mutation")
			cmd.Flags().BoolVar(&force, "force", false, "Permit supported replacement")
		case "start", "stop":
			cmd.Flags().StringVar(&project, "project", "", "Select owned local project")
			cmd.Flags().BoolVar(&confirm, "confirm-"+action, false, "Confirm scoped existing-instance transition")
		}
	}
	for _, name := range []string{"ls", "status"} {
		verb := name
		var project string
		argsRule := cobra.MaximumNArgs(1)
		if name == "ls" {
			argsRule = cobra.NoArgs
		}
		cmd := add(workloads, verb+" [NAME]", map[string][]string{"ls": {"list"}}[verb], argsRule, nil,
			func(cmd *cobra.Command, args []string, cfg *config.Config) int {
				if cmd.Flags().Changed("project") {
					return runScopedWorkloadStatus(cmd.Context(), append([]string{"--project", project}, args...), stdout, stderr, cfg, verb == "ls")
				}
				if verb == "ls" {
					return runActiveWorkloadList(cmd.Context(), nil, stdout, stderr, cfg)
				}
				return runWorkloadStatus(cmd.Context(), args, stdout, stderr, cfg)
			})
		cmd.Flags().StringVar(&project, "project", "", "Read from an explicitly owned local project")
	}
	gateway := &cobra.Command{Use: "gateway"}
	workloads.AddCommand(gateway)
	var model, keyFile string
	provision := add(gateway, "provision WORKLOAD", nil, cobra.ExactArgs(1),
		func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("model") || !cmd.Flags().Changed("key-file") {
				return fmt.Errorf("provision requires --model and --key-file")
			}
			return nil
		},
		func(cmd *cobra.Command, args []string, cfg *config.Config) int {
			return runWorkloadGatewayProvision(cmd.Context(), []string{args[0], "--model=" + model, "--key-file=" + keyFile}, stdout, stderr, cfg)
		})
	provision.Flags().StringVar(&model, "model", "", "Approved model alias")
	provision.Flags().StringVar(&keyFile, "key-file", "", "Private workload gateway key file")
}
