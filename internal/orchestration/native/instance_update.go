package native

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// InstanceUpdateClient exposes the bounded read surface and one full writable
// instance update. It has no lifecycle, device-specific, or removal methods.
type InstanceUpdateClient interface {
	InstanceReader
	UpdateInstance(string, api.InstancePut, string) (InstanceOperation, error)
}

type sdkInstanceUpdateClient struct{ server incus.InstanceServer }

func (c sdkInstanceUpdateClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return c.server.GetConnectionInfo()
}
func (c sdkInstanceUpdateClient) GetProject(name string) (*api.Project, string, error) {
	return c.server.GetProject(name)
}
func (c sdkInstanceUpdateClient) GetInstance(name string) (*api.Instance, string, error) {
	return c.server.GetInstance(name)
}
func (c sdkInstanceUpdateClient) GetImageAlias(name string) (*api.ImageAliasesEntry, string, error) {
	return c.server.GetImageAlias(name)
}
func (c sdkInstanceUpdateClient) GetImage(fingerprint string) (*api.Image, string, error) {
	return c.server.GetImage(fingerprint)
}
func (c sdkInstanceUpdateClient) UpdateInstance(name string, put api.InstancePut, etag string) (InstanceOperation, error) {
	return c.server.UpdateInstance(name, put, etag)
}

type InstanceUpdateResult struct {
	Identity InstanceIdentity `json:"identity"`
	Changed  bool             `json:"changed"`
	Verified bool             `json:"verified"`
}

// UpdateLocalInstance reconciles only explicitly supplied scalar config keys
// and description on an existing, verified owned instance. Lifecycle remains
// the responsibility of EnsureInstanceStateWithClient.
func UpdateLocalInstance(ctx context.Context, intent InstanceIntent) (InstanceUpdateResult, error) {
	if err := intent.Validate(); err != nil {
		return InstanceUpdateResult{}, err
	}
	server, cancel, err := connectLocal(ctx, intent.Identity.Project)
	if err != nil {
		return InstanceUpdateResult{}, err
	}
	defer cancel()
	defer server.Disconnect()
	return UpdateInstanceWithClient(ctx, intent, sdkInstanceUpdateClient{server: server})
}

