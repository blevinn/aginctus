package gateway

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

type NativeGatewayRecreationClient interface {
	NativeGatewayRemovalClient
	NativeGatewayStoppedCreateClient
}

type NativeGatewayRecreationResult struct {
	Removal  NativeGatewayRemovalResult `json:"removal"`
	Creation NativeGatewayStoppedResult `json:"creation"`
}

// RecreateNativeEnvironmentGatewayLiteLLMWithClient changes only the image of
// an explicitly selected stopped LiteLLM guest. Both images must already be
// proven in the same project. The replacement is a direct successor pin; the
// caller's guard authorizes this change across both revisions and approves
// each effect separately, including discard of the old guest's root state.
// The replacement stays stopped and has no credential handoff. An interrupted
// creation requires fresh observation and a separately authorized recovery.
func RecreateNativeEnvironmentGatewayLiteLLMWithClient(ctx context.Context, descriptor environment.Descriptor, selected environment.Binding,
	top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, images NativeGatewayCreateImages,
	old NativeGatewayRemoval, next environment.AcceptedRevision, replacement NativeGatewayCreateImage, client NativeGatewayRecreationClient,
	guard func(context.Context, environment.AcceptedRevision, environment.AcceptedRevision, native.Action, string) error,
) (NativeGatewayRecreationResult, error) {
	var out NativeGatewayRecreationResult
	id, err := NativeGatewayIdentity(descriptor, selected)
	if err != nil || guard == nil || old.Instance != id.LiteLLMInstance || !gatewayRevisionMatches(old.Revision, descriptor, selected) ||
		!gatewayRevisionMatches(next, descriptor, selected) || next.ParentID != old.Revision.ID || next.Generation != old.Revision.Generation+1 ||
		replacement.Expected == images.LiteLLM.Expected || replacement.Evidence.Alias == images.LiteLLM.Evidence.Alias ||
		replacement.Expected.IncusFingerprint == images.LiteLLM.Expected.IncusFingerprint || ctx.Err() != nil || !gatewayServerMatches(client, selected) {
		return out, fmt.Errorf("native gateway LiteLLM recreation intent or authority unverified")
	}
	newTop := top
	newTop.Services = slices.Clone(top.Services)
	if top.ValidateExecutionShape() != nil || top.EnvironmentID != id.EnvironmentID || top.Project != id.Project || top.DatabaseVolume != id.DatabaseVolume {
		return out, fmt.Errorf("native gateway LiteLLM recreation topology unverified")
	}
	newTop.Services[1].ImageReference = replacement.Expected.Registry + "/" + replacement.Expected.Repository + "@" + replacement.Expected.ManifestDigest
	newPair, err := PlanNativeGatewayInstances(newTop, devices, pair.Postgres.ImageAlias, replacement.Evidence.Alias)
	if err != nil {
		return out, fmt.Errorf("native gateway LiteLLM replacement plan unverified")
	}
	verifyReplacementImage := func(ctx context.Context) error {
		return native.ObserveGatewayImageReuseWithClient(ctx, native.GatewayImageAcquireIntent{Project: id.Project, Image: replacement.Expected}, newPair.LiteLLM.ImageAlias, replacement.Evidence, client)
	}
	if verifyReplacementImage(ctx) != nil {
		return out, fmt.Errorf("native gateway LiteLLM replacement image unverified")
	}
	pg, pgTag, err := client.GetInstance(id.PostgresInstance)
	if err != nil || pgTag == "" || !matchesNativeGatewayInstance(pg, newPair.Postgres, images.Postgres.Expected.IncusFingerprint) || pg.CreatedAt.IsZero() || pg.Status != "Stopped" {
		return out, fmt.Errorf("native gateway retained PostgreSQL guest unverified")
	}
	pool := newPair.Postgres.Devices["database"]["pool"]
	volume, _, err := client.GetStoragePoolVolume(pool, "custom", id.DatabaseVolume)
	if err != nil || verifyGatewayVolume(volume, id.Project, id.EnvironmentID, id.DatabaseVolume, id.PostgresInstance) != nil || volume.CreatedAt.IsZero() || len(volume.UsedBy) != 1 {
		return out, fmt.Errorf("native gateway retained PostgreSQL volume unverified")
	}
	pgCreation := pg.CreatedAt
	pgUUID := pg.Config["volatile.uuid"]
	volumeCreated := volume.CreatedAt
	volumeConfig, volumeDescription := maps.Clone(volume.Config), volume.Description
	retained := func() bool {
		current, etag, err := client.GetInstance(id.PostgresInstance)
		if err != nil || etag != pgTag || !matchesNativeGatewayInstance(current, newPair.Postgres, images.Postgres.Expected.IncusFingerprint) ||
			!current.CreatedAt.Equal(pgCreation) || current.Config["volatile.uuid"] != pgUUID || current.Status != "Stopped" {
			return false
		}
		v, _, err := client.GetStoragePoolVolume(pool, "custom", id.DatabaseVolume)
		return err == nil && verifyGatewayVolume(v, id.Project, id.EnvironmentID, id.DatabaseVolume, id.PostgresInstance) == nil && len(v.UsedBy) == 1 &&
			v.CreatedAt.Equal(volumeCreated) && maps.Equal(volumeConfig, v.Config) && v.Description == volumeDescription
	}
	out.Removal, err = RemoveNativeEnvironmentGatewayServiceWithClient(ctx, descriptor, selected, top, devices, pair, images, old, client,
		func(ctx context.Context, target NativeGatewayRemoval) error {
			if verifyReplacementImage(ctx) != nil || !retained() {
				return fmt.Errorf("native gateway recreation prerequisites changed")
			}
			return guard(ctx, old.Revision, next, native.Delete, target.Instance)
		})
	if err != nil {
		return out, err
	}
	images.LiteLLM = replacement
	// Reuse the normal create primitive, but expose only the replacement
	// submission. A concurrently missing PostgreSQL must not be recreated.
	if !retained() {
		return out, fmt.Errorf("native gateway PostgreSQL retention unverified after removal")
	}
	out.Creation, err = createStoppedGateway(ctx, newTop, devices, newPair, images, client, func(ctx context.Context, name string) error {
		if name != id.LiteLLMInstance || !gatewayServerMatches(client, selected) || !retained() || verifyReplacementImage(ctx) != nil {
			return fmt.Errorf("native gateway recreation prerequisites changed after removal")
		}
		// Do not retain a trusted guard's opaque error in the diagnostic chain.
		if guard(ctx, old.Revision, next, native.Create, name) != nil {
			return fmt.Errorf("native gateway replacement authority unverified")
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if !retained() || !slices.Equal(out.Creation.Created, []string{id.LiteLLMInstance}) {
		return out, fmt.Errorf("native gateway stopped replacement or retained PostgreSQL unverified")
	}
	return out, nil
}
