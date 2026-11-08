package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/workload"
)

var ensureScopedExistingWorkload = native.EnsureExistingLocalInstance

// runScopedWorkloadApply uses the manual lifecycle authority contract. The
// cooperative lease is not a fence against independent Incus administrators.
func runScopedWorkloadApply(ctx context.Context, project, id string, out, stderr io.Writer, cfg *config.Config) int {
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
	release, err := acquireScopedImageMutationLease(scope)
	if err != nil {
		return fail("project writer lease unavailable; no instance operation attempted")
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var state string
	check := func() error {
		if err := requireNoKnownLifecycleUnits(ctx); err != nil {
			return err
		}
		_, instances, err := readScopedManagedInstances(ctx, cfg, project)
		if err != nil {
			return err
		}
		state, err = findScopedManagedInstance(instances, spec.Name, "workload", "agent-workload", spec.ID)
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
	result, err := ensureScopedExistingWorkload(ctx, intent, func(context.Context) error { return check() })
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