// UpdateInstanceWithClient observes, validates, re-observes at the mutation
// boundary, submits one ETag-guarded full writable update, waits, and verifies.
func UpdateInstanceWithClient(ctx context.Context, intent InstanceIntent, client InstanceUpdateClient) (InstanceUpdateResult, error) {
	result := InstanceUpdateResult{Identity: intent.Identity}
	fail := func(stage string, mutated, uncertain bool, cause error) (InstanceUpdateResult, error) {
		return result, &ReconcileError{Stage: stage, Mutated: mutated, Uncertain: uncertain, cause: cause}
	}
	if err := intent.Validate(); err != nil {
		return fail("intent", false, false, err)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false, false, err)
	}
	info, err := client.GetConnectionInfo()
	if err != nil || localTransport(info, intent.Identity.Project) != nil {
		return fail("transport", false, false, fmt.Errorf("update requires the local socket bound to the selected project"))
	}
	scope, _ := incusscope.NewProjectScope(intent.Identity.Project)
	checkProject := func() error {
		project, _, err := client.GetProject(scope.Name)
		if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
			return fmt.Errorf("selected project ownership or policy unverified")
		}
		return ctx.Err()
	}
	if err := checkProject(); err != nil {
		return fail("project", false, false, err)
	}
	read := func() (*api.Instance, string, string, error) {
		instance, etag, err := client.GetInstance(intent.Identity.Name)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			return nil, "", "", fmt.Errorf("selected instance is absent")
		}
		if err != nil || instance == nil || etag == "" || instance.Name != intent.Identity.Name || instance.Project != scope.Name {
			return nil, "", "", fmt.Errorf("selected instance observation unavailable")
		}
		for key, want := range intent.ownership() {
			if instance.Config[key] != want {
				return nil, "", "", fmt.Errorf("existing workload ownership unverified")
			}
		}
		if deployment := instance.Config["user.aginctus.deployment"]; deployment != "" && deployment != scope.Name {
			return nil, "", "", fmt.Errorf("existing workload deployment mismatch")
		}
		if deployment, supplied := intent.Config["user.aginctus.deployment"]; supplied && instance.Config["user.aginctus.deployment"] != deployment {
			return nil, "", "", fmt.Errorf("existing workload deployment marker is immutable")
		}
		if instance.Status != "Running" && instance.Status != "Stopped" {
			return nil, "", "", fmt.Errorf("existing workload lifecycle unverified")
		}
		alias, _, err := client.GetImageAlias(intent.ImageAlias)
		if err != nil || alias == nil || alias.Name != intent.ImageAlias || !fingerprintPattern.MatchString(alias.Target) {
			return nil, "", "", fmt.Errorf("selected image alias unverified")
		}
		image, _, err := client.GetImage(alias.Target)
		if err != nil || image == nil || image.Fingerprint != alias.Target || image.Project != scope.Name || image.Type != intent.Type ||
			instance.Config["volatile.base_image"] != alias.Target {
			return nil, "", "", fmt.Errorf("existing workload image identity unverified")
		}
		return instance, etag, alias.Target, nil
	}
	before, etag, _, err := read()
	if err != nil {
		return fail("observation", false, false, err)
	}
	observation := instanceObservation(before, before.Config["volatile.base_image"])
	plan, err := PlanExistingInstance(intent, observation)
	if err != nil {
		return fail("planning", false, false, err)
	}
	if plan.Action == ReplacementRequired {
		return fail("planning", false, false, fmt.Errorf("instance drift requires replacement; no update applied"))
	}
	changed := intent.Description != before.Description
	for key, value := range intent.Config {
		if before.Config[key] != value {
			changed = true
		}
	}
	current, currentETag, _, err := read()
	if err != nil {
		return fail("mutation-boundary", false, false, err)
	}
	if currentETag != etag || !EqualObservation(observation, instanceObservation(current, current.Config["volatile.base_image"])) || !before.CreatedAt.Equal(current.CreatedAt) {
		return fail("mutation-boundary", false, false, fmt.Errorf("instance changed during planning"))
	}
	if !slices.Equal(current.Profiles, intent.Profiles) || current.Type != intent.Type {
		return fail("mutation-boundary", false, false, fmt.Errorf("instance drift requires replacement; no update applied"))
	}
	if err := checkProject(); err != nil {
		return fail("mutation-boundary", false, false, err)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", false, false, err)
	}
	if !changed {
		result.Verified = true
		return result, nil
	}
	put := api.InstancePut{
		Architecture: current.Architecture,
		Description:  current.Description,
		Config:       api.ConfigMap(maps.Clone(current.Config)),
		Devices:      api.DevicesMap(cloneDevices(current.Devices)),
		Ephemeral:    current.Ephemeral,
		Profiles:     slices.Clone(current.Profiles),
		Stateful:     current.Stateful,
	}
	for key, value := range intent.Config {
		put.Config[key] = value
	}
	put.Description = intent.Description
	op, err := client.UpdateInstance(intent.Identity.Name, put, currentETag)
	if err != nil {
		return fail("update", true, true, err)
	}
	if op == nil {
		return fail("operation", true, true, fmt.Errorf("update operation unavailable"))
	}
	if err := op.WaitContext(ctx); err != nil {
		return fail("operation", true, true, err)
	}
	result.Changed = true
	if err := checkProject(); err != nil {
		return fail("verification", true, true, err)
	}
	after, _, _, err := read()
	if err != nil {
		return fail("verification", true, true, err)
	}
	if after.Architecture != current.Architecture || after.Description != intent.Description || after.Type != current.Type ||
		after.Ephemeral != current.Ephemeral || after.Stateful != current.Stateful || after.Status != current.Status || !slices.Equal(after.Profiles, current.Profiles) ||
		!reflect.DeepEqual(after.Devices, current.Devices) {
		return fail("verification", true, true, fmt.Errorf("instance update postcondition unverified"))
	}
	for key, value := range intent.Config {
		if after.Config[key] != value {
			return fail("verification", true, true, fmt.Errorf("instance update config unverified"))
		}
	}
	for key, value := range current.Config {
		if _, explicit := intent.Config[key]; !explicit {
			if got, exists := after.Config[key]; !exists || got != value {
				return fail("verification", true, true, fmt.Errorf("unspecified instance config changed"))
			}
		}
	}
	result.Verified = true
	return result, nil
}
