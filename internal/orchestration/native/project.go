package native

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ProjectCreateAuthorization authorizes only create-if-absent under the existing
// operator confirmation contract. It is not authority to adopt, repair, remove,
// migrate or reconcile resources in an existing project, or activate a controller.
type ProjectCreateAuthorization struct {
	Confirmed bool
}

type ProjectProvisionResult struct {
	Project  string `json:"project"`
	Created  bool   `json:"created"`
	Verified bool   `json:"verified"`
}

// ProjectProvisionError reports uncertainty without claiming rollback. Once a
// create request is sent, the project may exist even if its response is lost.
type ProjectProvisionError struct {
	Stage           string
	CreateAttempted bool
	MayExist        bool
	cause           error
}

func (err *ProjectProvisionError) Error() string {
	return "native project provisioning failed at " + err.Stage + "; inspect exact project state before retry; no automatic rollback"
}

func (err *ProjectProvisionError) Unwrap() error { return err.cause }

type ProjectClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
	GetProject(string) (*api.Project, string, error)
	CreateProject(api.ProjectsPost) error
}

func EnsureLocalProject(ctx context.Context, scope incusscope.ProjectScope, authorization ProjectCreateAuthorization) (ProjectProvisionResult, error) {
	if _, err := incusscope.NewProjectScope(scope.Name); err != nil || !authorization.Confirmed {
		return ProjectProvisionResult{}, fmt.Errorf("native project creation requires explicit valid scope and confirmation")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return ProjectProvisionResult{}, err
	}
	client, err := incus.ConnectIncusUnixWithContext(ctx, "", &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return ProjectProvisionResult{}, observationFailure(ctx, "native local project connection unavailable")
	}
	defer client.Disconnect()
	return EnsureProjectWithClient(ctx, scope, authorization, client.UseProject(scope.Name))
}

// EnsureProjectWithClient has exactly one mutation: atomic POST of a new project
// with its initial ownership/features. It never edits/deletes an existing object.
// Preflight and mutation-boundary reads do not replace Incus collision handling.
func EnsureProjectWithClient(ctx context.Context, scope incusscope.ProjectScope, authorization ProjectCreateAuthorization, client ProjectClient) (ProjectProvisionResult, error) {
	result := ProjectProvisionResult{Project: scope.Name}
	fail := func(stage string, attempted bool) (ProjectProvisionResult, error) {
		return result, &ProjectProvisionError{Stage: stage, CreateAttempted: attempted, MayExist: attempted, cause: ctx.Err()}
	}
	if _, err := incusscope.NewProjectScope(scope.Name); err != nil || !authorization.Confirmed {
		return fail("authorization", false)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false)
	}
	connection, err := client.GetConnectionInfo()
	if err != nil || connection == nil || connection.Protocol != "incus" || !filepath.IsAbs(connection.SocketPath) || connection.Project != scope.Name {
		return fail("local-project-transport", false)
	}
	inspect := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		project, _, err := client.GetProject(scope.Name)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return false, nil
		}
		if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
			return false, fmt.Errorf("native project ownership or policy unverified")
		}
		return true, ctx.Err()
	}
	exists, err := inspect()
	if err != nil {
		return fail("preflight", false)
	}
	if exists {
		result.Verified = true
		return result, nil
	}
	// A second read refuses newly visible objects rather than adopting them
	// under the earlier absence observation. Atomic create closes the last race.
	exists, err = inspect()
	if err != nil || exists {
		return fail("mutation-boundary", false)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false)
	}
	if err := client.CreateProject(api.ProjectsPost{Name: scope.Name, ProjectPut: api.ProjectPut{Config: scope.DesiredConfig()}}); err != nil {
		return fail("create", true)
	}
	result.Created = true
	exists, err = inspect()
	if err != nil || !exists {
		return fail("postflight", true)
	}
	result.Verified = true
	return result, nil
}
