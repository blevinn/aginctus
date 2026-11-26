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
	Mode         string                     `json:"mode"`
	LiveState    string                     `json:"liveState"`
	WriterPin    bool                       `json:"writerPin"`
	Selection    NativePlan                 `json:"selection"`
	Capabilities []AcceptedIntentCapability `json:"capabilities"`
}

// AcceptedIntentCapability describes a prerequisite or gate, never a grant.
type AcceptedIntentCapability struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type AcceptedIntentSelection struct {
	All       bool
	Workloads []string
	Herdr     bool
	Gateway   bool
}

// Validate checks explicit selection before any authenticated read.
func (s AcceptedIntentSelection) Validate() error {
	selected := len(s.Workloads) > 0 || s.Herdr || s.Gateway
	if s.All == selected {
		return errors.New("select --all alone or one or more of --workload, --herdr and --gateway")
	}
	if len(s.Workloads) >= maxResources { // reserve the bridge for workloads
		return errors.New("too many explicitly selected workloads")
	}
	seen := make(map[string]bool, len(s.Workloads))
	for _, id := range s.Workloads {
		if !scopeIdentifier.MatchString(id) || seen[id] {
			return errors.New("workload selection contains an invalid or duplicate ID")
		}
		seen[id] = true
	}
	return nil
}

// PreviewAcceptedIntent derives the supported dependency graph from the exact
// accepted body. No cross-domain edge, runtime permission or outcome is inferred.
func PreviewAcceptedIntent(revision environment.AcceptedRevision, accepted environment.Candidate, selection AcceptedIntentSelection) (AcceptedIntentPreview, error) {
	if err := selection.Validate(); err != nil {
		return AcceptedIntentPreview{}, err
	}
	if revision.ValidateAgainst(accepted) != nil {
		return AcceptedIntentPreview{}, errors.New("accepted snapshot content identity mismatch")
	}
	if selection.All {
		selection.Workloads = accepted.SelectedWorkloads
		selection.Herdr = accepted.Herdr != nil
		selection.Gateway = accepted.Gateway != nil
	}
	plan := NativePlan{APIVersion: NativeAPIVersion, Kind: NativePlanKind, Revision: revision}
	add := func(id string, kind NativeResourceKind, deps ...string) {
		plan.Resources = append(plan.Resources, NativeResource{ID: id, Kind: kind, DependsOn: deps})
	}
	const unverified = "required-unverified"
	capabilities := []AcceptedIntentCapability{{"execution-authority", unverified}, {"host-attachments", unverified}}
	if len(selection.Workloads) > 0 || selection.Herdr {
		add(managementNetworkStage, NativeManagementNetwork)
	}
	for _, id := range selection.Workloads {
		add(id, NativeWorkload, managementNetworkStage)
	}
	if selection.Herdr {
		if accepted.Herdr == nil {
			return AcceptedIntentPreview{}, errors.New("no accepted Herdr intent")
		}
		add(herdrVolumeStage, NativeHerdrVolume, managementNetworkStage)
		add(herdrInstanceStage, NativeHerdrInstance, herdrVolumeStage)
		bootstrap := "not-accepted"
		if accepted.Herdr.AllowStateBootstrap {
			bootstrap = "separate-approval-required"
		}
		capabilities = append(capabilities, AcceptedIntentCapability{"herdr-state-bootstrap", bootstrap})
	}
	if selection.Gateway {
		g := accepted.Gateway
		if g == nil || g.Postgres.IncusFingerprint == g.LiteLLM.IncusFingerprint {
			return AcceptedIntentPreview{}, errors.New("gateway selection requires accepted distinct image identities")
		}
		add(GatewayImageStage, NativeGatewayImage)
		add(GatewayVolumeStage, NativeGatewayVolume, GatewayImageStage)
		add(GatewayInstanceStage, NativeGatewayInstance, GatewayVolumeStage)
		capabilities = append(capabilities, AcceptedIntentCapability{"gateway-image-provenance", unverified}, AcceptedIntentCapability{"gateway-runtime", "gated"})
	}
	ordered, err := plan.ordered()
	if err != nil {
		return AcceptedIntentPreview{}, err
	}
	if err := verifyLocalNativeSelection(accepted, ordered, false); err != nil {
		return AcceptedIntentPreview{}, fmt.Errorf("unsupported accepted native selection: %w", err)
	}
	plan.Resources = ordered
	return AcceptedIntentPreview{TypeMeta: api.NewTypeMeta("AcceptedIntentPreview"), Mode: "intent-only", LiveState: "unobserved", Selection: plan, Capabilities: capabilities}, nil
}
