package gateway

import (
	"fmt"
)

// NativeGatewayCreateAction is a read-only classification, never execution
// authority. No action means a compatible owned service is already present.
type NativeGatewayCreateAction string

const (
	NativeGatewayCreateStopped NativeGatewayCreateAction = "create-stopped"
	NativeGatewayRetainExisting NativeGatewayCreateAction = "retain-existing"
)

// NativeGatewayCreateStep names a single proposed service transition.
// Order is fixed: PostgreSQL before LiteLLM. Neither step starts a service.
type NativeGatewayCreateStep struct {
	Name    string                    `json:"name"`
	Project string                    `json:"project"`
	Action  NativeGatewayCreateAction `json:"action"`
}

type NativeGatewayCreatePlan struct {
	Project string                    `json:"project"`
	Steps   []NativeGatewayCreateStep `json:"steps"`
}

// PlanNativeGatewayCreate requires exactly the two completed read-only
// observations for the selected project, in service dependency order.
// Partial observations, duplicate identities and unknown states fail closed.
// The plan is not permission to write, adopt, stop, or delete anything.
func PlanNativeGatewayCreate(pair NativeGatewayInstancePair, observed []NativeGatewayInstanceObservation) (NativeGatewayCreatePlan, error) {
	if pair.Postgres.Project == "" || pair.Postgres.Project != pair.LiteLLM.Project ||
		pair.Postgres.Name != "postgres-1" || pair.Postgres.Role != "postgres" ||
		pair.LiteLLM.Name != "litellm-1" || pair.LiteLLM.Role != "litellm" ||
		pair.Postgres.ImageAlias == pair.LiteLLM.ImageAlias ||
		len(observed) != 2 {
		return NativeGatewayCreatePlan{}, fmt.Errorf("native gateway creation requires two selected service observations")
	}
	steps := make([]NativeGatewayCreateStep, 0, 2)
	for i, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
		got := observed[i]
		if got.Name != spec.Name || got.Project != spec.Project {
			return NativeGatewayCreatePlan{}, fmt.Errorf("native gateway observation identity mismatch")
		}
		action := NativeGatewayRetainExisting
		switch got.Status {
		case NativeGatewayAbsent:
			action = NativeGatewayCreateStopped
		case NativeGatewayCompatible:
			// A compatible observation permits a no-op only, never adoption.
		default:
			return NativeGatewayCreatePlan{}, fmt.Errorf("native gateway observation state unverified")
		}
		steps = append(steps, NativeGatewayCreateStep{Name: spec.Name, Project: spec.Project, Action: action})
	}
	return NativeGatewayCreatePlan{Project: pair.Postgres.Project, Steps: steps}, nil
}
