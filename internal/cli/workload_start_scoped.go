package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/workload"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// This is an opt-in, existing-instance-only operation. Never forward a
// scoped start through the v0.1 default-project lifecycle runner.
var startScopedWorkloadIncus = func(ctx context.Context, scope incusscope.ProjectScope, instance string) error {
	return exec.CommandContext(ctx, "incus", scopedWorkloadStartArgs(scope, instance)...).Run()
}

func scopedWorkloadStartArgs(scope incusscope.ProjectScope, instance string) []string {
	return []string{"--force-local", "--project", scope.Name, "start", instance}
}

// A manual start must not race any of the known installed system/current-user
// Aginctus reconcilers. This is only a conservative denial gate: an absent
// allowlist is NOT evidence of exclusive control over Incus.
func requireNoKnownLifecycleUnits(ctx context.Context) error {
	for _, unit := range operatorLifecycleUnits {
		for _, manager := range []string{"system", "user"} {
			if inspectOperatorUnitEvidence(ctx, manager, unit) != unitAbsent {
				return fmt.Errorf("known lifecycle authority is present or unverified")
			}
		}
	}
	return nil
}

func runScopedWorkloadStart(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	project, rest, err := parseScopedInstanceStatusArgs(args)
	if err != nil || len(rest) != 2 || rest[0] == "" || rest[1] != "--confirm-start" {
		fmt.Fprintln(stderr, "Usage: aginctus workload start --project NAME WORKLOAD-ID --confirm-start")
		return 2
	}
	spec, err := workload.FromConfig(cfg, rest[0])
	if err != nil {
		fmt.Fprintln(stderr, "workload start: invalid selected workload configuration")
		return 1
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, "workload start: requested project must match incus.project")
		return 2
	}
	// Share the existing cooperative per-project writer lease with the
	// opt-in image importers. Legacy v0.1 and external Incus clients do
	// not honor it, so the checks below must still fail closed.
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		fmt.Fprintln(stderr, "workload start: project writer lease unavailable; no instance transition attempted")
		return 1
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := requireNoKnownLifecycleUnits(ctx); err != nil {
		fmt.Fprintln(stderr, "workload start: known or unverified lifecycle controller; refusing manual start")
		return 1
	}
	_, instances, err := readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, "workload start: owned local project inventory unverified")
		return 1
	}
	state, err := findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil {
		fmt.Fprintln(stderr, "workload start: instance ownership unverified")
		return 1
	}
	switch {
	case strings.EqualFold(state, "running"):
		fmt.Fprintf(out, "workload %q: already running in project %q\n", spec.ID, project)
		return 0
	case !strings.EqualFold(state, "stopped"):
		fmt.Fprintln(stderr, "workload start: a verified stopped instance is required; creation is not supported")
		return 1
	}

	// Refuse changing identities, policy, state or controller evidence.
	// These observations happen immediately before the one scoped write;
	// they are not an Incus transaction or a globally fenced lock.
	if err := requireNoKnownLifecycleUnits(ctx); err != nil {
		fmt.Fprintln(stderr, "workload start: lifecycle authority changed or is unverified")
		return 1
	}
	_, instances, err = readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, "workload start: project or instance inventory changed before mutation")
		return 1
	}
	state, err = findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil || !strings.EqualFold(state, "stopped") {
		fmt.Fprintln(stderr, "workload start: instance changed before mutation; no start attempted")
		return 1
	}
	if err := startScopedWorkloadIncus(ctx, scope, spec.Name); err != nil {
		fmt.Fprintln(stderr, "workload start: scoped Incus start failed; inspect local project before retrying")
		return 1
	}
	_, instances, err = readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, "workload start: post-start project inventory unverified; inspect before retrying")
		return 1
	}
	state, err = findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil || !strings.EqualFold(state, "running") {
		fmt.Fprintln(stderr, "workload start: running state not verified; inspect before retrying")
		return 1
	}
	fmt.Fprintf(out, "workload %q: started in project %q\n", spec.ID, project)
	return 0
}
