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

// Credential volumes are manipulated only through the existing injected
// gateway capabilities; the operation package handles result formatting.
func executeGatewayCredential(ctx context.Context, action string, stdout, stderr io.Writer, effective *config.Config) int {
	return gatewayops.ExecuteCredential(ctx, action, stdout, stderr, effective, gatewayops.CredentialActions{
		Audit: auditGatewayCredentials, Attach: attachGatewayCredentials, Ensure: ensureCredentialStorage,
	})
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
