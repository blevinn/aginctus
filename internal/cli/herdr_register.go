package cli

import (
	"context"
	"io"

	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
	"github.com/blevinn/aginctus/internal/config"
)

// The guest transport is intentionally independently replaceable for tests.
var herdrMachineGuestExec = herdrAuthorizationGuestExec

type savedHerdrMachine = herdrops.SavedHerdrMachine
func parseSavedHerdrMachines(raw string) ([]savedHerdrMachine, error) {
	return herdrops.ParseSavedMachines(raw)
}
func verifySavedHerdrMachine(machines []savedHerdrMachine, label, target string) (bool, error) {
	return herdrops.VerifySavedMachine(machines, label, target)
}
func registerHerdrMachine(ctx context.Context, cfg *config.Config, id string) error {
	return herdrops.RegisterMachine(ctx, cfg, id, herdrActions())
}
func runHerdrRegister(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return herdrops.RunRegister(ctx, args, stdout, stderr, cfg, herdrActions())
}
