package native

import (
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"testing"
)

func instanceFixture() (InstanceIntent, InstanceObservation) {
	intent := InstanceIntent{
		Identity: InstanceIdentity{Project: "aginctus", Name: "dev"}, WorkloadID: "dev",
		Type: "container", ImageAlias: "aginctus-opencode", EnsureRunning: true, Description: "managed workload", Profiles: []string{},
		Config:  map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload", "user.aginctus.workload": "dev", "user.aginctus.runtime": "opencode"},
		Devices: map[string]map[string]string{"root": {"type": "disk", "path": "/", "pool": "btrfs"}, "management": {"type": "nic", "network": "aginctus-mgmt", "name": "eth0"}},
	}
	config := maps.Clone(intent.Config)
	config["volatile.base_image"] = strings.Repeat("a", 64)
	config["environment.UNMANAGED_SECRET"] = "sensitive-value"
	devices := map[string]map[string]string{}
	for key, value := range intent.Devices {
		devices[key] = maps.Clone(value)
	}
	devices["workspace"] = map[string]string{"type": "disk", "source": "/host/workspace", "path": "/workspace"}
	devices["state"] = map[string]string{"type": "disk", "pool": "btrfs", "source": "protected-volume", "path": "/var/lib/aginctus"}
	devices["root"]["size"] = "30GiB"
	return intent, InstanceObservation{
		Identity: intent.Identity, Type: "container", State: "Running", Description: intent.Description,
		Config: config, Devices: devices, Profiles: []string{}, ImageFingerprint: strings.Repeat("a", 64),
	}
}

func TestExistingInstanceIdempotencePreservationAndRedaction(t *testing.T) {
	intent, observed := instanceFixture()
	before, _ := json.Marshal(observed)
	for range 2 {
		plan, err := PlanExistingInstance(intent, observed)
		if err != nil || plan.Action != Unchanged || len(plan.Changes) != 0 || plan.WriteAuthorized {
			t.Fatalf("unexpected no-op: %+v, %v", plan, err)
		}
	}
	intent.Config["environment.MANAGED_SECRET"] = "desired-secret-value"
	observed.Config["environment.MANAGED_SECRET"] = "old-secret-value"
	plan, err := PlanExistingInstance(intent, observed)
	if err != nil || plan.Action != Update {
		t.Fatal("managed config drift not classified")
	}
	encoded, _ := json.Marshal(plan)
	for _, value := range []string{"sensitive-value", "desired-secret-value", "old-secret-value", "/host/workspace", "protected-volume"} {
		if strings.Contains(string(encoded), value) {
			t.Fatal("plan exposed configuration values")
		}
	}
	delete(observed.Config, "environment.MANAGED_SECRET")
	after, _ := json.Marshal(observed)
	if string(before) != string(after) {
		t.Fatal("pure planning modified observed fields or unmanaged devices")
	}
}

func TestExistingInstanceFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*InstanceIntent, *InstanceObservation)
	}{
		{"blank project", func(i *InstanceIntent, _ *InstanceObservation) { i.Identity.Project = "" }},
		{"default project", func(i *InstanceIntent, _ *InstanceObservation) { i.Identity.Project = "default" }},
		{"remote target", func(i *InstanceIntent, _ *InstanceObservation) { i.Identity.Name = "remote:dev" }},
		{"missing workload id", func(i *InstanceIntent, _ *InstanceObservation) { i.WorkloadID = "" }},
		{"implicit profiles", func(i *InstanceIntent, _ *InstanceObservation) { i.Profiles = nil }},
		{"invalid type", func(i *InstanceIntent, _ *InstanceObservation) { i.Type = "invented" }},
		{"invalid desired ownership", func(i *InstanceIntent, _ *InstanceObservation) { i.Config["user.aginctus.resource"] = "instance" }},
		{"absent observation", func(_ *InstanceIntent, o *InstanceObservation) { *o = InstanceObservation{} }},
		{"wrong observed project", func(_ *InstanceIntent, o *InstanceObservation) { o.Identity.Project = "foreign" }},
		{"wrong observed name", func(_ *InstanceIntent, o *InstanceObservation) { o.Identity.Name = "other" }},
		{"name alone", func(_ *InstanceIntent, o *InstanceObservation) { o.Config = nil }},
		{"wrong role", func(_ *InstanceIntent, o *InstanceObservation) { o.Config["user.aginctus.role"] = "infrastructure" }},
		{"wrong resource marker", func(_ *InstanceIntent, o *InstanceObservation) { o.Config["user.aginctus.resource"] = "instance" }},
		{"wrong workload", func(_ *InstanceIntent, o *InstanceObservation) { o.Config["user.aginctus.workload"] = "foreign" }},
		{"wrong deployment", func(_ *InstanceIntent, o *InstanceObservation) { o.Config["user.aginctus.deployment"] = "foreign" }},
		{"unknown state", func(_ *InstanceIntent, o *InstanceObservation) { o.State = "Frozen" }},
		{"missing source identity", func(_ *InstanceIntent, o *InstanceObservation) { delete(o.Config, "volatile.base_image") }},
		{"ambiguous image fingerprint", func(_ *InstanceIntent, o *InstanceObservation) { o.ImageFingerprint = "short" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, observed := instanceFixture()
			test.edit(&intent, &observed)
			if _, err := PlanExistingInstance(intent, observed); err == nil {
				t.Fatal("unsafe state accepted")
			}
		})
	}
}

func TestExistingInstanceReplacementIsExplicitAndDeterministic(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*InstanceObservation)
	}{
		{"image", func(o *InstanceObservation) { o.ImageFingerprint = strings.Repeat("b", 64) }},
		{"type", func(o *InstanceObservation) { o.Type = "virtual-machine" }},
		{"profiles", func(o *InstanceObservation) { o.Profiles = []string{"default"} }},
		{"root pool", func(o *InstanceObservation) { o.Devices["root"]["pool"] = "foreign" }},
		{"host bridge", func(o *InstanceObservation) { o.Devices["management"]["network"] = "foreign" }},
		{"missing managed device", func(o *InstanceObservation) { delete(o.Devices, "management") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent, observed := instanceFixture()
			test.edit(&observed)
			first, err := PlanExistingInstance(intent, observed)
			if err != nil || first.Action != ReplacementRequired || first.WriteAuthorized {
				t.Fatalf("unsafe replacement classification: %+v, %v", first, err)
			}
			for range 10 {
				again, err := PlanExistingInstance(intent, observed)
				if err != nil || !reflect.DeepEqual(first, again) {
					t.Fatal("non-deterministic operation order")
				}
			}
		})
	}
	intent, observed := instanceFixture()
	observed.State = "Stopped"
	plan, err := PlanExistingInstance(intent, observed)
	if err != nil || plan.Action != Update || len(plan.Changes) != 0 {
		t.Fatal("start-only intent misclassified")
	}
}
