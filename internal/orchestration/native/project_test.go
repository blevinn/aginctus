package native

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeProjectClient struct {
	scope          incusscope.ProjectScope
	project        *api.Project
	mode           string
	reads, creates int
	request        api.ProjectsPost
	cancel         context.CancelFunc
}

func (f *fakeProjectClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	info := &incus.ConnectionInfo{Protocol: "incus", Project: f.scope.Name, SocketPath: "/test/incus.sock"}
	if f.mode == "remote" {
		info.SocketPath = ""
	}
	if f.mode == "wrong project" {
		info.Project = "default"
	}
	return info, nil
}

func (f *fakeProjectClient) GetProject(name string) (*api.Project, string, error) {
	f.reads++
	if name != f.scope.Name {
		return nil, "", errors.New("wrong project read")
	}
	if f.mode == "denied" {
		return nil, "", api.StatusErrorf(http.StatusForbidden, "sensitive denial")
	}
	if f.mode == "hidden collision" || f.project == nil && f.mode != "boundary race" {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "not visible")
	}
	if f.mode == "boundary race" {
		if f.reads == 1 {
			return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
		}
		return &api.Project{Name: name}, "etag", nil
	}
	if f.mode == "postflight ownership drift" && f.creates > 0 {
		return &api.Project{Name: name}, "etag", nil
	}
	return f.project, "etag", nil
}

func (f *fakeProjectClient) CreateProject(request api.ProjectsPost) error {
	f.creates++
	f.request = request
	if f.mode == "hidden collision" || f.mode == "create failure" {
		return errors.New("sensitive collision or transport failure")
	}
	f.project = &api.Project{Name: request.Name, ProjectPut: request.ProjectPut}
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}

func TestNativeProjectProvisioningCreateReuseAndPreservation(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	fake := &fakeProjectClient{scope: scope}
	authorization := ProjectCreateAuthorization{Confirmed: true}
	for attempt := range 2 {
		result, err := EnsureProjectWithClient(context.Background(), scope, authorization, fake)
		if err != nil || !result.Verified || result.Created != (attempt == 0) || fake.creates != 1 {
			t.Fatalf("unexpected project create/reuse: %+v, %v", result, err)
		}
	}
	if fake.request.Name != scope.Name || !maps.Equal(fake.request.Config, scope.DesiredConfig()) {
		t.Fatal("project creation omitted initial ownership/features")
	}
	fake.project.Config["user.foreign-field"] = "retained"
	if _, err := EnsureProjectWithClient(context.Background(), scope, authorization, fake); err != nil || fake.project.Config["user.foreign-field"] != "retained" || fake.creates != 1 {
		t.Fatal("project ensure changed existing metadata")
	}
}

func TestNativeProjectProvisioningFailsClosed(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	for _, mode := range []string{"remote", "wrong project", "denied", "foreign", "policy drift", "boundary race", "hidden collision", "create failure", "postflight ownership drift", "unconfirmed", "default project"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeProjectClient{scope: scope, mode: mode}
			selected := scope
			authorization := ProjectCreateAuthorization{Confirmed: true}
			switch mode {
			case "foreign":
				fake.project = &api.Project{Name: scope.Name}
			case "policy drift":
				config := scope.DesiredConfig()
				config["features.networks"] = "true"
				fake.project = &api.Project{Name: scope.Name, ProjectPut: api.ProjectPut{Config: config}}
			case "unconfirmed":
				authorization.Confirmed = false
			case "default project":
				selected.Name = "default"
			}
			result, err := EnsureProjectWithClient(context.Background(), selected, authorization, fake)
			var failure *ProjectProvisionError
			if !errors.As(err, &failure) || result.Verified || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("unsafe project failure: %+v, %v", result, err)
			}
			attempted := mode == "hidden collision" || mode == "create failure" || mode == "postflight ownership drift"
			if failure.CreateAttempted != attempted || failure.MayExist != attempted || (fake.creates != 0) != attempted {
				t.Fatalf("incorrect partial completion report: %+v, creates=%d", failure, fake.creates)
			}
			if mode == "postflight ownership drift" && !result.Created {
				t.Fatal("completed create lost from partial result")
			}
		})
	}
}

func TestNativeProjectProvisioningCancellationAndNoRollback(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		fake := &fakeProjectClient{scope: scope, cancel: cancel}
		if before {
			cancel()
		}
		result, err := EnsureProjectWithClient(ctx, scope, ProjectCreateAuthorization{Confirmed: true}, fake)
		cancel()
		if !errors.Is(err, context.Canceled) || result.Verified || result.Created == before {
			t.Fatalf("cancellation misreported: %+v, %v", result, err)
		}
		if before && fake.creates != 0 || !before && (fake.creates != 1 || fake.project == nil) {
			t.Fatal("cancellation caused unsafe mutation or automatic rollback")
		}
	}
}
