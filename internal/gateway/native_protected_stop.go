package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// StopLocalProtectedGateway is a separately authorized graceful stop. The
// caller supplies both observed creation timestamps; stopping never requires
// secret material or external registry connectivity and never removes storage.
func StopLocalProtectedGateway(ctx context.Context, grantPath, operatorPath string, postgresAt, liteLLMAt time.Time) (result NativeGatewayStopResult, id string, err error) {
	if postgresAt.IsZero() || liteLLMAt.IsZero() {
		return result, "", errProtectedGateway
	}
	e, err := openProtectedExecution(ctx, grantPath, operatorPath, "gateway-stop")
	if err != nil {
		return result, "", err
	}
	id = e.session.ID()
	defer func() { err = errors.Join(err, e.finish(ctx, err == nil)) }()
	guard := func(ctx context.Context) error {
		if e.guard(ctx) != nil || ObserveNativeGatewayAttachmentsWithReader(ctx, e.plan.top, e.plan.devices, e.sdk) != nil {
			return errProtectedGateway
		}
		for _, spec := range []NativeGatewayInstanceSpec{e.plan.pair.Postgres, e.plan.pair.LiteLLM} {
			image, at := e.candidate.Gateway.Postgres, postgresAt
			if spec.Role == "litellm" {
				image, at = e.candidate.Gateway.LiteLLM, liteLLMAt
			}
			want := native.GatewayOCIIdentity{Registry: image.Registry, Repository: image.Repository, ManifestDigest: image.ManifestDigest, ConfigDigest: image.ConfigDigest, IncusFingerprint: image.IncusFingerprint}
			evidence, err := e.provenance.ExistingImageEvidence(ctx, e.revision, spec.Role)
			if err != nil || native.ObserveGatewayImageReuseWithClient(ctx, native.GatewayImageAcquireIntent{Project: e.grant.ProtectedProject, Image: want}, image.Alias, evidence, e.sdk) != nil {
				return errProtectedGateway
			}
			i, _, err := e.sdk.GetInstance(spec.Name)
			if err != nil || !matchesNativeGatewayInstance(i, spec, image.IncusFingerprint) || !i.CreatedAt.Equal(at) || i.Stateful || !equalNativeDevices(i.ExpandedDevices, spec.Devices) {
				return errProtectedGateway
			}
		}
		return ctx.Err()
	}
	if err := guard(ctx); err != nil {
		return result, id, err
	}
	return stopProtectedPair(ctx, e, guard)
}

func stopProtectedPair(ctx context.Context, e *protectedExecution, guard func(context.Context) error) (NativeGatewayStopResult, string, error) {
	sdk := protectedStopSDK{InstanceServer: e.sdk, exec: e, ctx: ctx, guard: guard}
	result, err := StopNativeGatewayWithClient(ctx, e.plan.top, e.plan.devices, e.plan.pair, sdk, guard)
	return result, e.session.ID(), err
}

type protectedStopSDK struct {
	incus.InstanceServer
	exec  *protectedExecution
	ctx   context.Context
	guard func(context.Context) error
}

func (s protectedStopSDK) UpdateInstanceState(name string, req api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	role := ""
	switch name {
	case s.exec.plan.pair.Postgres.Name:
		role = "postgres"
	case s.exec.plan.pair.LiteLLM.Name:
		role = "litellm"
	}
	if role == "" || req.Action != "stop" || req.Force || s.guard(s.ctx) != nil || s.exec.session.BeginEffect(s.ctx, "stop-"+role) != nil || s.guard(s.ctx) != nil {
		return nil, errProtectedGateway
	}
	op, err := s.InstanceServer.UpdateInstanceState(name, req, etag)
	if err != nil || op == nil {
		return nil, errProtectedGateway
	}
	return runtimeTrackedOperation{op, s.exec.session, s.guard}, nil
}
