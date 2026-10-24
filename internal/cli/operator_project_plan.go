package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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
type observedProject struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}

func parseOperatorProjectPlanArgs(args []string) (incusscope.ProjectScope, error) {
	project := ""
	switch {
	case len(args) == 1 && strings.HasPrefix(args[0], "--project="):
		project = strings.TrimPrefix(args[0], "--project=")
	case len(args) == 2 && args[0] == "--project":
		project = args[1]
	default:
		return incusscope.ProjectScope{}, fmt.Errorf("explicit --project NAME is required")
	}
	return incusscope.NewProjectScope(project)
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
	var projects []observedProject
	if err := json.Unmarshal(raw, &projects); err != nil || projects == nil {
		fmt.Fprintln(stderr, "operator project-plan: invalid Incus project enumeration")
		return 1
	}
	status := "not-visible"
	matches := 0
	for _, project := range projects {
		if project.Name != scope.Name {
			continue
		}
		matches++
		status = string(scope.AssessProject(project.Name, project.Config))
	}
	if matches > 1 {
		fmt.Fprintln(stderr, "operator project-plan: ambiguous project enumeration")
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
