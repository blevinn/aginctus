package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	incusapply "github.com/abiosoft/incus-apply/apply"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
	"github.com/blevinn/aginctus/internal/workload"
)

// The envelope intentionally is not an executable orchestration document.
// It is a review artifact, NOT proof of authority to execute this plan.
// Never pipe it to legacy top-level 'aginctus apply --from'.
type scopedWorkloadApplyPreview struct {
	Kind            string             `json:"kind"`
	Project         string             `json:"project"`
	WorkloadID      string             `json:"workloadId"`
	ObservedState   string             `json:"observedState"`
	WriteAuthorized bool               `json:"writeAuthorized"`
	Plan            orchestration.Plan `json:"plan"`
}

// Injected in hermetic tests: a native diff only, never ExecuteContext.
var planScopedExistingWorkloadDiff = func(ctx context.Context, raw json.RawMessage) (incusapply.Preview, error) {
	return applydriver.New().PlanScopedInstance(ctx, raw)
}

type scopedWorkloadApplyDiff struct {
	Kind            string `json:"kind"`
	Project         string `json:"project"`
	WorkloadID      string `json:"workloadId"`
	ObservedState   string `json:"observedState"`
	Action          string `json:"action"`
	ChangeCount     int    `json:"changeCount"`
	WriteAuthorized bool   `json:"writeAuthorized"`
}

func classifyScopedWorkloadDiff(preview incusapply.Preview) (string, int, error) {
	// No raw diff values, notes, resource names, or upstream error detail
	// are returned. Treat creation, replacement, unknown or multi-resource
	// previews as errors, even though planning itself never mutates.
	if preview.ResourceCount != 1 || len(preview.Groups) != 1 ||
		len(preview.Groups[0].Items) != 1 {
		return "", 0, fmt.Errorf("ambiguous scoped workload diff")
	}
	group := preview.Groups[0]
	switch group.Action {
	case "unchanged":
		return "unchanged", 0, nil
	case "update":
		return "would-update", len(group.Items[0].Changes), nil
	default:
		return "", 0, fmt.Errorf("create, replace or unsupported scoped diff")
	}
}

func runScopedWorkloadApplyRender(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	project, rest, err := parseScopedInstanceStatusArgs(args)
	if err != nil || len(rest) != 2 || rest[0] == "" ||
		(rest[1] != "--render" && rest[1] != "--dry-run") {
		fmt.Fprintln(stderr, "Usage: aginctus workload apply --project NAME WORKLOAD-ID --render|--dry-run")
		return 2
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, "workload apply preview: project must match declared incus.project")
		return 2
	}
	spec, err := workload.FromConfig(cfg, rest[0])
	if err != nil {
		fmt.Fprintln(stderr, "workload apply preview: invalid workload configuration")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	_, instances, err := readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, "workload apply preview: owned local project inventory unverified")
		return 1
	}
	state, err := findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil || (!strings.EqualFold(state, "running") && !strings.EqualFold(state, "stopped")) {
		fmt.Fprintln(stderr, "workload apply preview: requires an existing, verified running or stopped owned workload")
		return 1
	}
	plan, err := spec.PlanScopedExisting(scope)
	if err != nil {
		fmt.Fprintln(stderr, "workload apply preview: cannot build safe instance-only plan")
		return 1
	}
	if rest[1] == "--dry-run" {
		// This path intentionally calls only the native planner. The
		// preview is never passed to the orchestration Execute engine.
		preview, err := planScopedExistingWorkloadDiff(ctx, plan.Steps[0].Configuration)
		if err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: native local-project diff unavailable or rejected")
			return 1
		}
		action, changes, err := classifyScopedWorkloadDiff(preview)
		if err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: native diff proposes unsafe or ambiguous action")
			return 1
		}
		// Refuse success if the observed owned project or workload changed
		// while the planner was reading it. No write authority is implied.
		_, refreshed, err := readScopedManagedInstances(ctx, cfg, project)
		if err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: project or instance state changed during planning")
			return 1
		}
		after, err := findScopedManagedInstance(refreshed, spec.Name, "workload", "agent-workload", spec.ID)
		if err != nil || after != state {
			fmt.Fprintln(stderr, "workload apply dry-run: instance identity or state changed during planning")
			return 1
		}
		report := scopedWorkloadApplyDiff{
			Kind: "ScopedWorkloadApplyDiff", Project: project, WorkloadID: spec.ID,
			ObservedState: state, Action: action, ChangeCount: changes, WriteAuthorized: false,
		}
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: unable to encode report")
			return 1
		}
		return 0
	}
	report := scopedWorkloadApplyPreview{
		Kind: "ScopedWorkloadApplyPreview", Project: project, WorkloadID: spec.ID,
		ObservedState: state, WriteAuthorized: false, Plan: plan,
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, "workload apply preview: unable to encode report")
		return 1
	}
	return 0
}
