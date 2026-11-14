package cli

import (
	"context"
	"io"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

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
	return operatorops.RunProjectPlan(ctx, args, stdout, stderr, inspectOperatorIncus, maxProjectListBytes)
}
