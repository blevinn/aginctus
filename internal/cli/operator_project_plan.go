package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

const maxProjectListBytes = 4 << 20

type projectPlanReport struct {
	Project       string            `json:"project"`
	Status        string            `json:"status"`
	DesiredConfig map[string]string `json:"desiredConfig"`
}

// A project list is a read-only observation, not evidence of global
// absence: confined Incus identities may see only their own projects.
// Keep plan/output DTOs and legacy call points local while policy is shared.
type observedProject = operatorops.ObservedProject

func parseOperatorProjectPlanArgs(args []string) (incusscope.ProjectScope, error) {
	return operatorops.ParseProjectArgs(args)
}

func assessObservedProjectList(scope incusscope.ProjectScope, raw []byte) (string, error) {
	return operatorops.AssessObservedProjects(scope, raw)
}

func runOperatorProjectPlan(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	scope, err := parseOperatorProjectPlanArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus operator project-plan --project NAME (not default)")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Use the same GET-only subprocess probe as operator preflight.
	// Suppress its raw stderr; remotes may expose sensitive details.
	raw, err := inspectOperatorIncus(ctx, "project", "list", "--format", "json")
	if err != nil || len(raw) > maxProjectListBytes {
		fmt.Fprintln(stderr, "operator project-plan: unable to verify Incus project enumeration")
		return 1
	}
	status, err := assessObservedProjectList(scope, raw)
	if err != nil {
		fmt.Fprintln(stderr, "operator project-plan: invalid or ambiguous Incus project enumeration")
		return 1
	}
	report := projectPlanReport{
		Project: scope.Name, Status: status, DesiredConfig: scope.DesiredConfig(),
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, "operator project-plan: unable to write project plan")
		return 1
	}
	if status != string(incusscope.ProjectCompatible) {
		fmt.Fprintln(stderr, "operator project-plan: project not verified as owned with exact feature policy; no changes made")
		return 1
	}
	return 0
}
