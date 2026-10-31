package workload

import (
	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeExistingIntent describes one selected workload without Apply/Compose
// schema or SDK types. The shared host bridge is an attachment, not a resource
// this project-scoped action may provision or repair.
func (s Spec) NativeExistingIntent(scope incusscope.ProjectScope) (native.InstanceIntent, error) {
	if err := s.Validate(); err != nil {
		return native.InstanceIntent{}, err
	}
	instanceType := "container"
	if s.Isolation == "vm" {
		instanceType = "virtual-machine"
	}
	intent := native.InstanceIntent{
		Identity: native.InstanceIdentity{Project: scope.Name, Name: s.Name}, WorkloadID: s.ID,
		Type: instanceType, ImageAlias: s.ImageAlias, EnsureRunning: true,
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
