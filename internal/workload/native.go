package workload

import (
	"context"
	"fmt"
	"slices"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeExistingIntent describes one selected workload without Apply/Compose
// schema or SDK types. The shared host bridge is an attachment, not a resource
// this project-scoped action may provision or repair.
func (s Spec) NativeExistingIntent(scope incusscope.ProjectScope) (native.InstanceIntent, error) {
	return s.nativeIntent(scope, "")
}

func (s Spec) nativeIntent(scope incusscope.ProjectScope, fingerprint string) (native.InstanceIntent, error) {
	if err := s.validate(fingerprint == ""); err != nil {
		return native.InstanceIntent{}, err
	}
	alias := s.ImageAlias
	if fingerprint != "" {
		alias = ""
	}
	instanceType := "container"
	if s.Isolation == "vm" {
		instanceType = "virtual-machine"
	}
	intent := native.InstanceIntent{
		Identity: native.InstanceIdentity{Project: scope.Name, Name: s.Name}, WorkloadID: s.ID,
		Type: instanceType, ImageAlias: alias, ImageFingerprint: fingerprint, EnsureRunning: true,
		Description: "Aginctus managed agent workload " + s.ID, Profiles: []string{},
		Config: map[string]string{
			"user.aginctus.managed": "true", "user.aginctus.resource": "workload",
			"user.aginctus.role": "agent-workload", "user.aginctus.workload": s.ID,
			"user.aginctus.runtime": s.Runtime,
		},
		Devices: map[string]map[string]string{
			"root":       {"type": "disk", "path": "/", "pool": s.StoragePool},
			"management": {"type": "nic", "network": s.ManagementNetwork, "name": "eth0"},
		},
	}
	return intent, intent.Validate()
}

// BindNative resolves this explicitly selected workload from the accepted body.
// The caller verifies the extra typed attachment/runtime intent (absent from
// today's candidate schema) independently; it must not come from ambient config.
func (s Spec) BindNative(accepted environment.Candidate, revision environment.AcceptedRevision, op native.ProjectInstanceOperation, trustedPin string, client native.ProjectInstanceClient, expectedIncarnation time.Time, verifyUse func(context.Context) error) (orchestration.NativeResourceBinding, error) {
	if revision.ValidateAgainst(accepted) != nil || verifyUse == nil {
		return orchestration.NativeResourceBinding{}, fmt.Errorf("pinned accepted workload and verified attachment binding required")
	}
	for _, w := range accepted.Workloads {
		if w.ID != s.ID {
			continue
		}
		i, err := s.nativeIntent(incusscope.ProjectScope{Name: accepted.Environment.Binding.Project}, w.ImageFingerprint)
		if err != nil {
			return orchestration.NativeResourceBinding{}, err
		}
		if w.InstanceName != s.Name || w.InstanceType != i.Type {
			return orchestration.NativeResourceBinding{}, fmt.Errorf("workload identity differs from pinned intent")
		}
		i.EnsureRunning, i.Profiles = w.EnsureRunning, slices.Clone(w.Profiles)
		i.EnvironmentID = accepted.Environment.ID
		i.Config["user.aginctus.environment-id"] = i.EnvironmentID
		stage, err := native.NewProjectInstanceStage(i, op, accepted.Environment.Binding, trustedPin, client)
		if err != nil {
			return orchestration.NativeResourceBinding{}, err
		}
		return orchestration.BindNativeProjectInstance(s.ID, revision.ID, stage, expectedIncarnation, verifyUse, verifyUse), nil
	}
	return orchestration.NativeResourceBinding{}, fmt.Errorf("workload is not selected in the accepted revision")
}
