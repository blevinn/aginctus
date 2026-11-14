package workloadops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/workload"
)

// ScopedObservation preserves the distinction between project-inventory and
// workload-ownership failures while keeping CLI-private Incus DTOs local.
type ScopedObservation struct {
	State string
	InventoryErr error
	LookupErr error
}

type ScopedActions struct {
	Parse func([]string) (string, []string, error)
	Observe func(context.Context, *config.Config, string, string, string, string, string) ScopedObservation
	Lease func(incusscope.ProjectScope) (func(), error)
	GuardControllers func(context.Context) error
	Plan func(context.Context, native.InstanceIntent) (native.InstancePlan, error)
	Ensure func(context.Context, native.InstanceIntent, func(context.Context) error) (native.InstanceEnsureResult, error)
	Transition func(context.Context, native.InstanceLifecycleIntent) error
}

// Scoped previews never authorize mutation or execute the plan.
// The envelope intentionally is not an executable orchestration document.
// It is a review artifact, NOT proof of authority to execute this plan.
// Never pipe it to legacy top-level 'aginctus apply --from'.
type ScopedApplyPreview struct {
	Kind            string             `json:"kind"`
	Project         string             `json:"project"`
	WorkloadID      string             `json:"workloadId"`
	ObservedState   string             `json:"observedState"`
	WriteAuthorized bool               `json:"writeAuthorized"`
	Plan            orchestration.Plan `json:"plan"`
}

type ScopedApplyDiff struct {
	Kind            string `json:"kind"`
	Project         string `json:"project"`
	WorkloadID      string `json:"workloadId"`
	ObservedState   string `json:"observedState"`
	Action          string `json:"action"`
	ChangeCount     int    `json:"changeCount"`
	WriteAuthorized bool   `json:"writeAuthorized"`
}

func ClassifyScopedDiff(preview native.InstancePlan, identity native.InstanceIdentity) (string, int, error) {
	// No raw diff values, notes, resource names, or upstream error detail
	// are returned. Treat creation, replacement, unknown or multi-resource
	// previews as errors, even though planning itself never mutates.
	if preview.Identity != identity || preview.WriteAuthorized {
		return "", 0, fmt.Errorf("ambiguous scoped workload diff")
	}
	switch preview.Action {
	case native.Unchanged:
		if len(preview.Changes) != 0 {
			return "", 0, fmt.Errorf("inconsistent scoped workload diff")
		}
		return "unchanged", 0, nil
	case native.Update:
		return "would-update", len(preview.Changes), nil
	default:
		return "", 0, fmt.Errorf("create, replace or unsupported scoped diff")
	}
}

func RunScopedApplyRender(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, actions ScopedActions) int {
	project, rest, err := actions.Parse(args)
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
	observed := actions.Observe(ctx, cfg, project, spec.Name, "workload", "agent-workload", spec.ID)
	if observed.InventoryErr != nil {
		fmt.Fprintln(stderr, "workload apply preview: owned local project inventory unverified")
		return 1
	}
	state, err := observed.State, observed.LookupErr
	if err != nil || (!strings.EqualFold(state, "running") && !strings.EqualFold(state, "stopped")) {
		fmt.Fprintln(stderr, "workload apply preview: requires an existing, verified running or stopped owned workload")
		return 1
	}
	if rest[1] == "--dry-run" {
		// This path intentionally calls only the native planner. The
		// preview is never passed to the orchestration Execute engine.
		intent, err := spec.NativeExistingIntent(scope)
		if err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: cannot build safe native intent")
			return 1
		}
		preview, err := actions.Plan(ctx, intent)
		if err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: native local-project diff unavailable or rejected")
			return 1
		}
		action, changes, err := ClassifyScopedDiff(preview, intent.Identity)
		if err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: native diff proposes unsafe or ambiguous action")
			return 1
		}
		// Refuse success if the observed owned project or workload changed
		// while the planner was reading it. No write authority is implied.
		refreshed := actions.Observe(ctx, cfg, project, spec.Name, "workload", "agent-workload", spec.ID)
		if refreshed.InventoryErr != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: project or instance state changed during planning")
			return 1
		}
		after, err := refreshed.State, refreshed.LookupErr
		if err != nil || after != state {
			fmt.Fprintln(stderr, "workload apply dry-run: instance identity or state changed during planning")
			return 1
		}
		report := ScopedApplyDiff{
			Kind: "ScopedWorkloadApplyDiff", Project: project, WorkloadID: spec.ID,
			ObservedState: state, Action: action, ChangeCount: changes, WriteAuthorized: false,
		}
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			fmt.Fprintln(stderr, "workload apply dry-run: unable to encode report")
			return 1
		}
		return 0
	}
	plan, err := spec.PlanScopedExisting(scope)
	if err != nil {
		fmt.Fprintln(stderr, "workload apply preview: cannot build safe instance-only plan")
		return 1
	}
	report := ScopedApplyPreview{
		Kind: "ScopedWorkloadApplyPreview", Project: project, WorkloadID: spec.ID,
		ObservedState: state, WriteAuthorized: false, Plan: plan,
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintln(stderr, "workload apply preview: unable to encode report")
		return 1
	}
	return 0
}

