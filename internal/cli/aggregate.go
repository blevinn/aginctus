package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration"
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
		return orchestration.Plan{}, fmt.Errorf("Herdr client: %w", err)
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

func runAggregateApply(args []string, stdout, stderr io.Writer, effective *config.Config) int {
	if len(args) != 1 || args[0] != "--render" {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] apply --render")
		fmt.Fprintln(stderr, "Execution and --from support are not implemented yet")
		return 2
	}
	plan, err := aggregateApplyPlan(effective)
	if err != nil {
		fmt.Fprintf(stderr, "render orchestration: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(plan); err != nil {
		fmt.Fprintf(stderr, "encode orchestration: %v\n", err)
		return 1
	}
	return 0
}
