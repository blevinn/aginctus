package native

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type updateFake struct {
	intent        InstanceIntent
	instance      *api.Instance
	reads, writes int
	etag          string
	mode          string
	put           api.InstancePut
	op            *fakeInstanceOperation
}

func (f *updateFake) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/tmp/incus.sock", Project: f.intent.Identity.Project}, nil
}
func (f *updateFake) GetProject(name string) (*api.Project, string, error) {
	scope, _ := incusscope.NewProjectScope(name)
	config := scope.DesiredConfig()
	if f.mode == "project drift" && f.reads > 1 {
		config["features.networks"] = "true"
	}
	return &api.Project{Name: name, ProjectPut: api.ProjectPut{Config: config}}, "project-etag", nil
}
func (f *updateFake) GetInstance(name string) (*api.Instance, string, error) {
	f.reads++
	copy := *f.instance
	if f.mode == "race" && f.reads == 2 {
		copy.Config = maps.Clone(copy.Config)
		copy.Config["operator.key"] = "raced"
		return &copy, "etag-2", nil
	}
	if f.mode == "foreign" {
		copy.Config = maps.Clone(copy.Config)
		delete(copy.Config, "user.aginctus.workload")
		return &copy, f.etag, nil
	}
	if f.mode == "after failure" && f.writes > 0 {
		return nil, "", errors.New("failed read")
	}
	return &copy, f.etag, nil
}
func (f *updateFake) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	return &api.ImageAliasesEntry{Name: name, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: strings.Repeat("a", 64)}}, "alias-etag", nil
}
func (f *updateFake) GetImage(fp string) (*api.Image, string, error) {
	return &api.Image{Fingerprint: fp, Project: f.intent.Identity.Project, Type: f.intent.Type}, "image-etag", nil
}
func (f *updateFake) UpdateInstance(_ string, put api.InstancePut, etag string) (InstanceOperation, error) {
	f.writes++
	f.put = put
	if etag != f.etag {
		return nil, errors.New("incorrect ETag")
	}
	if f.mode == "submit failure" {
		return nil, errors.New("submit failed")
	}
	f.instance = &api.Instance{Name: f.instance.Name, Project: f.instance.Project, Type: f.instance.Type, Status: f.instance.Status, InstancePut: put}
	f.op = &fakeInstanceOperation{}
	if f.mode == "operation failure" {
		f.op.err = errors.New("wait failed")
	}
	if f.mode == "operation cancel" {
		f.op.err = context.Canceled
	}
	return f.op, nil
}

func TestNativeInstanceUpdateEmptyConfigPresence(t *testing.T) {
	intent, fake := updateFixture(t)
	fake.instance.Description = intent.Description
	fake.instance.Config["user.aginctus.runtime"] = intent.Config["user.aginctus.runtime"]
	intent.Config["user.explicit-empty"] = ""
	result, err := UpdateInstanceWithClient(context.Background(), intent, fake)
	if err != nil || !result.Changed || !result.Verified || fake.writes != 1 {
		t.Fatalf("absent empty value was skipped: %+v %v", result, err)
	}
}

func TestNativeInstanceUpdateNoopRechecksAndDeploymentIsImmutable(t *testing.T) {
	for _, mode := range []string{"race", "project drift"} {
		t.Run(mode, func(t *testing.T) {
			intent, fake := updateFixture(t)
			fake.instance.Description = intent.Description
			fake.instance.Config["user.aginctus.runtime"] = intent.Config["user.aginctus.runtime"]
			fake.mode = mode
			result, err := UpdateInstanceWithClient(context.Background(), intent, fake)
			if err == nil || result.Verified || fake.writes != 0 {
				t.Fatalf("inconsistent no-op accepted: %+v %v", result, err)
			}
		})
	}
	for _, deployment := range []string{"", "other", "aginctus-test"} {
		intent, fake := updateFixture(t)
		delete(fake.instance.Config, "user.aginctus.deployment")
		if deployment == "aginctus-test" {
			deployment = intent.Identity.Project
		}
		intent.Config["user.aginctus.deployment"] = deployment
		result, err := UpdateInstanceWithClient(context.Background(), intent, fake)
		if err == nil || result.Verified || fake.writes != 0 {
			t.Fatalf("deployment marker changed: %+v %v", result, err)
		}
	}
}

