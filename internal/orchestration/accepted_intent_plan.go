package orchestration

import (
	"errors"
	"fmt"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/pkg/api"
)

// AcceptedIntentPreview is a dependency preview of one snapshot. Selection
// describes logical resources only; it carries neither observed effects nor
// an execution pin. The envelope itself is not an engine plan document.
type AcceptedIntentPreview struct {
	api.TypeMeta
	Mode      string     `json:"mode"`
	LiveState string     `json:"liveState"`
	WriterPin bool       `json:"writerPin"`
	Selection NativePlan `json:"selection"`
}

// ValidateAcceptedWorkloadSelection checks input before any authenticated read.
func ValidateAcceptedWorkloadSelection(ids []string) error {
	if len(ids) == 0 || len(ids) >= maxResources { // reserve one stage for the bridge
		return errors.New("select between 1 and 255 workloads explicitly")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !scopeIdentifier.MatchString(id) || seen[id] {
			return errors.New("workload selection contains an invalid or duplicate ID")
		}
		seen[id] = true
	}
	return nil
}

// PreviewAcceptedWorkloads derives the supported workload/bridge graph from
// the exact accepted body. A valid graph is not evidence the bridge exists,
// that the instance is owned or that any operation would create or update it.
func PreviewAcceptedWorkloads(revision environment.AcceptedRevision, accepted environment.Candidate, ids []string) (AcceptedIntentPreview, error) {
	if err := ValidateAcceptedWorkloadSelection(ids); err != nil {
		return AcceptedIntentPreview{}, err
	}
	if revision.ValidateAgainst(accepted) != nil {
		return AcceptedIntentPreview{}, errors.New("accepted workload snapshot content identity mismatch")
	}
	plan := NativePlan{APIVersion: NativeAPIVersion, Kind: NativePlanKind, Revision: revision,
		Resources: []NativeResource{{ID: managementNetworkStage, Kind: NativeManagementNetwork}}}
	for _, id := range ids {
		plan.Resources = append(plan.Resources, NativeResource{ID: id, Kind: NativeWorkload, DependsOn: []string{managementNetworkStage}})
	}
	ordered, err := plan.ordered()
	if err != nil {
		return AcceptedIntentPreview{}, err
	}
	if err := verifyLocalLifecycleSelection(accepted, ordered, false); err != nil {
		return AcceptedIntentPreview{}, fmt.Errorf("unsupported accepted workload selection: %w", err)
	}
	plan.Resources = ordered
	return AcceptedIntentPreview{TypeMeta: api.NewTypeMeta("AcceptedIntentPreview"), Mode: "intent-only", LiveState: "unobserved", Selection: plan}, nil
}
