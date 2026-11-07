package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	"github.com/blevinn/aginctus/internal/siwc"
	gatewayassets "github.com/blevinn/aginctus/scripts/gateway"
	"github.com/spf13/cobra"
)

// Private administration stays explicit; catalog binding reads host declarations.
func addSIWCCommands(gateway *cobra.Command, options *config.Options, status *int, load func() (*config.Config, error)) {
	group := &cobra.Command{Use: "siwc", Short: "Enroll and administer opt-in Sign in with ChatGPT", PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Root().PersistentFlags().Changed("configuration-file") && options.ConfigurationFile == "" {
			return fmt.Errorf("--configuration-file requires a path")
		}
		if len(options.Overrides) != 0 || options.ConfigurationFile != "" {
			catalog, _ := cmd.Flags().GetString("catalog")
			if cmd.Name() != "bind" || catalog == "" {
				return fmt.Errorf("SIWC private administration requires explicit state; host configuration is supported only for catalog binding")
			}
		}
		return nil
	}}
	gateway.AddCommand(group)
	var directory string
	group.PersistentFlags().StringVar(&directory, "state-dir", "", "Absolute private SIWC state directory")
	add := func(name, short string, action func(*cobra.Command, *siwc.Owner) error) *cobra.Command {
		command := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs, PreRunE: func(cmd *cobra.Command, _ []string) error {
			if directory == "" {
				return fmt.Errorf("required flag --state-dir not set")
			}
			for _, flag := range []string{"account", "route"} {
				if cmd.Flags().Changed(flag) {
					value, _ := cmd.Flags().GetString(flag)
					if !siwc.ValidName(value) {
						return fmt.Errorf("--%s requires a simple label (letters, digits, hyphen, underscore)", flag)
					}
				}
			}
			return nil
		}, Run: func(cmd *cobra.Command, _ []string) {
			owner, err := siwc.Open(directory)
			if err == nil {
				err = action(cmd, owner)
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), siwc.Diagnostic(err))
				*status = 1
			}
		}}
		group.AddCommand(command)
		return command
	}
	writeJSON := func(cmd *cobra.Command, value any) error {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	add("init", "Initialize a private host identity", func(cmd *cobra.Command, owner *siwc.Owner) error {
		host, err := owner.Init(cmd.Context())
		if err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), host)
		}
		return err
	})
	add("status", "Show local account state without remote verification", func(cmd *cobra.Command, owner *siwc.Owner) error {
		value, err := owner.Status(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, value)
	})
	add("render", "Render public LiteLLM configuration", func(cmd *cobra.Command, owner *siwc.Owner) error {
		value, err := owner.Render(cmd.Context())
		if err != nil {
			return err
		}
		return writeJSON(cmd, value)
	})
	accountFlag := func(cmd *cobra.Command, account *string) {
		cmd.Flags().StringVar(account, "account", "", "Local account label")
		_ = cmd.MarkFlagRequired("account")
	}
	var logoutAccount string
	logout := add("logout", "Revoke the remote session and clear local tokens", func(cmd *cobra.Command, owner *siwc.Owner) error {
		confirmed, err := owner.Logout(cmd.Context(), logoutAccount)
		if err != nil {
			return err
		}
		message := "Remote revocation confirmed."
		if !confirmed {
			message = "Remote revocation NOT confirmed; disconnect Aginctus in ChatGPT Settings."
			*status = 1
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Local tokens cleared; registration retained. "+message)
		return nil
	})
	accountFlag(logout, &logoutAccount)
	var binding siwc.Binding
	bind := add("bind", "Bind a model to an explicitly consented workload key", func(cmd *cobra.Command, owner *siwc.Owner) error {
		if err := owner.Bind(cmd.Context(), binding); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "SIWC route bound to the selected registration, model and workload key.")
		return nil
	})
	var bindingCatalog string
	bind.Flags().StringVar(&binding.Account, "account", "", "Private account label (explicit-state administration)")
	bind.Flags().StringVar(&bindingCatalog, "catalog", "", "Resolve this route's provider and model from a catalog")
	bind.Flags().StringVar(&binding.Route, "route", "", "SIWC route alias (siwc- prefix)")
	bind.Flags().StringVar(&binding.Model, "model", "", "Exact account catalog model slug")
	bind.Flags().StringVar(&binding.WorkloadKeyFile, "workload-key-file", "", "Private LiteLLM workload key file")
	bind.Flags().BoolVar(&binding.ConsentConnectedApp, "consent-connected-app", false, "Consent to connected Aginctus application use")
	bind.Flags().BoolVar(&binding.ConsentBackground, "consent-background", false, "Consent to background use")
	for _, flag := range []string{"route", "workload-key-file"} {
		_ = bind.MarkFlagRequired(flag)
	}
	validateBinding := bind.PreRunE
	bind.PreRunE = func(cmd *cobra.Command, args []string) error {
		if bindingCatalog != "" {
			for _, flag := range []string{"account", "model", "state-dir"} {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--catalog cannot be combined with explicit --account, --model or --state-dir")
				}
			}
			effective, err := load()
			if err != nil {
				return err
			}
			store, err := configuredGatewayStore(effective)
			if err != nil {
				return err
			}
			directory, err = resolveSIWCCatalogBinding(store, bindingCatalog, &binding)
			if err != nil {
				return err
			}
		} else if binding.Account == "" || binding.Model == "" {
			return fmt.Errorf("bind requires --catalog or explicit --state-dir, --account and --model")
		}
		return validateBinding(cmd, args)
	}
	var unbindRoute string
	unbind := add("unbind", "Remove a route binding", func(cmd *cobra.Command, owner *siwc.Owner) error {
		if err := owner.Unbind(cmd.Context(), unbindRoute); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "SIWC route binding removed.")
		return nil
	})
	unbind.Flags().StringVar(&unbindRoute, "route", "", "SIWC route alias")
	_ = unbind.MarkFlagRequired("route")
	var exportAccount, targetHost, exportCapsule string
	export := add("export", "Disarm the source and write a private one-time handoff", func(cmd *cobra.Command, owner *siwc.Owner) error {
		if err := owner.Export(cmd.Context(), exportAccount, targetHost, exportCapsule); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Source session disabled. Transfer the private capsule securely, receive once, then remove both copies.")
		return nil
	})
	accountFlag(export, &exportAccount)
	export.Flags().StringVar(&targetHost, "target-host-id", "", "Initialized target host's urn:uuid identity")
	export.Flags().StringVar(&exportCapsule, "capsule", "", "New private capsule inside the state directory")
	_ = export.MarkFlagRequired("target-host-id")
	_ = export.MarkFlagRequired("capsule")
	var receiveCapsule string
	receive := add("receive", "Validate and receive a handoff for this host", func(cmd *cobra.Command, owner *siwc.Owner) error {
		if err := owner.Receive(cmd.Context(), receiveCapsule); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "SIWC handoff validated and persisted for this host. Remove the private capsule.")
		return nil
	})
	receive.Flags().StringVar(&receiveCapsule, "capsule", "", "Private handoff capsule")
	_ = receive.MarkFlagRequired("capsule")
	group.AddCommand(&cobra.Command{Use: "bundle DIRECTORY", Short: "Write public LiteLLM runtime sources to a new directory", Args: cobra.ExactArgs(1), Run: func(cmd *cobra.Command, args []string) {
		dir := args[0]
		if !filepath.IsAbs(dir) {
			fmt.Fprintln(cmd.ErrOrStderr(), "SIWC bundle requires an absolute directory")
			*status = 2
			return
		}
		if err := os.Mkdir(dir, 0700); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "SIWC bundle destination must be a new directory")
			*status = 1
			return
		}
		for name, content := range gatewayassets.SIWCFiles() {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "SIWC bundle delivery failed")
				*status = 1
				return
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "SIWC public runtime bundle written; enrollment and activation are separate operations")
	}})
}

func resolveSIWCCatalogBinding(store *gatewaycontrol.Store, name string, binding *siwc.Binding) (string, error) {
	providers, err := store.Providers()
	if err != nil {
		return "", err
	}
	assets, err := store.Assets()
	if err != nil {
		return "", err
	}
	catalog, err := gatewaycontrol.Resolve(name, providers, assets)
	if err != nil {
		return "", err
	}
	asset, ok := catalog.Models[binding.Route]
	if !ok {
		return "", fmt.Errorf("route is not an exposed model in the selected catalog")
	}
	provider := providers[asset.Provider]
	if provider.Plugin != "openai-siwc" {
		return "", fmt.Errorf("selected catalog model does not belong to an openai-siwc provider")
	}
	binding.Account = provider.Name
	binding.Model = provider.Config.Models[asset.Name].Source
	return provider.Config.StateDir, nil
}
