package native

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// InstanceCreateResult reports a create request and its verified postcondition.
// Created is true once Incus accepted the operation, even if later verification
// fails; callers must inspect the instance and must not infer cleanup.
type InstanceCreateResult struct {
	Identity InstanceIdentity `json:"identity"`
	Created  bool             `json:"created"`
	Verified bool             `json:"verified"`
}

// InstanceCreateError identifies a failure stage without exposing daemon
// diagnostics. A submitted operation may have created the instance.
type InstanceCreateError struct {
	Stage     string
	Attempted bool
	Uncertain bool
	cause     error
}

func (err *InstanceCreateError) Error() string {
	return "native instance creation failed at " + err.Stage + "; inspect the selected instance before retry; no automatic cleanup"
}

func (err *InstanceCreateError) Unwrap() error { return err.cause }

// InstanceCreateClient is the exact local-project SDK surface needed for one
// create. It has no update, start, stop, delete, profile, or volume methods.
type InstanceCreateClient interface {
	InstanceReader
	CreateInstance(api.InstancesPost) (InstanceCreateOperation, error)
}

// InstanceCreateOperation is the SDK operation wait capability required here.
type InstanceCreateOperation interface {
	WaitContext(context.Context) error
}

// CreateLocalInstance opens the local Incus Unix socket and binds every SDK
// request to the intent's explicit non-default project. Creation leaves the
// instance stopped; EnsureRunning is not an implicit post-create transition.
func CreateLocalInstance(ctx context.Context, intent InstanceIntent) (InstanceCreateResult, error) {
	if err := intent.Validate(); err != nil {
		return InstanceCreateResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstanceCreateResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	server, err := incus.ConnectIncusUnixWithContext(ctx, "", &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return InstanceCreateResult{}, observationFailure(ctx, "local Incus instance creation unavailable")
	}
	defer server.Disconnect()
	return CreateInstanceWithClient(ctx, intent, sdkInstanceCreateClient{server: server.UseProject(intent.Identity.Project)})
}

type sdkInstanceCreateClient struct{ server incus.InstanceServer }

func (c sdkInstanceCreateClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return c.server.GetConnectionInfo()
}

func (c sdkInstanceCreateClient) GetProject(name string) (*api.Project, string, error) {
	return c.server.GetProject(name)
}

func (c sdkInstanceCreateClient) GetInstance(name string) (*api.Instance, string, error) {
	return c.server.GetInstance(name)
}

func (c sdkInstanceCreateClient) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	return c.server.GetImageAlias(name)
}

func (c sdkInstanceCreateClient) GetImage(fingerprint string) (*api.Image, string, error) {
	return c.server.GetImage(fingerprint)
}

func (c sdkInstanceCreateClient) CreateInstance(request api.InstancesPost) (InstanceCreateOperation, error) {
	return c.server.CreateInstance(request)
}

