package workloadops

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/workload"
)

type Options struct {
	DryRun bool
	Force bool
}

// Configured runs the selected workload operations using only caller-provided
// lifecycle capabilities, preserving the existing all-configured fallback.
// executeConfiguredWorkloads takes parsed actions and selected IDs, with an
// empty selection preserving v0.1's 'all configured workloads' semantics.
func Configured(ctx context.Context, action string, ids []string, options Options, stdout, stderr io.Writer, effective *config.Config, transition func(context.Context, workload.Spec, string) error, execute func(context.Context, workload.Spec, workload.Options) error) int {
	if len(ids) == 0 {
		ids = IDs(effective)
	}
	if len(ids) == 0 {
		fmt.Fprintln(stderr, "no workloads are configured")
		return 1
	}

	operation := "upsert"
	if action == "remove" {
		operation = "delete"
	}

	for _, id := range ids {
		spec, err := workload.FromConfig(effective, id)
		if err != nil {
			fmt.Fprintf(stderr, "workload %q configuration: %v\n", id, err)
			return 1
		}

		if action == "start" || action == "stop" {
			if err := transition(ctx, spec, action); err != nil {
				fmt.Fprintf(stderr, "workload %q %s: %v\n", id, action, err)
				return 1
			}
			verb := "started"
			if action == "stop" {
				verb = "stopped"
			}
			fmt.Fprintf(stdout, "workload %q: %s\n", id, verb)
			continue
		}
		err = execute(ctx, spec, workload.Options{
			DryRun:    options.DryRun,
			Force:     options.Force,
			Operation: operation,
		})
		if err != nil {
			fmt.Fprintf(stderr, "workload %q: %v\n", id, err)
			return 1
		}

		switch {
		case operation == "upsert" && options.DryRun:
			fmt.Fprintf(stdout, "workload %q: would reconcile\n", id)
		case operation == "upsert":
			fmt.Fprintf(stdout, "workload %q: ready\n", id)
		case options.DryRun:
			fmt.Fprintf(stdout, "workload %q: would delete if present\n", id)
		default:
			fmt.Fprintf(stdout, "workload %q: absent\n", id)
		}
	}
	return 0
}

func IDs(effective *config.Config) []string {
	value, ok := effective.Get("workloads")
	if !ok {
		return nil
	}
	workloads, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(workloads))
	for id := range workloads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
