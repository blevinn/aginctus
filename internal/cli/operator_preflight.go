package cli

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
)

// This runner reads only the requested Incus endpoints; it never switches
// the ambient project or obtains sudo privileges.
var inspectOperatorIncus = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "incus", args...).Output()
}

func runOperatorPreflight(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return operatorops.Preflight(ctx, args, stdout, stderr, operatorops.PreflightActions{
		Read: readOperatorAPI,
		Identity: inspectOperatorIdentity,
		Inspect: inspectOperatorIncus,
		Confinement: func(ctx context.Context, project string) operatorops.ProjectConfinementEvidence {
			return inspectProjectConfinement(ctx, project)
		},
		ReadFailure: operatorReadFailure,
	})
}
