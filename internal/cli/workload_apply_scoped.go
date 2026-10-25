package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
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

func runScopedWorkloadApplyRender(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	project, rest, err := parseScopedInstanceStatusArgs(args)
	if err != nil || len(rest) != 2 || rest[0] == "" || rest[1] != "--render" {
		fmt.Fprintln(stderr, "Usage: aginctus workload apply --project NAME WORKLOAD-ID --render")
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
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
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
