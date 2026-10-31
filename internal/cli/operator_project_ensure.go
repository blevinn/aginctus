package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

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

type operatorProjectEnsureReport struct {
	Project string `json:"project"`
	Result  string `json:"result"`
	Status  string `json:"status"`
}

func parseOperatorProjectEnsureArgs(args []string) (incusscope.ProjectScope, error) {
	if len(args) < 2 || args[len(args)-1] != "--confirm-create" {
		return incusscope.ProjectScope{}, fmt.Errorf("explicit --confirm-create is required")
	}
	return parseOperatorProjectPlanArgs(args[:len(args)-1])
}

func readLocalProjectAssessment(ctx context.Context, scope incusscope.ProjectScope) (string, error) {
	raw, err := inspectLocalProjects(ctx)
	if err != nil || len(raw) > maxProjectListBytes {
		return "", fmt.Errorf("unable to verify local Incus project enumeration")
	}
	return assessObservedProjectList(scope, raw)
}

// This is create-only provisioning, NOT a general-purpose reconciler. A
// not-visible result must not be interpreted as proof that the name is free:
// only Incus's atomic create operation can settle that race. Reinspection
// after a successful create is required before reporting success.
func runOperatorProjectEnsure(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	scope, err := parseOperatorProjectEnsureArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus operator project-ensure --project NAME --confirm-create")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	status, err := readLocalProjectAssessment(ctx, scope)
	if err != nil {
		fmt.Fprintln(stderr, "operator project-ensure: local project state unverified; no creation attempted")
		return 1
	}
	result := "existing"
	switch status {
	case string(incusscope.ProjectCompatible):
		// Idempotent reuse with NO writes.
	case "not-visible":
		// A restricted enumeration may omit an existing project. Never
		// assume absence, adopt it, or try to repair foreign metadata.
		// Explicit create is safe only because Incus itself rejects a
		// same-name existing project atomically.
		if err := createLocalProject(ctx, scope); err != nil {
			fmt.Fprintln(stderr, "operator project-ensure: native project creation refused or failed; project may exist; inspect exact project state before retry; no automatic rollback")
			return 1
		}
		result = "created"
		status, err = readLocalProjectAssessment(ctx, scope)
		if err != nil || status != string(incusscope.ProjectCompatible) {
			fmt.Fprintln(stderr, "operator project-ensure: create returned success but exact ownership/policy verification failed; project may exist; no automatic rollback")
			return 1
		}
	default:
		fmt.Fprintln(stderr, "operator project-ensure: project is foreign-owned or has policy drift; refusing modification")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(operatorProjectEnsureReport{
		Project: scope.Name, Result: result, Status: status,
	}); err != nil {
		fmt.Fprintln(stderr, "operator project-ensure: unable to write confirmation")
		return 1
	}
	return 0
}
