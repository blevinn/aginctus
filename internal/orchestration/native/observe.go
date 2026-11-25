package native

import (
	"context"
	"fmt"
	"maps"
	"slices"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// InstanceReader exposes only read operations. Injected implementations cannot
// be used by this path to create, start, update, remove or repair resources.
type InstanceReader interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
	GetProject(string) (*api.Project, string, error)
	GetInstance(string) (*api.Instance, string, error)
	GetImageAlias(string) (*api.ImageAliasesEntry, string, error)
	GetImage(string) (*api.Image, string, error)
}

type instanceImageReader interface {
	GetImageAlias(string) (*api.ImageAliasesEntry, string, error)
	GetImage(string) (*api.Image, string, error)
}

func resolveInstanceImage(intent InstanceIntent, reader instanceImageReader) (string, error) {
	fingerprint := intent.ImageFingerprint
	if fingerprint == "" {
		alias, _, err := reader.GetImageAlias(intent.ImageAlias)
		if err != nil || alias == nil || alias.Name != intent.ImageAlias || !fingerprintPattern.MatchString(alias.Target) {
			return "", fmt.Errorf("selected project image alias unverified")
		}
		fingerprint = alias.Target
	}
	image, _, err := reader.GetImage(fingerprint)
	if err != nil || image == nil || image.Fingerprint != fingerprint || image.Project != intent.Identity.Project || image.Type != intent.Type {
		return "", fmt.Errorf("selected project image identity unverified")
	}
	return fingerprint, nil
}

func PlanLocalExistingInstance(ctx context.Context, intent InstanceIntent) (InstancePlan, error) {
	if err := intent.Validate(); err != nil {
		return InstancePlan{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstancePlan{}, err
	}
	server, err := incus.ConnectIncusUnixWithContext(ctx, "", &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return InstancePlan{}, observationFailure(ctx, "local Incus observation unavailable")
	}
	defer server.Disconnect()
	return PlanWithInstanceReader(ctx, intent, server.UseProject(intent.Identity.Project))
}

func PlanWithInstanceReader(ctx context.Context, intent InstanceIntent, reader InstanceReader) (InstancePlan, error) {
	if err := intent.Validate(); err != nil {
		return InstancePlan{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstancePlan{}, err
	}
	if err := verifyInstanceTransport(ctx, reader, intent.Identity.Project); err != nil {
		return InstancePlan{}, observationFailure(ctx, "instance observation requires exact local project transport")
	}
	scope, _ := incusscope.NewProjectScope(intent.Identity.Project)
	checkProject := func() error {
		project, _, err := reader.GetProject(scope.Name)
		if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
			return observationFailure(ctx, "instance observation requires compatible owned project")
		}
		return ctx.Err()
	}
	if err := checkProject(); err != nil {
		return InstancePlan{}, err
	}
	instance, etag, err := reader.GetInstance(intent.Identity.Name)
	if err != nil || instance == nil || etag == "" {
		return InstancePlan{}, observationFailure(ctx, "selected existing instance unavailable")
	}
	fingerprint, err := resolveInstanceImage(intent, reader)
	if err != nil {
		return InstancePlan{}, observationFailure(ctx, "selected project image identity unverified")
	}
	observation := instanceObservation(instance, fingerprint)
	plan, err := PlanExistingInstance(intent, observation)
	if err != nil {
		return InstancePlan{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstancePlan{}, err
	}
	// Fail closed on changes during observation. This still is not a lock or
	// authorization for a later write, even when all observations agree.
	again, afterETag, err := reader.GetInstance(intent.Identity.Name)
	if err != nil || again == nil || etag != afterETag || !EqualObservation(observation, instanceObservation(again, fingerprint)) || !instance.CreatedAt.Equal(again.CreatedAt) {
		return InstancePlan{}, observationFailure(ctx, "selected instance changed during observation")
	}
	afterFingerprint, err := resolveInstanceImage(intent, reader)
	if err != nil || afterFingerprint != fingerprint {
		return InstancePlan{}, observationFailure(ctx, "selected image changed during observation")
	}
	if err := checkProject(); err != nil {
		return InstancePlan{}, err
	}
	return plan, nil
}

func observationFailure(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return fmt.Errorf("%s", message)
}

func instanceObservation(instance *api.Instance, fingerprint string) InstanceObservation {
	devices := make(map[string]map[string]string, len(instance.Devices))
	for name, device := range instance.Devices {
		devices[name] = maps.Clone(device)
	}
	return InstanceObservation{
		Identity: InstanceIdentity{Project: instance.Project, Name: instance.Name},
		Type:     string(instance.Type), State: instance.Status, Description: instance.Description,
		Config: maps.Clone(instance.Config), Devices: devices, Profiles: slices.Clone(instance.Profiles), ImageFingerprint: fingerprint,
	}
}
