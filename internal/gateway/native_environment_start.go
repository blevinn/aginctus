package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeEnvironmentGatewayStartClient exposes only observation and lifecycle
// state transitions; no create/update/delete for images, instances or volumes.
type NativeEnvironmentGatewayStartClient interface {
	NativeGatewayStopClient
	GetServer() (*api.Server, string, error)
}

// The existing lifecycle primitive does the instance-level preflight. Recheck
// gateway-specific prerequisites at its actual submission boundary as well.
type gatewayLifecycleBoundary struct {
	native.InstanceLifecycleClient
	before func() error
}

func (c gatewayLifecycleBoundary) UpdateInstanceState(name string, state api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	if err := c.before(); err != nil {
		return nil, err
	}
	return c.InstanceLifecycleClient.UpdateInstanceState(name, state, etag)
}

// NativeEnvironmentGatewayReadiness is a separately trusted service probe.
// A successful Incus Running observation is never a readiness result. The
// implementation must authenticate each service, avoid secret-bearing errors,
// and bind its guest target to the exact environment resource identities.
type NativeEnvironmentGatewayReadiness interface {
	VerifyGatewayServiceReady(context.Context, NativeGatewayResourceIdentity, string) error
}

type NativeEnvironmentGatewayStartResult struct {
	Project       string   `json:"project"`
	EnvironmentID string   `json:"environmentId"`
	Started       []string `json:"started,omitempty"`
	Ready         []string `json:"ready,omitempty"`
}

type NativeEnvironmentGatewayStartError struct {
	Stage     string
	Service   string
	Attempted bool
	Uncertain bool
}

func (e *NativeEnvironmentGatewayStartError) Error() string {
	return "environment gateway start failed at " + e.Stage + " for " + e.Service + "; inspect selected environment and retained PostgreSQL volume before retry"
}

