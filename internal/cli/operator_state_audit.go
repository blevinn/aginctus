package cli

import (
	"io"
	"os"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/filestate"
)

const operatorSystemStateRoot = operatorops.SystemStateRoot
var operatorGatewayID = operatorops.GatewayIDPattern

type gatewayStateEvidence = operatorops.GatewayStateEvidence

const (
	gatewayStateAbsent gatewayStateEvidence = operatorops.GatewayStateAbsent
	gatewayStatePresent gatewayStateEvidence = operatorops.GatewayStatePresent
	gatewayStateIncomplete gatewayStateEvidence = operatorops.GatewayStateIncomplete
	gatewayStateUnverified gatewayStateEvidence = operatorops.GatewayStateUnverified
	gatewayStateUnsafe gatewayStateEvidence = operatorops.GatewayStateUnsafe
)

func inspectGatewayState(root, gatewayID string, expectedUID uint32) gatewayStateEvidence {
	return operatorops.InspectGatewayState(root, gatewayID, expectedUID)
}

func auditOperatorGatewayState(owner, systemRoot, userRoot, gatewayID string, systemUID, userUID uint32) (gatewayStateEvidence, gatewayStateEvidence, bool) {
	return operatorops.AuditGatewayState(owner, systemRoot, userRoot, gatewayID, systemUID, userUID)
}

func runOperatorStateAudit(args []string, stdout, stderr io.Writer, effective *config.Config) int {
	return operatorops.RunStateAudit(args, stdout, stderr, effective, os.Geteuid, filestate.StateRoot)
}

func renderOperatorStateAudit(owner, systemRoot, userRoot, gatewayID string, systemUID, userUID uint32, stdout, stderr io.Writer) int {
	return operatorops.RenderStateAudit(owner, systemRoot, userRoot, gatewayID, systemUID, userUID, stdout, stderr)
}
