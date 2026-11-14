package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
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