func updateFixture(t *testing.T) (InstanceIntent, *updateFake) {
	t.Helper()
	intent, observed := instanceFixture()
	intent.Description = "reconciled description"
	intent.Config = map[string]string{
		"user.aginctus.managed": "true", "user.aginctus.resource": "workload", "user.aginctus.role": "agent-workload",
		"user.aginctus.workload": "dev", "user.aginctus.runtime": "new-runtime",
	}
	config := maps.Clone(observed.Config)
	config["user.aginctus.deployment"] = intent.Identity.Project
	return intent, &updateFake{intent: intent, etag: "etag-1", instance: &api.Instance{
		Name: intent.Identity.Name, Project: intent.Identity.Project, Type: intent.Type, Status: "Running",
		InstancePut: api.InstancePut{Description: observed.Description, Config: config, Devices: api.DevicesMap(observed.Devices), Profiles: []string{}},
	}}
}

func TestNativeInstanceUpdatePreservesUnmanagedWritableData(t *testing.T) {
	intent, fake := updateFixture(t)
	fake.instance.Architecture = "x86_64"
	fake.instance.Ephemeral = true
	fake.instance.Stateful = true
	beforeDevices := cloneDevices(map[string]map[string]string(fake.instance.Devices))
	beforeProfiles := append([]string(nil), fake.instance.Profiles...)
	beforeArchitecture := fake.instance.Architecture
	result, err := UpdateInstanceWithClient(context.Background(), intent, fake)
	if err != nil || !result.Changed || !result.Verified || fake.writes != 1 || fake.op.waits != 1 {
		t.Fatalf("update failed: %+v %v", result, err)
	}
	if fake.put.Config["environment.UNMANAGED_SECRET"] != "sensitive-value" ||
		!reflect.DeepEqual(map[string]map[string]string(fake.put.Devices), beforeDevices) || !slices.Equal(fake.put.Profiles, beforeProfiles) ||
		fake.put.Architecture != beforeArchitecture || !fake.put.Ephemeral || !fake.put.Stateful ||
		fake.instance.Architecture != beforeArchitecture || !fake.instance.Ephemeral || !fake.instance.Stateful ||
		fake.instance.Status != "Running" || fake.instance.Type != intent.Type {
		t.Fatalf("update did not preserve observed data: devices=%t profiles=%t architecture=%q ephemeral=%t stateful=%t status=%q type=%q", reflect.DeepEqual(map[string]map[string]string(fake.put.Devices), beforeDevices), reflect.DeepEqual(fake.put.Profiles, beforeProfiles), fake.put.Architecture, fake.put.Ephemeral, fake.put.Stateful, fake.instance.Status, fake.instance.Type)
	}
	if fake.put.Config["user.aginctus.runtime"] != "new-runtime" || fake.put.Description != intent.Description {
		t.Fatalf("explicit fields not applied: %+v", fake.put)
	}
}

func TestNativeInstanceUpdateIsIdempotentAndFailsClosed(t *testing.T) {
	intent, fake := updateFixture(t)
	fake.instance.Description = intent.Description
	fake.instance.Config["user.aginctus.runtime"] = intent.Config["user.aginctus.runtime"]
	result, err := UpdateInstanceWithClient(context.Background(), intent, fake)
	if err != nil || !result.Verified || result.Changed || fake.writes != 0 {
		t.Fatalf("no-op update mutated: %+v %v", result, err)
	}
	for _, mode := range []string{"race", "foreign", "project drift", "submit failure", "operation failure", "operation cancel", "after failure"} {
		t.Run(mode, func(t *testing.T) {
			intent, fake := updateFixture(t)
			fake.mode = mode
			result, err := UpdateInstanceWithClient(context.Background(), intent, fake)
			if err == nil || result.Verified {
				t.Fatalf("%s unexpectedly succeeded: %+v", mode, result)
			}
			if (mode == "race" || mode == "foreign" || mode == "project drift") && fake.writes != 0 {
				t.Fatal("mutation crossed an unsafe ownership or observation boundary")
			}
			if (mode == "operation failure" || mode == "operation cancel" || mode == "after failure") && fake.writes != 1 {
				t.Fatalf("expected one uncertain write: %d", fake.writes)
			}
		})
	}
}
