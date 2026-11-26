// Package nativelifecycle builds the host-network, Herdr and selected workload
// portion of a native graph. Authority and cross-stack edges belong to the caller.
package nativelifecycle

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/workload"
)

const ManagementNetworkID = "management-network"

type Workload struct {
	Spec      workload.Spec
	Operation native.ProjectInstanceOperation
}

type Herdr struct {
	Spec             herdrclient.Spec
	Operation        native.ProjectInstanceOperation
	ImageFingerprint string
	CreateState      bool
}

type ManagementNetwork struct {
	Spec   managementnetwork.Spec
	Reader native.NetworkReader
	// VerifyBinding confirms the observer and the exact bridge policy are
	// independently approved for the enrolled project. It must be read-only.
	VerifyBinding func(context.Context, environment.Binding, managementnetwork.Spec) error
	// VerifyObserved is a read-only scoped observer when no local transport is supplied.
	VerifyObserved func(context.Context) error
}

type Selection struct {
	Workloads []Workload
	Herdr     *Herdr
	Bridge    *ManagementNetwork
}

// AttachmentVerifier validates the exact independently approved attachment
// coordinates, including host capability and project policy. The caller must
// also verify the complete typed selection and op scopes before any preview.
type AttachmentVerifier func(ctx context.Context, env environment.Descriptor, instance, pool, bridge string) error

// Build projects only the explicit selection; omitted resources have no effect.
// No client is dialed and no implicit state promotion or host effect is added.
func Build(enrolled environment.Descriptor, revision environment.AcceptedRevision, accepted environment.Candidate, selection Selection, trustedPin string, client native.ProjectInstanceClient, verifyAttachments AttachmentVerifier) (orchestration.NativePlan, map[string]orchestration.NativeResourceBinding, error) {
	return build(enrolled, revision, accepted, selection, trustedPin, client, verifyAttachments, orchestration.LocalLifecycleTeardown{})
}

func build(enrolled environment.Descriptor, revision environment.AcceptedRevision, accepted environment.Candidate, selection Selection, trustedPin string, client native.ProjectInstanceClient, verifyAttachments AttachmentVerifier, teardown orchestration.LocalLifecycleTeardown) (orchestration.NativePlan, map[string]orchestration.NativeResourceBinding, error) {
	if enrolled.Validate() != nil || !accepted.Environment.SameEnvironment(enrolled) || revision.ValidateAgainst(accepted) != nil || client == nil || verifyAttachments == nil {
		return orchestration.NativePlan{}, nil, errors.New("pinned native selection and independent attachment verifier required")
	}
	// Reserve the component stage IDs even when the component is not selected.
	reserved := map[string]bool{ManagementNetworkID: true, herdrclient.NativeInstanceID: true, herdrclient.NativeVolumeID: true}
	physical := make(map[string]string, len(accepted.Workloads))
	for _, w := range accepted.Workloads {
		physical[w.InstanceName] = w.ID
	}
	plan := orchestration.NativePlan{APIVersion: orchestration.NativeAPIVersion, Kind: orchestration.NativePlanKind, Revision: revision}
	bindings := make(map[string]orchestration.NativeResourceBinding)
	add := func(resource orchestration.NativeResource, binding orchestration.NativeResourceBinding) {
		plan.Resources = append(plan.Resources, resource)
		bindings[resource.ID] = binding
	}
	var bridge ManagementNetwork
	if selection.Bridge != nil {
		bridge = *selection.Bridge
	}
	var verifyBridge func(context.Context) error
	depends := func(op native.ProjectInstanceOperation, name string) ([]string, error) {
		if op == native.StopInstance || op == native.RemoveInstance {
			return nil, nil
		}
		if (bridge.Reader == nil && bridge.VerifyObserved == nil) || bridge.VerifyBinding == nil || bridge.Spec.Name != name {
			return nil, errors.New("selected attachment requires the exact approved management bridge")
		}
		if verifyBridge == nil {
			verifyBinding := func(ctx context.Context) error { return bridge.VerifyBinding(ctx, enrolled.Binding, bridge.Spec) }
			verifyBridge = func(ctx context.Context) error {
				if err := verifyBinding(ctx); err != nil {
					return err
				}
				if bridge.VerifyObserved != nil {
					return bridge.VerifyObserved(ctx)
				}
				return bridge.Spec.VerifyNative(ctx, bridge.Reader)
			}
			if _, err := bridge.Spec.NativeIntent(); err != nil {
				return nil, err
			}
			resource, binding, err := managementnetwork.BindNativePrerequisite(ManagementNetworkID, revision.ID, verifyBridge)
			if err != nil {
				return nil, err
			}
			add(resource, binding)
		}
		return []string{ManagementNetworkID}, nil
	}
	verifyUse := func(op native.ProjectInstanceOperation, name, pool, network string) func(context.Context) error {
		return func(ctx context.Context) error {
			if op == native.StopInstance || op == native.RemoveInstance {
				return ctx.Err() // no host attachment effects on these paths
			}
			if err := verifyBridge(ctx); err != nil {
				return err
			}
			return verifyAttachments(ctx, enrolled, name, pool, network)
		}
	}
	for _, w := range selection.Workloads {
		if reserved[w.Spec.ID] {
			return orchestration.NativePlan{}, nil, errors.New("duplicate or reserved native workload selection")
		}
		reserved[w.Spec.ID] = true
		edges, err := depends(w.Operation, w.Spec.ManagementNetwork)
		if err != nil {
			return orchestration.NativePlan{}, nil, err
		}
		binding, err := w.Spec.BindNative(accepted, revision, w.Operation, trustedPin, client, teardown.Instances[w.Spec.ID], verifyUse(w.Operation, w.Spec.Name, w.Spec.StoragePool, w.Spec.ManagementNetwork))
		if err != nil {
			return orchestration.NativePlan{}, nil, err
		}
		add(orchestration.NativeResource{ID: w.Spec.ID, Kind: orchestration.NativeWorkload, DependsOn: edges}, binding)
	}
	if selection.Herdr != nil {
		h := *selection.Herdr
		if _, exists := physical[h.Spec.Name]; exists {
			return orchestration.NativePlan{}, nil, errors.New("Herdr instance collides with a pinned workload")
		}
		herdrSDK, ok := client.(herdrclient.NativeClient)
		if !ok {
			return orchestration.NativePlan{}, nil, errors.New("Herdr requires project-scoped state volume transport")
		}
		edges, err := depends(h.Operation, h.Spec.ManagementNetwork)
		if err != nil {
			return orchestration.NativePlan{}, nil, err
		}
		volume, instance, err := h.Spec.BindNative(enrolled, revision, h.Operation, h.ImageFingerprint, trustedPin, h.CreateState, herdrSDK, teardown.Instances[herdrclient.NativeInstanceID], teardown.HerdrState, verifyUse(h.Operation, h.Spec.Name, h.Spec.StoragePool, h.Spec.ManagementNetwork))
		if err != nil {
			return orchestration.NativePlan{}, nil, err
		}
		add(orchestration.NativeResource{ID: herdrclient.NativeVolumeID, Kind: orchestration.NativeHerdrVolume, DependsOn: edges}, volume)
		add(orchestration.NativeResource{ID: herdrclient.NativeInstanceID, Kind: orchestration.NativeHerdrInstance, DependsOn: []string{herdrclient.NativeVolumeID}}, instance)
	}
	if err := plan.Validate(); err != nil {
		return orchestration.NativePlan{}, nil, err
	}
	return plan, bindings, nil
}
