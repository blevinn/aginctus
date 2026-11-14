package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	gatewaycmd "github.com/blevinn/aginctus/internal/cli/commands/gateway"
	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	siwc "github.com/blevinn/aginctus/internal/plugins/openai-siwc"
	"github.com/spf13/cobra"
)

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
	if err == nil && (action == "validate" || action == "render") {
		spec, err = configuredGatewaySpec(effective)
	}
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
		spec, err = configuredGatewaySpec(effective)
		if err != nil {
			fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
			return 1
		}
		if err := spec.Deploy(ctx, runtimeEnvironment); err != nil {
			fmt.Fprintf(stderr, "gateway deployment: %v\n", err)
			return 1
		}
		if spec.SIWCConfig != "" {
			attachment, err := siwcAttachment(effective)
			if err == nil {
				err = prepareGatewaySIWC(ctx, attachment)
			}
			if err == nil {
				var store *gatewaycontrol.Store
				store, err = configuredGatewayStore(effective)
				if err == nil {
					_, err = runManagedSIWC(ctx, effective, store, siwc.Operation{Action: "sync"})
				}
			}
			if err != nil {
				fmt.Fprintf(stderr, "managed SIWC configuration: %v\n", err)
				return 1
			}
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
		fmt.Fprintln(stderr, "unsupported gateway lifecycle operation")
		return 2
	}
}


func addGatewayCoreCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) *cobra.Command {
	return gatewaycmd.Register(root, stdout, stderr, load, status, gatewaycmd.Handlers{
		Status: runGatewayStatus,
		Lifecycle: executeGatewayLifecycle,
		Credential: executeGatewayCredential,
	})
}

// Test-visible input seam preserves offline transcript injection.
var gatewayResponsesInput io.Reader = os.Stdin

func verifyGatewayResponsesStream(args []string, stdout, stderr io.Writer) int {
	return gatewayops.VerifyResponsesStream(args, stdout, stderr, gatewayResponsesInput)
}
