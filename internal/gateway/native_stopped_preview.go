package gateway

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// NativeGatewayStoppedPreview has no authority or readiness semantics. The
// image stage must have completed (or proven both aliases already reusable).
type NativeGatewayStoppedPreview struct {
	CreateDatabaseVolume bool                    `json:"createDatabaseVolume"`
	Instances            NativeGatewayCreatePlan `json:"instances"`
}

// PreviewNativeEnvironmentGatewayStoppedWithReader observes both exact guest
// identities before the caller may create the database volume. The reader
// must already be authenticated to selected; this does not establish that
// transport or bind gateway intent to the authority's accepted revision.
func PreviewNativeEnvironmentGatewayStoppedWithReader(ctx context.Context, descriptor environment.Descriptor, selected environment.Binding,
	top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, images NativeGatewayCreateImages,
	reader NativeGatewayStartupReader,
) (NativeGatewayStoppedPreview, error) {
	identity, err := NativeGatewayIdentity(descriptor, selected)
	if err != nil || identity.Project != top.Project || identity.EnvironmentID != top.EnvironmentID || identity.DatabaseVolume != top.DatabaseVolume {
		return NativeGatewayStoppedPreview{}, fmt.Errorf("native gateway preview environment unverified")
	}
	expected, err := PlanNativeGatewayInstances(top, devices, pair.Postgres.ImageAlias, pair.LiteLLM.ImageAlias)
	if err != nil || !sameGatewayPair(pair, expected) {
		return NativeGatewayStoppedPreview{}, fmt.Errorf("native gateway preview guest intent unverified")
	}
	volumePresent, err := observeNativeGatewayAttachments(ctx, top, devices, reader, true)
	if err != nil {
		return NativeGatewayStoppedPreview{}, err
	}
	observed, err := observePinnedGatewayInstances(ctx, top, pair, images, reader)
	if err != nil {
		return NativeGatewayStoppedPreview{}, err
	}
	plan, err := PlanNativeGatewayCreate(pair, observed)
	if err != nil {
		return NativeGatewayStoppedPreview{}, err
	}
	if !volumePresent && plan.Steps[0].Action != NativeGatewayCreateStopped {
		return NativeGatewayStoppedPreview{}, fmt.Errorf("native gateway existing PostgreSQL guest has no verified database volume")
	}
	return NativeGatewayStoppedPreview{CreateDatabaseVolume: !volumePresent, Instances: plan}, ctx.Err()
}
