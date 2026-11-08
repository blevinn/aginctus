package native

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeCreateOperation struct {
	err   error
	waits int
}

func (op *fakeCreateOperation) WaitContext(context.Context) error {
	op.waits++
	return op.err
}

type fakeCreateClient struct {
	intent        InstanceIntent
	mode          string
	projectReads  int
	aliasReads    int
	imageReads    int
	instanceReads int
	creates       int
	request       api.InstancesPost
	instance      *api.Instance
	operation     *fakeCreateOperation
	operationErr  error
}

func (f *fakeCreateClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	info := &incus.ConnectionInfo{Protocol: "incus", SocketPath: "/test/incus.sock", Project: f.intent.Identity.Project}
	if f.mode == "remote" {
		info.SocketPath = ""
	}
	if f.mode == "wrong project" {
		info.Project = "default"
	}
	return info, nil
}

func (f *fakeCreateClient) GetProject(name string) (*api.Project, string, error) {
	f.projectReads++
	scope, _ := incusscope.NewProjectScope(name)
	config := scope.DesiredConfig()
	if f.mode == "foreign project" {
		delete(config, "user.aginctus.managed")
	}
	if f.mode == "project drift" {
		config["features.networks"] = "true"
	}
	return &api.Project{Name: name, ProjectPut: api.ProjectPut{Config: config}}, "project-etag", nil
}

func (f *fakeCreateClient) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	f.aliasReads++
	target := strings.Repeat("a", 64)
	if f.mode == "alias race" && f.aliasReads > 1 {
		target = strings.Repeat("b", 64)
	}
	if f.mode == "wrong alias" {
		name = "foreign"
	}
	return &api.ImageAliasesEntry{Name: name, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: target}}, "alias-etag", nil
}

func (f *fakeCreateClient) GetImage(fingerprint string) (*api.Image, string, error) {
	f.imageReads++
	project := f.intent.Identity.Project
	if f.mode == "foreign image" {
		project = "foreign"
	}
	return &api.Image{Fingerprint: fingerprint, Project: project, Type: f.intent.Type}, "image-etag", nil
}

func (f *fakeCreateClient) GetInstance(name string) (*api.Instance, string, error) {
	f.instanceReads++
	if f.mode == "existing" && f.instanceReads == 1 {
		return &api.Instance{Name: name, Project: f.intent.Identity.Project}, "existing-etag", nil
	}
	if f.mode == "race" && f.instanceReads > 1 {
		return &api.Instance{Name: name, Project: f.intent.Identity.Project}, "raced-etag", nil
	}
	if f.instance != nil {
		return f.instance, "created-etag", nil
	}
	return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
}

func (f *fakeCreateClient) CreateInstance(request api.InstancesPost) (InstanceCreateOperation, error) {
	f.creates++
	f.request = request
	if f.mode == "submit failure" {
		return nil, errors.New("sensitive submission failure")
	}
	config := maps.Clone(map[string]string(request.Config))
	config["volatile.base_image"] = request.Source.Fingerprint
	f.instance = &api.Instance{
		Name: request.Name, Project: f.intent.Identity.Project, Type: string(request.Type), Status: "Stopped",
		InstancePut: api.InstancePut{Description: request.Description, Config: config, Devices: request.Devices, Profiles: request.Profiles},
	}
	if f.mode == "verification mismatch" {
		f.instance.Config["user.aginctus.managed"] = "false"
	}
	f.operation = &fakeCreateOperation{err: f.operationErr}
	return f.operation, nil
}

func TestNativeInstanceCreateUsesExplicitScopedIntentAndVerifies(t *testing.T) {
	intent, _ := instanceFixture()
	fake := &fakeCreateClient{intent: intent}
	result, err := CreateInstanceWithClient(context.Background(), intent, fake)
	if err != nil || !result.Created || !result.Verified || fake.creates != 1 {
		t.Fatalf("create not verified: %+v, %v", result, err)
	}
	if fake.operation == nil || fake.operation.waits != 1 {
		t.Fatal("create operation was not awaited")
	}
	if fake.request.Name != intent.Identity.Name || fake.request.Source.Type != "image" ||
		fake.request.Source.Fingerprint != strings.Repeat("a", 64) || fake.request.Type != api.InstanceType(intent.Type) ||
		!maps.Equal(map[string]string(fake.request.Config), intent.Config) ||
		!reflect.DeepEqual(map[string]map[string]string(fake.request.Devices), intent.Devices) ||
		!slices.Equal(fake.request.Profiles, intent.Profiles) {
		t.Fatalf("create request did not preserve the explicit intent: %+v", fake.request)
	}
	if fake.request.Start {
		t.Fatal("creation unexpectedly started the instance")
	}
}

func TestNativeInstanceCreateRefusesUnsafeTargetsBeforeMutation(t *testing.T) {
	for _, mode := range []string{"remote", "wrong project", "foreign project", "project drift", "wrong alias", "foreign image", "existing", "race", "alias race"} {
		t.Run(mode, func(t *testing.T) {
			intent, _ := instanceFixture()
			fake := &fakeCreateClient{intent: intent, mode: mode}
			result, err := CreateInstanceWithClient(context.Background(), intent, fake)
			if err == nil || result.Verified || fake.creates != 0 {
				t.Fatalf("unsafe target caused success or mutation: %+v, %v, creates=%d", result, err, fake.creates)
			}
		})
	}
}

func TestNativeInstanceCreateReportsUncertaintyWithoutCleanup(t *testing.T) {
	for _, mode := range []string{"submit failure", "operation failure", "verification mismatch"} {
		t.Run(mode, func(t *testing.T) {
			intent, _ := instanceFixture()
			fake := &fakeCreateClient{intent: intent, mode: mode}
			if mode == "operation failure" {
				fake.operationErr = errors.New("sensitive operation failure")
			}
			result, err := CreateInstanceWithClient(context.Background(), intent, fake)
			var failure *InstanceCreateError
			if !errors.As(err, &failure) || result.Verified || !failure.Attempted || !failure.Uncertain || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("uncertain create misreported: %+v, %+v, %v", result, failure, err)
			}
			if fake.creates != 1 || mode == "submit failure" && result.Created || mode != "submit failure" && !result.Created {
				t.Fatalf("create attempt/partial result misreported: %+v creates=%d", result, fake.creates)
			}
		})
	}
}
