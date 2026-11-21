package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/filestate"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// Operator authority probes and project writes retain their independently
// injected CLI capabilities. All operator operation policy remains in
// operations/operator, never in Cobra registration.

type operatorReadStatus = operatorops.ReadStatus

const (
	operatorReadOK          operatorReadStatus = operatorops.ReadOK
	operatorReadDenied      operatorReadStatus = operatorops.ReadDenied
	operatorReadNoIdentity  operatorReadStatus = operatorops.ReadNoIdentity
	operatorReadUnavailable operatorReadStatus = operatorops.ReadUnavailable
	operatorReadNoCLI       operatorReadStatus = operatorops.ReadNoCLI
	operatorReadCanceled    operatorReadStatus = operatorops.ReadCanceled
	operatorReadTimeout     operatorReadStatus = operatorops.ReadTimeout
	operatorReadInvalid     operatorReadStatus = operatorops.ReadInvalid
)

func readOperatorAPI(ctx context.Context, path string) (json.RawMessage, operatorReadStatus) {
	return operatorops.ReadAPI(ctx, path, inspectOperatorIncus)
}
func operatorReadFailure(err error) operatorReadStatus {
	return operatorops.ReadFailure(err)
}
func inspectOperatorIdentity(ctx context.Context, server json.RawMessage, project string) (string, bool) {
	return operatorops.InspectIdentity(ctx, server, project, readOperatorAPI)
}

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

type projectConfinementEvidence = operatorops.ProjectConfinementEvidence

const (
	projectConfinementUnknown projectConfinementEvidence = operatorops.ProjectConfinementUnknown
	projectConfinementUnrestricted projectConfinementEvidence = operatorops.ProjectConfinementUnrestricted
	projectConfinementShared projectConfinementEvidence = operatorops.ProjectConfinementShared
	projectConfinementNetwork projectConfinementEvidence = operatorops.ProjectConfinementNetwork
	projectConfinementCandidate projectConfinementEvidence = operatorops.ProjectConfinementCandidate
)

func classifyProjectConfinement(data json.RawMessage, expectedProject string) projectConfinementEvidence {
	return operatorops.ClassifyProjectConfinement(data, expectedProject)
}

func inspectProjectConfinement(ctx context.Context, project string) projectConfinementEvidence {
	return operatorops.InspectProjectConfinement(ctx, project, readOperatorAPI)
}

// Provisioning is deliberately limited to the LOCAL Incus daemon. A caller's
// ambient remote or selected project must not determine the target.
var inspectLocalProjects = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "incus", "--force-local", "project", "list", "--format", "json").Output()
}

var createLocalProject = func(ctx context.Context, scope incusscope.ProjectScope) error {
	// The CLI confirmation gates this call. Native execution independently
	// revalidates local scope, ownership/policy and absence at the write boundary.
	result, err := native.EnsureLocalProject(ctx, scope, native.ProjectCreateAuthorization{Confirmed: true})
	if err != nil {
		return err
	}
	if !result.Created || !result.Verified {
		return fmt.Errorf("project became visible before creation; no mutation performed")
	}
	return nil
}

type operatorProjectEnsureReport = operatorops.ProjectEnsureReport

func parseOperatorProjectEnsureArgs(args []string) (incusscope.ProjectScope, error) {
	return operatorops.ParseProjectEnsureArgs(args)
}

func readLocalProjectAssessment(ctx context.Context, scope incusscope.ProjectScope) (string, error) {
	return operatorops.ReadLocalProjectAssessment(ctx, scope, inspectLocalProjects, maxProjectListBytes)
}

func runOperatorProjectEnsure(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return operatorops.RunProjectEnsure(ctx, args, stdout, stderr, readLocalProjectAssessment, createLocalProject)
}

const maxProjectListBytes = 4 << 20

type projectPlanReport = operatorops.ProjectPlanReport
type observedProject = operatorops.ObservedProject

func parseOperatorProjectPlanArgs(args []string) (incusscope.ProjectScope, error) {
	return operatorops.ParseProjectArgs(args)
}

func assessObservedProjectList(scope incusscope.ProjectScope, raw []byte) (string, error) {
	return operatorops.AssessObservedProjects(scope, raw)
}

func runOperatorProjectPlan(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runOperatorProjectPlanWithQuery(ctx, args, stdout, stderr, inspectOperatorIncus)
}

// Keep the public operator call unchanged while allowing independent,
// per-invocation project-list readers in negative-security tests. The
// operation owner continues to validate explicit scope and output bounds.
func runOperatorProjectPlanWithQuery(ctx context.Context, args []string, stdout, stderr io.Writer,
	query func(context.Context, ...string) ([]byte, error),
) int {
	return operatorops.RunProjectPlan(ctx, args, stdout, stderr, query, maxProjectListBytes)
}

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

func runOperatorStateAudit(args []string, stdout, stderr io.Writer, effective *config.Config) int {
	return operatorops.RunStateAudit(args, stdout, stderr, effective, os.Geteuid, filestate.StateRoot)
}

func renderOperatorStateAudit(location, root, gatewayID string, uid uint32, stdout, stderr io.Writer) int {
	return operatorops.RenderStateAudit(location, root, gatewayID, uid, stdout, stderr)
}

// These are the known Aginctus units that can import images or reconcile
// managed resources. Host firewall-only units are excluded because they
// belong to separate root host policy, not a competing project owner.
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

func runOperatorUnitsAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return operatorops.RunUnitsAudit(ctx, args, stdout, stderr, os.Geteuid, operatorLifecycleUnits, inspectOperatorUnit)
}

func renderOperatorUnitsAudit(ctx context.Context, stdout, stderr io.Writer) int {
	return operatorops.RenderUnitsAudit(ctx, stdout, stderr, operatorLifecycleUnits, inspectOperatorUnit)
}