// Confirmed native reconciliation rechecks the same bounded local project
// under a cooperative project lease and independently guarded native write.
func RunScopedApplyConfirm(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, actions ScopedActions) int {
	project, rest, err := actions.Parse(args)
	if err != nil || len(rest) != 2 || rest[0] == "" || rest[1] != "--confirm-apply" {
		fmt.Fprintln(stderr, "Usage: aginctus workload apply --project NAME WORKLOAD-ID --confirm-apply")
		return 2
	}
	return RunScopedApply(ctx, project, rest[0], out, stderr, cfg, actions)
}

// runScopedWorkloadApply uses the manual lifecycle authority contract. The
// cooperative lease is not a fence against independent Incus administrators.
func RunScopedApply(ctx context.Context, project, id string, out, stderr io.Writer, cfg *config.Config, actions ScopedActions) int {
	fail := func(message string) int { fmt.Fprintln(stderr, "workload apply: "+message); return 1 }
	scope, err := cfg.IncusProjectScope()
	if err != nil || scope.Name != project {
		return fail("requested project must match incus.project")
	}
	spec, err := workload.FromConfig(cfg, id)
	if err != nil {
		return fail("invalid selected workload configuration")
	}
	intent, err := spec.NativeExistingIntent(scope)
	if err != nil {
		return fail("cannot build safe native intent")
	}
	release, err := actions.Lease(scope)
	if err != nil {
		return fail("project writer lease unavailable; no instance operation attempted")
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var state string
	check := func() error {
		if err := actions.GuardControllers(ctx); err != nil {
			return err
		}
		observed := actions.Observe(ctx, cfg, project, spec.Name, "workload", "agent-workload", spec.ID)
		if observed.InventoryErr != nil {
			return observed.InventoryErr
		}
		state, err = observed.State, observed.LookupErr
		if err != nil {
			return err
		}
		if state != "Running" && state != "Stopped" {
			return fmt.Errorf("existing owned workload required")
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return fail("existing owned local workload or controller authority unverified")
	}
	if err := check(); err != nil {
		return fail("project, workload or controller changed before execution")
	}
	result, err := actions.Ensure(ctx, intent, func(context.Context) error { return check() })
	if err != nil || !result.Verified {
		return fail("native ensure failed; inspect selected instance before retry; no automatic rollback")
	}
	if err := check(); err != nil {
		return fail("post-apply authority or inventory unverified; inspect before retry")
	}
	if state != "Running" {
		return fail("post-apply running state unverified; inspect before retry")
	}
	fmt.Fprintf(out, "workload %q: reconciled existing instance in project %q\n", spec.ID, project)
	return 0
}

// Start and stop only transition an already-owned instance in the exact scope.
func RunScopedWorkloadStart(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, actions ScopedActions) int {
	return RunScopedWorkloadTransition(ctx, args, out, stderr, cfg, "start", native.InstanceRunning, actions)
}

func RunScopedWorkloadStop(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, actions ScopedActions) int {
	return RunScopedWorkloadTransition(ctx, args, out, stderr, cfg, "stop", native.InstanceStopped, actions)
}

// Both operations require an explicit project and a confirmed single ID.
// Read/verify, mutation and postflight run under one cooperative project
// lease, but independent Incus clients are still not fenced by that lease.
func RunScopedWorkloadTransition(
	ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config,
	action string, target native.InstanceState, actions ScopedActions,
) int {
	project, rest, err := actions.Parse(args)
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
	release, err := actions.Lease(scope)
	if err != nil {
		fmt.Fprintln(stderr, prefix+"project writer lease unavailable; no instance transition attempted")
		return 1
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := actions.GuardControllers(ctx); err != nil {
		fmt.Fprintln(stderr, prefix+"known or unverified lifecycle controller; refusing manual transition")
		return 1
	}
	observed := actions.Observe(ctx, cfg, project, spec.Name, "workload", "agent-workload", spec.ID)
	if observed.InventoryErr != nil {
		fmt.Fprintln(stderr, prefix+"owned local project inventory unverified")
		return 1
	}
	state, err := observed.State, observed.LookupErr
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
	if err := actions.GuardControllers(ctx); err != nil {
		fmt.Fprintln(stderr, prefix+"lifecycle authority changed or is unverified")
		return 1
	}
	observed = actions.Observe(ctx, cfg, project, spec.Name, "workload", "agent-workload", spec.ID)
	if observed.InventoryErr != nil {
		fmt.Fprintln(stderr, prefix+"project or instance inventory changed before mutation")
		return 1
	}
	state, err = observed.State, observed.LookupErr
	if err != nil || !strings.EqualFold(state, required) {
		fmt.Fprintln(stderr, prefix+"instance changed before mutation; no transition attempted")
		return 1
	}
	if err := actions.Transition(ctx, native.InstanceLifecycleIntent{
		Scope:      scope,
		Name:       spec.Name,
		Resource:   "workload",
		Role:       "agent-workload",
		WorkloadID: spec.ID,
		State:      target,
	}); err != nil {
		fmt.Fprintln(stderr, prefix+"scoped Incus transition failed; inspect local project before retrying")
		return 1
	}
	observed = actions.Observe(ctx, cfg, project, spec.Name, "workload", "agent-workload", spec.ID)
	if observed.InventoryErr != nil {
		fmt.Fprintln(stderr, prefix+"post-transition project inventory unverified; inspect before retrying")
		return 1
	}
	state, err = observed.State, observed.LookupErr
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
