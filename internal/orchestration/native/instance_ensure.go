package native

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// InstanceEnsureClient has no removal, volume, network or profile mutations.
type InstanceEnsureClient interface {
	InstanceCreateClient
	InstanceUpdateClient
	InstanceLifecycleClient
}

type sdkInstanceEnsureClient struct{ sdkInstanceUpdateClient }

func (c sdkInstanceEnsureClient) CreateInstance(post api.InstancesPost) (InstanceCreateOperation, error) {
	return c.server.CreateInstance(post)
}

func (c sdkInstanceEnsureClient) UpdateInstanceState(name string, put api.InstanceStatePut, etag string) (InstanceOperation, error) {
	return c.server.UpdateInstanceState(name, put, etag)
}

// InstanceEnsureResult retains completed stages if a later operation fails.
// Plan is a redacted review artifact, never authorization for subsequent writes.
type InstanceEnsureResult struct {
	Plan     InstancePlan `json:"plan"`
	Created  bool         `json:"created"`
	Updated  bool         `json:"updated"`
	Started  bool         `json:"started"`
	Verified bool         `json:"verified"`
}

// PlanInstanceWithReader extends existing-instance planning with verified absence.
// Neither this read-only capability nor its plan confers actor authority.
func PlanInstanceWithReader(ctx context.Context, intent InstanceIntent, reader InstanceReader) (InstancePlan, error) {
	if err := intent.Validate(); err != nil {
		return InstancePlan{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstancePlan{}, err
	}
	info, err := reader.GetConnectionInfo()
	if err != nil || localTransport(info, intent.Identity.Project) != nil {
		return InstancePlan{}, fmt.Errorf("instance planning requires local selected-project transport")
	}
	instance, _, err := reader.GetInstance(intent.Identity.Name)
	if err == nil && instance != nil {
		return PlanWithInstanceReader(ctx, intent, reader)
	}
	if !api.StatusErrorCheck(err, http.StatusNotFound) {
		return InstancePlan{}, fmt.Errorf("instance existence unverified")
	}
	scope, _ := incusscope.NewProjectScope(intent.Identity.Project)
	var fingerprint string
	for range 2 {
		if err := ctx.Err(); err != nil {
			return InstancePlan{}, err
		}
		project, _, err := reader.GetProject(scope.Name)
		if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
			return InstancePlan{}, fmt.Errorf("selected project policy unverified")
		}
		alias, _, err := reader.GetImageAlias(intent.ImageAlias)
		if err != nil || alias == nil || alias.Name != intent.ImageAlias || !fingerprintPattern.MatchString(alias.Target) || fingerprint != "" && fingerprint != alias.Target {
			return InstancePlan{}, fmt.Errorf("selected project image alias unverified")
		}
		fingerprint = alias.Target
		image, _, err := reader.GetImage(fingerprint)
		if err != nil || image == nil || image.Project != scope.Name || image.Fingerprint != fingerprint || image.Type != intent.Type {
			return InstancePlan{}, fmt.Errorf("selected project image unverified")
		}
		if _, _, err := reader.GetInstance(intent.Identity.Name); !api.StatusErrorCheck(err, http.StatusNotFound) {
			return InstancePlan{}, fmt.Errorf("instance absence changed during planning")
		}
	}
	if err := ctx.Err(); err != nil {
		return InstancePlan{}, err
	}
	return InstancePlan{Identity: intent.Identity, Action: Create}, nil
}

// EnsureLocalInstance composes guarded primitives for one explicit intent.
// Callers own actor authorization and writer exclusion. Dry-run only observes.
// EnsureRunning=false preserves existing state; it never means stop.
func EnsureLocalInstance(ctx context.Context, intent InstanceIntent, dryRun bool) (InstanceEnsureResult, error) {
	if err := intent.Validate(); err != nil {
		return InstanceEnsureResult{}, err
	}
	server, cancel, err := connectLocal(ctx, intent.Identity.Project)
	if err != nil {
		return InstanceEnsureResult{}, err
	}
	defer cancel()
	defer server.Disconnect()
	return EnsureInstanceWithClient(ctx, intent, dryRun, sdkInstanceEnsureClient{sdkInstanceUpdateClient{server: server}})
}

// EnsureInstanceWithClient stops at the first failure, without rollback or
// retry. Each primitive independently re-observes at its mutation boundary.
func EnsureInstanceWithClient(ctx context.Context, intent InstanceIntent, dryRun bool, client InstanceEnsureClient) (InstanceEnsureResult, error) {
	plan, err := PlanInstanceWithReader(ctx, intent, client)
	result := InstanceEnsureResult{Plan: plan}
	if err != nil {
		return result, err
	}
	if plan.Action == ReplacementRequired {
		return result, fmt.Errorf("instance requires replacement; no ensure applied")
	}
	if dryRun {
		return result, nil
	}
	if plan.Action == Create {
		created, err := CreateInstanceWithClient(ctx, intent, client)
		result.Created = created.Created
		if err != nil {
			return result, err
		}
	} else {
		updated, err := UpdateInstanceWithClient(ctx, intent, client)
		result.Updated = updated.Changed
		if err != nil {
			return result, err
		}
	}
	if intent.EnsureRunning {
		// Revalidate config, attachments, image and project before lifecycle.
		ready, err := PlanWithInstanceReader(ctx, intent, client)
		if err != nil {
			return result, err
		}
		if ready.Action == ReplacementRequired || len(ready.Changes) != 0 {
			return result, fmt.Errorf("instance changed before lifecycle; inspect before retry")
		}
		scope, _ := incusscope.NewProjectScope(intent.Identity.Project)
		state, err := EnsureInstanceStateWithClient(ctx, InstanceLifecycleIntent{
			Scope: scope, Name: intent.Identity.Name, Resource: "workload", Role: "agent-workload", WorkloadID: intent.WorkloadID, State: InstanceRunning,
		}, client)
		result.Started = state.Changed
		if err != nil {
			return result, err
		}
	}
	final, err := PlanWithInstanceReader(ctx, intent, client)
	if err != nil {
		return result, err
	}
	if final.Action != Unchanged {
		return result, fmt.Errorf("instance ensure postcondition unverified")
	}
	result.Verified = true
	return result, nil
}
