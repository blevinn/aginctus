package cli

import (
	"context"
	"io"
	"os"

	gatewaycmd "github.com/blevinn/aginctus/internal/cli/commands/gateway"
	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/config"
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
	return gatewayops.ExecuteLifecycle(ctx, action, force, stdout, stderr, effective, gatewayops.LifecycleActions{
		ConfiguredSpec: configuredGatewaySpec,
		Status: runGatewayStatus,
		SyncSIWC: syncGatewaySIWC,
	})
}

func addGatewayCoreCommands(
	root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) *cobra.Command {
	return gatewaycmd.Register(root, stdout, stderr, load, status, gatewaycmd.Handlers{
		Status:     runGatewayStatus,
		Lifecycle:  executeGatewayLifecycle,
		Credential: executeGatewayCredential,
	})
}

// Test-visible input seam preserves offline transcript injection.
var gatewayResponsesInput io.Reader = os.Stdin

func verifyGatewayResponsesStream(args []string, stdout, stderr io.Writer) int {
	return gatewayops.VerifyResponsesStream(args, stdout, stderr, gatewayResponsesInput)
}
