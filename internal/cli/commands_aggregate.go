package cli

import (
	"context"
	"io"

	aggregatecmd "github.com/blevinn/aginctus/internal/cli/commands/aggregate"
	aggregateops "github.com/blevinn/aginctus/internal/cli/operations/aggregate"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/spf13/cobra"
)

// Root-level aggregate verbs are registered separately from domain logic.
func addAggregateCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	aggregatecmd.Register(root, stdout, stderr, load, status, aggregatecmd.Handlers{
		Apply: executeAggregateApply,
		Remove: func(ctx context.Context, out, errOut io.Writer, cfg *config.Config) int {
			return runAggregateRemove(ctx, nil, out, errOut, cfg)
		},
		Status: func(ctx context.Context, out, errOut io.Writer, cfg *config.Config) int {
			return runAggregateStatus(ctx, nil, out, errOut, cfg)
		},
	})
}

// runAggregateStatus binds the existing independent resource inspections to
// the all-or-nothing aggregate formatter, retaining the original order.
func runAggregateStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return aggregateops.Status(ctx, args, out, stderr, cfg, []aggregateops.StatusCheck{
		{Name: "network", Run: runNetworkStatus},
		{Name: "gateway", Run: runGatewayStatus},
		{Name: "Herdr client", Run: runHerdrClientStatus},
		{Name: "workloads", Run: runWorkloadStatus},
	})
}

// The aggregate removal path never exposes force and preserves volumes.
var removeAggregateGateway = func(ctx context.Context, spec gateway.Spec) error {
	return spec.Remove(ctx, false)
}

func runAggregateRemove(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return aggregateops.Remove(ctx, args, out, stderr, cfg, workloadIDs,
		executeWorkload, executeHerdrClient, removeAggregateGateway, executeManagementNetwork)
}

// Aggregate apply remains a thin entrypoint to the existing typed planner,
// execution engine and strict regular-file parser. The engine variable is
// retained as the regression tests' explicit failure-injection seam.
func aggregateApplyPlan(effective *config.Config) (orchestration.Plan, error) {
	return aggregateops.Plan(effective, workloadIDs)
}

var executeAggregatePlan = aggregateops.ExecutePlan

func executeAggregateApply(ctx context.Context, render bool, fromFile string, stdout, stderr io.Writer, effective *config.Config) int {
	return aggregateops.Apply(ctx, render, fromFile, stdout, stderr, effective,
		aggregateApplyPlan, readAggregatePlan, executeAggregatePlan)
}

func readAggregatePlan(path string) (orchestration.Plan, error) {
	return aggregateops.ReadPlan(path)
}
