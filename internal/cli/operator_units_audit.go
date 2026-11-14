package cli

import (
	"context"
	"io"
	"os"
	"os/exec"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
)

// These are the known Aginctus units that can import images or reconcile
// managed resources. Host firewall-only units are deliberately excluded:
// they remain host-owned even when a user owns the workload lifecycle.
var operatorLifecycleUnits = []string{
	"aginctus-image-herdr-client.service",
	"aginctus-image-opencode-workload.service",
	"aginctus-image-update.service",
	"aginctus-reconcile.service",
}

// Query only selected, nonsensitive systemd properties. Never return unit
// environment, command lines, or raw subprocess errors to the operator.
var inspectOperatorUnit = func(ctx context.Context, manager, unit string) ([]byte, error) {
	return exec.CommandContext(ctx, "systemctl", "--"+manager, "show",
		"--no-pager", "--property=LoadState", "--property=ActiveState",
		"--property=UnitFileState", unit).Output()
}


type unitEvidence = operatorops.UnitEvidence
const (
	unitAbsent unitEvidence = operatorops.UnitAbsent
	unitPresent unitEvidence = operatorops.UnitPresent
	unitActive unitEvidence = operatorops.UnitActive
	unitUnknown unitEvidence = operatorops.UnitUnknown
)

func classifyOperatorUnit(data []byte) unitEvidence {
	return operatorops.ClassifyUnit(data)
}

func inspectOperatorUnitEvidence(ctx context.Context, manager, unit string) unitEvidence {
	return operatorops.InspectUnitEvidence(ctx, manager, unit, inspectOperatorUnit)
}

func parseOperatorUnitsAuditArgs(args []string) (string, bool) {
	return operatorops.ParseUnitsAuditArgs(args)
}

func runOperatorUnitsAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return operatorops.RunUnitsAudit(ctx, args, stdout, stderr, os.Geteuid, operatorLifecycleUnits, inspectOperatorUnit)
}

func renderOperatorUnitsAudit(ctx context.Context, owner string, stdout, stderr io.Writer) int {
	return operatorops.RenderUnitsAudit(ctx, owner, stdout, stderr, operatorLifecycleUnits, inspectOperatorUnit)
}