// CreateInstanceWithClient creates only an absent instance in a compatible
// owned project. Existing instances are always refused, even if their markers
// match. Creation uses exactly the validated intent; no existing devices or
// profiles are imported. This API does not update, adopt, replace, or delete.
func CreateInstanceWithClient(ctx context.Context, intent InstanceIntent, client InstanceCreateClient) (InstanceCreateResult, error) {
	result := InstanceCreateResult{Identity: intent.Identity}
	fail := func(stage string, attempted, uncertain bool, cause error) (InstanceCreateResult, error) {
		return result, &InstanceCreateError{Stage: stage, Attempted: attempted, Uncertain: uncertain, cause: cause}
	}
	if err := intent.Validate(); err != nil {
		return fail("intent", false, false, err)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false, false, err)
	}
	connection, err := client.GetConnectionInfo()
	if err != nil || connection == nil || connection.Protocol != "incus" || !filepath.IsAbs(connection.SocketPath) || connection.Project != intent.Identity.Project {
		return fail("transport", false, false, fmt.Errorf("local selected-project transport unavailable"))
	}
	scope, _ := incusscope.NewProjectScope(intent.Identity.Project)
	checkProject := func() error {
		project, _, err := client.GetProject(scope.Name)
		if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
			return fmt.Errorf("selected project ownership or policy unverified")
		}
		return ctx.Err()
	}
	checkImage := func() (string, error) {
		alias, _, err := client.GetImageAlias(intent.ImageAlias)
		if err != nil || alias == nil || alias.Name != intent.ImageAlias || !fingerprintPattern.MatchString(alias.Target) {
			return "", fmt.Errorf("selected project image alias unverified")
		}
		image, _, err := client.GetImage(alias.Target)
		if err != nil || image == nil || image.Fingerprint != alias.Target || image.Project != scope.Name || image.Type != intent.Type {
			return "", fmt.Errorf("selected project image identity unverified")
		}
		return alias.Target, ctx.Err()
	}
	checkAbsent := func() error {
		instance, _, err := client.GetInstance(intent.Identity.Name)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return ctx.Err()
		}
		if err != nil || instance == nil {
			return fmt.Errorf("selected instance absence unverified")
		}
		return fmt.Errorf("selected instance already exists")
	}
	if err := checkProject(); err != nil {
		return fail("project", false, false, err)
	}
	fingerprint, err := checkImage()
	if err != nil {
		return fail("image", false, false, err)
	}
	if err := checkAbsent(); err != nil {
		return fail("preflight", false, false, err)
	}
	// The last reads occur immediately at the mutation boundary. Incus's atomic
	// create response still arbitrates any race after the absence check.
	if err := checkProject(); err != nil {
		return fail("mutation-boundary", false, false, err)
	}
	currentFingerprint, err := checkImage()
	if err != nil || currentFingerprint != fingerprint {
		return fail("mutation-boundary", false, false, fmt.Errorf("selected image changed during preflight"))
	}
	if err := checkAbsent(); err != nil {
		return fail("mutation-boundary", false, false, err)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false, false, err)
	}
	request := api.InstancesPost{
		Name:   intent.Identity.Name,
		Type:   api.InstanceType(intent.Type),
		Source: api.InstanceSource{Type: "image", Fingerprint: fingerprint},
		InstancePut: api.InstancePut{
			Description: intent.Description,
			Config:      api.ConfigMap(maps.Clone(intent.Config)),
			Devices:     api.DevicesMap(cloneDevices(intent.Devices)),
			Profiles:    slices.Clone(intent.Profiles),
		},
	}
	operation, err := client.CreateInstance(request)
	if err != nil {
		return fail("create", true, true, err)
	}
	result.Created = true
	if operation == nil {
		return fail("operation", true, true, fmt.Errorf("create operation unavailable"))
	}
	if err := operation.WaitContext(ctx); err != nil {
		return fail("operation", true, true, err)
	}
	if err := checkProject(); err != nil {
		return fail("verification", true, true, err)
	}
	if err := verifyCreatedInstance(ctx, client, intent, fingerprint); err != nil {
		return fail("verification", true, true, err)
	}
	result.Verified = true
	return result, nil
}

func cloneDevices(input map[string]map[string]string) map[string]map[string]string {
	output := make(map[string]map[string]string, len(input))
	for name, properties := range input {
		output[name] = maps.Clone(properties)
	}
	return output
}

func verifyCreatedInstance(ctx context.Context, client InstanceCreateClient, intent InstanceIntent, fingerprint string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	instance, _, err := client.GetInstance(intent.Identity.Name)
	if err != nil || instance == nil || instance.Name != intent.Identity.Name || instance.Project != intent.Identity.Project || string(instance.Type) != intent.Type {
		return fmt.Errorf("created instance identity unverified")
	}
	if instance.Description != intent.Description || instance.Config["volatile.base_image"] != fingerprint {
		return fmt.Errorf("created instance configuration or image unverified")
	}
	for key, value := range intent.Config {
		if instance.Config[key] != value {
			return fmt.Errorf("created instance ownership or configuration unverified")
		}
	}
	for name, wanted := range intent.Devices {
		for key, value := range wanted {
			if instance.Devices[name][key] != value {
				return fmt.Errorf("created instance device configuration unverified")
			}
		}
	}
	if !slices.Equal(instance.Profiles, intent.Profiles) {
		return fmt.Errorf("created instance profiles unverified")
	}
	return nil
}