// StartNativeEnvironmentGatewayWithClient is a library-only, unactivated
// implementation. The caller must enforce a live writer fence in guard,
// a project-scoped independently accepted environment revision and verified
// guest credential delivery in verifier, and authenticated readiness in ready.
// None may self-assert authority. No production caller is provided here.
//
// PostgreSQL must be running AND authenticated before LiteLLM may start.
// Every stage rechecks the same immutable environment/Incus identity and
// the current accepted-revision/credential version; failures preserve partial
// results without blind retry, rollback, or deletion of persistent storage.
func StartNativeEnvironmentGatewayWithClient(
	ctx context.Context, descriptor environment.Descriptor, selected environment.Binding,
	top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair,
	binding NativeGatewayRuntimeBinding, client NativeEnvironmentGatewayStartClient,
	guard func(context.Context) error, verifier NativeGatewayDeliveryVerifier,
	ready NativeEnvironmentGatewayReadiness,
) (NativeEnvironmentGatewayStartResult, error) {
	result := NativeEnvironmentGatewayStartResult{Project: top.Project, EnvironmentID: top.EnvironmentID}
	fail := func(stage, service string, attempted bool) (NativeEnvironmentGatewayStartResult, error) {
		return result, &NativeEnvironmentGatewayStartError{
			Stage: stage, Service: service, Attempted: attempted, Uncertain: attempted && (stage == "transition" || stage == "postcondition"),
		}
	}
	if guard == nil || verifier == nil || ready == nil {
		return fail("authority", "gateway", false)
	}
	identity, err := binding.physicalIdentity(descriptor, selected)
	if err != nil || top.Project != identity.Project || top.EnvironmentID != identity.EnvironmentID ||
		top.DatabaseVolume != identity.DatabaseVolume {
		return fail("environment", "gateway", false)
	}
	expected, err := PlanNativeGatewayInstances(top, devices, pair.Postgres.ImageAlias, pair.LiteLLM.ImageAlias)
	if err != nil || !sameGatewayPair(pair, expected) || binding.Validate(descriptor, selected, pair) != nil {
		return fail("intent", "gateway", false)
	}
	scope, _ := incusscope.NewProjectScope(identity.Project)
	check := func(running ...int) bool {
		if err := ctx.Err(); err != nil {
			return false
		}
		if !gatewayServerMatches(client, selected) {
			return false
		}
		if err := ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, client); err != nil {
			return false
		}
		obs, err := observePinnedGatewayInstances(ctx, top, pair, binding.Images, client)
		if err != nil || len(obs) != 2 || obs[0].Status != NativeGatewayCompatible || obs[1].Status != NativeGatewayCompatible {
			return false
		}
		for i, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
			pin := [2]NativeGatewayCreateImage{binding.Images.Postgres, binding.Images.LiteLLM}[i]
			instance, _, err := client.GetInstance(spec.Name)
			if err != nil || !matchesNativeGatewayInstance(instance, spec, pin.Expected.IncusFingerprint) ||
				(instance.ExpandedDevices != nil && !equalNativeDevices(instance.ExpandedDevices, spec.Devices)) ||
				(instance.Status != "Stopped" && instance.Status != "Running") {
				return false
			}
			for _, idx := range running {
				if idx == i && instance.Status != "Running" {
					return false
				}
			}
			if binding.Protected != nil && !gatewayLauncherMatches(instance, spec, pair.Postgres.Name) {
				return false
			}
			for _, config := range []map[string]string{instance.Config, instance.ExpandedConfig} {
				for key := range config {
					if strings.HasPrefix(key, "environment.") || strings.HasPrefix(key, "raw.") || strings.HasPrefix(key, "security.") {
						return false
					}
				}
			}
		}
		return ctx.Err() == nil
	}
	if !check() {
		return fail("preflight", "gateway", false)
	}
	for i, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
		if err := guard(ctx); err != nil {
			return fail("authority", spec.Name, false)
		}
		if err := VerifyNativeGatewayRuntimeBinding(ctx, descriptor, selected, pair, binding, verifier); err != nil {
			return fail("credentials", spec.Name, false)
		}
		var deps []int
		if i == 1 {
			deps = []int{0}
		}
		if !check(deps...) {
			return fail("mutation-boundary", spec.Name, false)
		}
		attempted := false
		stage := "transition"
		boundary := gatewayLifecycleBoundary{InstanceLifecycleClient: client, before: func() error {
			if !check(deps...) {
				stage = "mutation-boundary"
				return fmt.Errorf("gateway prerequisites changed")
			}
			if VerifyNativeGatewayRuntimeBinding(ctx, descriptor, selected, pair, binding, verifier) != nil {
				stage = "credentials"
				return fmt.Errorf("gateway delivery unverified")
			}
			if err := guard(ctx); err != nil {
				stage = "authority"
				return err
			}
			if err := ctx.Err(); err != nil {
				stage = "cancellation"
				return err
			}
			attempted = true
			return nil
		}}
		transition, err := native.EnsureInstanceStateWithClient(ctx, native.InstanceLifecycleIntent{
			Scope: scope, Name: spec.Name, Resource: "gateway-instance", Role: spec.Role, EnvironmentID: spec.EnvironmentID, State: native.InstanceRunning,
		}, boundary)
		if err != nil || !transition.Verified {
			return fail(stage, spec.Name, attempted)
		}
		if transition.Changed {
			result.Started = append(result.Started, spec.Name)
		}
		if !check(append(deps, i)...) {
			return fail("postcondition", spec.Name, attempted)
		}
		if err := VerifyNativeGatewayRuntimeBinding(ctx, descriptor, selected, pair, binding, verifier); err != nil {
			return fail("credentials-post-start", spec.Name, attempted)
		}
		if err := ready.VerifyGatewayServiceReady(ctx, identity, spec.Role); err != nil {
			return fail("readiness", spec.Name, attempted)
		}
		if err := ctx.Err(); err != nil {
			return fail("cancellation", spec.Name, attempted)
		}
		if VerifyNativeGatewayRuntimeBinding(ctx, descriptor, selected, pair, binding, verifier) != nil || guard(ctx) != nil {
			return fail("readiness-binding", spec.Name, attempted)
		}
		if !check(append(deps, i)...) {
			return fail("postcondition", spec.Name, attempted)
		}
		result.Ready = append(result.Ready, spec.Name)
	}
	return result, nil
}
