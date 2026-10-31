package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
	composedriver "github.com/blevinn/aginctus/internal/orchestration/drivers/compose"
	"github.com/blevinn/aginctus/internal/statevolume"
	"github.com/blevinn/aginctus/internal/workload"
)

// aggregateApplyPlan produces a stable preview. The management network is a
// shared dependency: it must be reconciled once, before any dependent step.
func aggregateApplyPlan(effective *config.Config) (orchestration.Plan, error) {
	network, err := managementnetwork.FromConfig(effective)
	if err != nil {
		return orchestration.Plan{}, fmt.Errorf("management network: %w", err)
	}
	networkPlan, err := network.Plan("upsert", false)
	if err != nil {
		return orchestration.Plan{}, err
	}
	client, err := herdrclient.FromConfig(effective)
	if err != nil {
		return orchestration.Plan{}, fmt.Errorf("herdr client: %w", err)
	}
	clientPlan, err := client.Plan("upsert", false)
	if err != nil {
		return orchestration.Plan{}, err
	}
	gatewaySpec, err := gateway.FromConfig(effective)
	if err != nil {
		return orchestration.Plan{}, fmt.Errorf("gateway: %w", err)
	}
	gatewayPlan, err := gatewaySpec.OrchestrationPlan()
	if err != nil {
		return orchestration.Plan{}, err
	}

	plan := orchestration.Plan{
		APIVersion: orchestration.APIVersion,
		Kind:       orchestration.Kind,
		Metadata:   orchestration.Metadata{Name: "aginctus"},
	}
	seen := make(map[string]struct{})
	appendSteps := func(source orchestration.Plan) error {
		for _, step := range source.Steps {
			if _, exists := seen[step.ID]; exists {
				// Only the common network dependency may appear repeatedly.
				// Detect divergent resource definitions rather than silently
				// accepting whichever one happened to be added first.
				if step.ID != "management-network" {
					return fmt.Errorf("duplicate orchestration step %q", step.ID)
				}
				for _, existing := range plan.Steps {
					if existing.ID == step.ID {
						var a, b any
						if err := json.Unmarshal(existing.Configuration, &a); err != nil {
							return err
						}
						if err := json.Unmarshal(step.Configuration, &b); err != nil {
							return err
						}
						ac, _ := json.Marshal(a)
						bc, _ := json.Marshal(b)
						if existing.Driver != step.Driver || string(ac) != string(bc) {
							return fmt.Errorf("conflicting shared management network resource")
						}
						break
					}
				}
				continue
			}
			seen[step.ID] = struct{}{}
			plan.Steps = append(plan.Steps, step)
		}
		return nil
	}

	for _, source := range []orchestration.Plan{networkPlan, clientPlan, gatewayPlan} {
		if err := appendSteps(source); err != nil {
			return orchestration.Plan{}, err
		}
	}
	for _, id := range workloadIDs(effective) {
		spec, err := workload.FromConfig(effective, id)
		if err != nil {
			return orchestration.Plan{}, fmt.Errorf("workload %q: %w", id, err)
		}
		workloadPlan, err := spec.Plan("upsert", false)
		if err != nil {
			return orchestration.Plan{}, err
		}
		if err := appendSteps(workloadPlan); err != nil {
			return orchestration.Plan{}, err
		}
	}
	if err := plan.Validate(); err != nil {
		return orchestration.Plan{}, err
	}
	return plan, nil
}

// executeAggregatePlan validates every step before initializing secrets or
// changing host resources. The engine still executes in document order.
var executeAggregatePlan = func(ctx context.Context, plan orchestration.Plan, effective *config.Config) error {
	spec, err := gateway.FromConfig(effective)
	if err != nil {
		return fmt.Errorf("gateway configuration: %w", err)
	}
	// First validate without side effects, including unknown drivers and
	// malformed Compose documents.
	validator := orchestration.NewEngine(map[string]orchestration.Driver{
		"apply":        applydriver.New(),
		"compose":      composedriver.New(),
		"state-volume": statevolume.NewDriver(),
	})
	if err := validator.Validate(plan, orchestration.ExecuteOptions{}); err != nil {
		return err
	}
	var environment map[string]string
	for _, step := range plan.Steps {
		if step.Driver == "compose" {
			// In a --from plan, only inject credentials into the configured
			// gateway project; never publish gateway secrets to arbitrary
			// Compose projects supplied in a document.
			var cfg composedriver.Config
			if err := json.Unmarshal(step.Configuration, &cfg); err != nil {
				return err
			}
			if cfg.Project != spec.Project {
				return fmt.Errorf("compose project %q is not configured gateway project %q", cfg.Project, spec.Project)
			}
			environment, err = spec.InitializeRuntimeEnvironment()
			if err != nil {
				return fmt.Errorf("gateway initialization: %w", err)
			}
			break
		}
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{
		"apply":        applydriver.New(),
		"compose":      composedriver.NewWithEnvironment(environment),
		"state-volume": statevolume.NewDriver(),
	})
	_, err = engine.Execute(ctx, plan, orchestration.ExecuteOptions{})
	return err
}

// executeAggregateApply runs a fully parsed, typed CLI operation. Flags and
// positional syntax have already been validated by Cobra before loading config.
func executeAggregateApply(ctx context.Context, render bool, fromFile string, stdout, stderr io.Writer, effective *config.Config) int {
	var plan orchestration.Plan
	var err error
	switch {
	case render:
		plan, err = aggregateApplyPlan(effective)
		if err == nil {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			err = encoder.Encode(plan)
		}
		if err != nil {
			fmt.Fprintf(stderr, "render orchestration: %v\n", err)
			return 1
		}
		return 0
	case fromFile != "":
		plan, err = readAggregatePlan(fromFile)
	default:
		plan, err = aggregateApplyPlan(effective)
	}
	if err != nil {
		fmt.Fprintf(stderr, "load orchestration: %v\n", err)
		return 1
	}
	if err := executeAggregatePlan(ctx, plan, effective); err != nil {
		fmt.Fprintf(stderr, "apply orchestration: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "applied orchestration %q (%d steps)\n", plan.Metadata.Name, len(plan.Steps))
	return 0
}

func readAggregatePlan(path string) (orchestration.Plan, error) {
	if path == "" || path == "-" {
		return orchestration.Plan{}, fmt.Errorf("--from requires a regular file path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return orchestration.Plan{}, err
	}
	if !info.Mode().IsRegular() {
		return orchestration.Plan{}, fmt.Errorf("orchestration input must be a regular file")
	}
	if info.Size() > 16<<20 {
		return orchestration.Plan{}, fmt.Errorf("orchestration input exceeds 16 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return orchestration.Plan{}, err
	}
	if len(data) > 16<<20 {
		return orchestration.Plan{}, fmt.Errorf("orchestration input exceeds 16 MiB")
	}
	return orchestration.ParsePlanStream(data)
}
