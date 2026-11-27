package environment

import (
	"testing"
	"time"

	"github.com/lxc/incus/v7/shared/api"
)

func TestSuccessfulRecoveryTaskRequiresPositiveExactEvidence(t *testing.T) {
	const id = "6916c8a6-9b7d-4abd-90b3-aedfec7ec7da"
	const resource = "/1.0/instances/selected-worker?project=workers"
	now := time.Now()
	effect := LocalNativeEffectRecord{Action: "create", DaemonID: id}
	fresh := func() api.Operation {
		return api.Operation{ID: id, Class: api.OperationClassTask, StatusCode: api.Success,
			Description: "Creating instance", CreatedAt: now, UpdatedAt: now.Add(time.Second),
			Resources: map[string][]string{"instances": {resource}}}
	}
	task := fresh()
	if !successfulRecoveryTask(&task, effect, resource) {
		t.Fatal("exact successful task rejected")
	}
	for _, alter := range []func(*api.Operation){
		func(op *api.Operation) { op.ID = "foreign" },
		func(op *api.Operation) { op.StatusCode = api.Running },
		func(op *api.Operation) { op.Description = "Deleting instance" },
		func(op *api.Operation) { op.Resources["instances"] = []string{"/1.0/instances/foreign"} },
		func(op *api.Operation) { op.Resources["volumes"] = []string{"/foreign"} },
		func(op *api.Operation) { op.UpdatedAt = time.Time{} },
	} {
		task := fresh()
		alter(&task)
		if successfulRecoveryTask(&task, effect, resource) {
			t.Fatal("unverified task accepted")
		}
	}
	if successfulRecoveryTask(nil, effect, resource) {
		t.Fatal("missing task accepted")
	}
}
