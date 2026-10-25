package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/workload"
)

// These are opt-in, existing-instance-only operations. Never forward
// project-scoped transitions through the v0.1 default-project runner.
var startScopedWorkloadIncus = func(ctx context.Context, scope incusscope.ProjectScope, instance string) error {
	return exec.CommandContext(ctx, "incus", scopedWorkloadStartArgs(scope, instance)...).Run()
}

func scopedWorkloadStartArgs(scope incusscope.ProjectScope, instance string) []string {
	return []string{"--force-local", "--project", scope.Name, "start", instance}
}

// Stop is separate from the legacy v0.1 default-project transition and uses
// the same explicit local-project argument ordering as start.
var stopScopedWorkloadIncus = func(ctx context.Context, scope incusscope.ProjectScope, instance string) error {
	return exec.CommandContext(ctx, "incus", scopedWorkloadStopArgs(scope, instance)...).Run()
}

func scopedWorkloadStopArgs(scope incusscope.ProjectScope, instance string) []string {
	return []string{"--force-local", "--project", scope.Name, "stop", instance}
}

// A manual transition must not race known installed system/current-user
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
	return runScopedWorkloadTransition(ctx, args, out, stderr, cfg, "start", startScopedWorkloadIncus)
}

func runScopedWorkloadStop(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return runScopedWorkloadTransition(ctx, args, out, stderr, cfg, "stop", stopScopedWorkloadIncus)
}

// Both operations require an explicit project and a confirmed single ID.
// Read/verify, mutation and postflight run under one cooperative project
// lease, but independent Incus clients are still not fenced by that lease.
func runScopedWorkloadTransition(
	ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config,
	action string, mutate func(context.Context, incusscope.ProjectScope, string) error,
) int {
	project, rest, err := parseScopedInstanceStatusArgs(args)
	if err != nil || len(rest) != 2 || rest[0] == "" || rest[1] != "--confirm-"+action {
		fmt.Fprintf(stderr, "Usage: aginctus workload %s --project NAME WORKLOAD-ID --confirm-%s\n", action, action)
		return 2
	}
	required, completed := "stopped", "running"
	if action == "stop" {
		required, completed = "running", "stopped"
	}
	prefix := "workload " + action + ": "
	spec, err := workload.FromConfig(cfg, rest[0])
	if err != nil {
		fmt.Fprintln(stderr, prefix+"invalid selected workload configuration")
		return 1
	}
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		fmt.Fprintln(stderr, prefix+"requested project must match incus.project")
		return 2
	}
	// Share the existing cooperative per-project writer lease with the
	// opt-in image importers. Legacy v0.1 and external Incus clients do
	// not honor it, so the checks below must still fail closed.
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		fmt.Fprintln(stderr, prefix+"project writer lease unavailable; no instance transition attempted")
		return 1
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := requireNoKnownLifecycleUnits(ctx); err != nil {
		fmt.Fprintln(stderr, prefix+"known or unverified lifecycle controller; refusing manual start")
		return 1
	}
	_, instances, err := readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, prefix+"owned local project inventory unverified")
		return 1
	}
	state, err := findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil {
		fmt.Fprintln(stderr, prefix+"instance ownership unverified")
		return 1
	}
	switch {
	case strings.EqualFold(state, completed):
		fmt.Fprintf(out, "workload %q: already %s in project %q\n", spec.ID, completed, project)
		return 0
	case !strings.EqualFold(state, required):
		fmt.Fprintln(stderr, prefix+"a verified "+required+" instance is required; instance creation/removal is not supported")
		return 1
	}

	// Refuse changing identities, policy, state or controller evidence.
	// These observations happen immediately before the one scoped write;
	// they are not an Incus transaction or a globally fenced lock.
	if err := requireNoKnownLifecycleUnits(ctx); err != nil {
		fmt.Fprintln(stderr, prefix+"lifecycle authority changed or is unverified")
		return 1
	}
	_, instances, err = readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, prefix+"project or instance inventory changed before mutation")
		return 1
	}
	state, err = findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil || !strings.EqualFold(state, required) {
		fmt.Fprintln(stderr, prefix+"instance changed before mutation; no transition attempted")
		return 1
	}
	if err := mutate(ctx, scope, spec.Name); err != nil {
		fmt.Fprintln(stderr, prefix+"scoped Incus transition failed; inspect local project before retrying")
		return 1
	}
	_, instances, err = readScopedManagedInstances(ctx, cfg, project)
	if err != nil {
		fmt.Fprintln(stderr, prefix+"post-transition project inventory unverified; inspect before retrying")
		return 1
	}
	state, err = findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
	if err != nil || !strings.EqualFold(state, completed) {
		fmt.Fprintln(stderr, prefix+"expected state not verified; inspect before retrying")
		return 1
	}
	verb := "started"
	if action == "stop" {
		verb = "stopped"
	}
	fmt.Fprintf(out, "workload %q: %s in project %q\n", spec.ID, verb, project)
	return 0
}
