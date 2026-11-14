package cli

import (
	"context"
	"io"

	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
	"github.com/blevinn/aginctus/internal/config"
)

// Preserve the existing Incus guest-exec injection points for CLI tests.
var herdrHostTrustGuestExec = herdrAuthorizationGuestExec
var managedSSHHostAlias = herdrops.ManagedSSHHostAlias

func parseWorkloadHostKey(raw string) (string, error) { return herdrops.ParseWorkloadHostKey(raw) }
func parseManagementAddress(raw string) (string, error) { return herdrops.ParseManagementAddress(raw) }
func pinHerdrHost(ctx context.Context, cfg *config.Config, id string) error {
	return herdrops.PinHost(ctx, cfg, id, herdrActions())
}
func runHerdrPinHost(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return herdrops.RunPinHost(ctx, args, stdout, stderr, cfg, herdrActions())
}
