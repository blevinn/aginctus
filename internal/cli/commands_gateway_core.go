package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/spf13/cobra"
)

// Gateway command families are converted incrementally. Only core commands
// are intercepted here; model/provider and egress remain on the legacy path.

// executeGatewayCredential keeps the existing private-volume validation
// and side-effect sequencing independent of CLI argument parsing.
func executeGatewayCredential(ctx context.Context, action string, stdout, stderr io.Writer, effective *config.Config) int {
	switch action {
	case "audit":
		id, err := effective.String("gateway.id")
		if err != nil {
			fmt.Fprintf(stderr, "gateway id: %v\n", err)
			return 1
		}
		project, err := effective.String("gateway.compose.project")
		if err != nil {
			fmt.Fprintf(stderr, "gateway Compose project: %v\n", err)
			return 1
		}
		pool, err := effective.String("gateway.credentials.storage.pool")
		if err != nil {
			fmt.Fprintf(stderr, "credential storage pool: %v\n", err)
			return 1
		}
		spec := gateway.CredentialAttachment{GatewayID: id, ComposeProject: project, Pool: pool}
		if err := auditGatewayCredentials(ctx, spec); err != nil {
			fmt.Fprintf(stderr, "credential attachment audit: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "credential volume attachment: owned and exclusive to LiteLLM (metadata only; in-guest permissions, OAuth and runtime readiness not verified)\n")
		return 0
	case "attach":
		id, err := effective.String("gateway.id")
		if err != nil {
			fmt.Fprintf(stderr, "gateway id: %v\n", err)
			return 1
		}
		project, err := effective.String("gateway.compose.project")
		if err != nil {
			fmt.Fprintf(stderr, "gateway Compose project: %v\n", err)
			return 1
		}
		pool, err := effective.String("gateway.credentials.storage.pool")
		if err != nil {
			fmt.Fprintf(stderr, "credential storage pool: %v\n", err)
			return 1
		}
		spec := gateway.CredentialAttachment{GatewayID: id, ComposeProject: project, Pool: pool}
		if err := attachGatewayCredentials(ctx, spec); err != nil {
			fmt.Fprintf(stderr, "credential attachment: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "private credential volume attached exclusively to LiteLLM in Compose project %q (OAuth not activated)\n", project)
		return 0
	case "ensure":
		id, err := effective.String("gateway.id")
		if err != nil {
			fmt.Fprintf(stderr, "gateway credential storage gateway ID: %v\n", err)
			return 1
		}
		project, err := effective.String("gateway.compose.project")
		if err != nil {
			fmt.Fprintf(stderr, "gateway credential storage Compose project: %v\n", err)
			return 1
		}
		pool, err := effective.String("gateway.credentials.storage.pool")
		if err != nil {
			fmt.Fprintf(stderr, "gateway credential storage pool: %v\n", err)
			return 1
		}
		a := gateway.CredentialAttachment{GatewayID: id, ComposeProject: project, Pool: pool}
		if err := a.Validate(); err != nil {
			fmt.Fprintf(stderr, "gateway credential storage configuration: %v\n", err)
			return 1
		}
		if err := ensureCredentialStorage(ctx, a); err != nil {
			fmt.Fprintf(stderr, "gateway credential storage: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway credential volume %q: ready in Compose project %q (attachment unchanged; no credentials imported)\n", a.VolumeName(), project)
		return 0
	default:
		fmt.Fprintln(stderr, "unsupported gateway credential operation")
		return 2
	}
}

func executeGatewayLifecycle(ctx context.Context, action string, force bool, stdout, stderr io.Writer, effective *config.Config) int {
	spec, err := gateway.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
		return 1
	}
	switch action {
	case "status":
		return runGatewayStatus(ctx, nil, stdout, stderr, effective)
	case "remove":
		if err := spec.Remove(ctx, force); err != nil {
			fmt.Fprintf(stderr, "gateway cleanup: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway %q: gateway instances removed (persistent volumes retained)\n", spec.ID)
		return 0
	case "apply", "up":
		runtimeEnvironment, err := spec.InitializeRuntimeEnvironment()
		if err != nil {
			fmt.Fprintf(stderr, "gateway initialization: %v\n", err)
			return 1
		}
		if err := spec.Deploy(ctx, runtimeEnvironment); err != nil {
			fmt.Fprintf(stderr, "gateway deployment: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway %q: started (application readiness not verified)\n", spec.ID)
		return 0
	case "validate":
		fmt.Fprintf(stdout, "gateway %q: configuration valid\n", spec.ID)
		return 0
	case "render":
		rendered, err := spec.RenderCompose()
		if err != nil {
			fmt.Fprintf(stderr, "gateway render: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, rendered)
		return 0
	default:
		printGatewayUsage(stderr)
		return 2
	}
}

func addGatewayCoreCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) *cobra.Command {
	gatewayCmd := &cobra.Command{Use: "gateway", Short: "Inspect and reconcile the AI gateway", Args: cobra.NoArgs}
	gatewayCmd.RunE = func(_ *cobra.Command, _ []string) error {
		printGatewayUsage(stderr)
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
				return runGatewayStatus(cmd.Context(), nil, stdout, stderr, cfg)
			}
			return executeGatewayLifecycle(cmd.Context(), verb, force, stdout, stderr, cfg)
		})
	}
	credentials := &cobra.Command{Use: "credential-storage", Short: "Manage dedicated private gateway storage"}
	gatewayCmd.AddCommand(credentials)
	for _, action := range []string{"ensure", "attach", "audit"} {
		verb := action
		add(credentials, verb, nil, nil, func(cmd *cobra.Command, cfg *config.Config) int {
			return executeGatewayCredential(cmd.Context(), verb, stdout, stderr, cfg)
		})
	}
	return gatewayCmd
}
