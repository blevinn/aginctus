package operatorops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// These handlers use explicit observation and create capabilities; not-visible
// never authorizes adoption, and create must reverify exact project ownership.
type ProjectPlanReport struct {
	Project       string            `json:"project"`
	Status        string            `json:"status"`
	DesiredConfig map[string]string `json:"desiredConfig"`
}

func RunProjectPlan(ctx context.Context, args []string, stdout, stderr io.Writer, inspect func(context.Context, ...string) ([]byte, error), maxBytes int) int {
	scope, err := ParseProjectArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus operator project-plan --project NAME (not default)")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Use the same GET-only subprocess probe as operator preflight.
	// Suppress its raw stderr; remotes may expose sensitive details.
	raw, err := inspect(ctx, "project", "list", "--format", "json")
	if err != nil || len(raw) > maxBytes {
		fmt.Fprintln(stderr, "operator project-plan: unable to verify Incus project enumeration")
		return 1
	}
	status, err := AssessObservedProjects(scope, raw)
	if err != nil {
		fmt.Fprintln(stderr, "operator project-plan: invalid or ambiguous Incus project enumeration")
		return 1
	}
	report := ProjectPlanReport{
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

type ProjectEnsureReport struct {
	Project string `json:"project"`
	Result  string `json:"result"`
	Status  string `json:"status"`
}

func ParseProjectEnsureArgs(args []string) (incusscope.ProjectScope, error) {
	if len(args) < 2 || args[len(args)-1] != "--confirm-create" {
		return incusscope.ProjectScope{}, fmt.Errorf("explicit --confirm-create is required")
	}
	return ParseProjectArgs(args[:len(args)-1])
}

func ReadLocalProjectAssessment(ctx context.Context, scope incusscope.ProjectScope, inspect func(context.Context) ([]byte, error), maxBytes int) (string, error) {
	raw, err := inspect(ctx)
	if err != nil || len(raw) > maxBytes {
		return "", fmt.Errorf("unable to verify local Incus project enumeration")
	}
	return AssessObservedProjects(scope, raw)
}

// This is create-only provisioning, NOT a general-purpose reconciler. A
// not-visible result must not be interpreted as proof that the name is free:
// only Incus's atomic create operation can settle that race. Reinspection
// after a successful create is required before reporting success.
func RunProjectEnsure(ctx context.Context, args []string, stdout, stderr io.Writer, read func(context.Context, incusscope.ProjectScope) (string, error), create func(context.Context, incusscope.ProjectScope) error) int {
	scope, err := ParseProjectEnsureArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, "Usage: aginctus operator project-ensure --project NAME --confirm-create")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	status, err := read(ctx, scope)
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
		if err := create(ctx, scope); err != nil {
			fmt.Fprintln(stderr, "operator project-ensure: native project creation refused or failed; project may exist; inspect exact project state before retry; no automatic rollback")
			return 1
		}
		result = "created"
		status, err = read(ctx, scope)
		if err != nil || status != string(incusscope.ProjectCompatible) {
			fmt.Fprintln(stderr, "operator project-ensure: create returned success but exact ownership/policy verification failed; project may exist; no automatic rollback")
			return 1
		}
	default:
		fmt.Fprintln(stderr, "operator project-ensure: project is foreign-owned or has policy drift; refusing modification")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(ProjectEnsureReport{
		Project: scope.Name, Result: result, Status: status,
	}); err != nil {
		fmt.Fprintln(stderr, "operator project-ensure: unable to write confirmation")
		return 1
	}
	return 0
}
