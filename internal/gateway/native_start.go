package gateway

import (
	"context"
	"fmt"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeGatewayStartClient is the bounded read and state-transition SDK
// capability. It cannot create/delete instances or mutate storage or images.
type NativeGatewayStartClient interface{ NativeGatewayStopClient }

// NativeGatewayServiceReadiness verifies actual guest service readiness using
// an independently reviewed authenticated, provider-independent probe. It
// must never return secret-bearing errors. Neither Incus Running nor
// pg_isready alone is sufficient proof of application authentication.
type NativeGatewayServiceReadiness interface {
	VerifyGatewayServiceReady(context.Context, string, string) error
}

// NativeGatewayStartResult preserves the exact verified stages when a later
// service fails. No automatic stop, rollback, retry, or volume cleanup occurs.
type NativeGatewayStartResult struct {
	Project string   `json:"project"`
	Started []string `json:"started,omitempty"`
	Ready   []string `json:"ready,omitempty"`
}
type NativeGatewayStartError struct {
	Stage     string
	Service   string
	Attempted bool
	Uncertain bool
}

func (e *NativeGatewayStartError) Error() string {
	return "native gateway start failed at " + e.Stage + " for " + e.Service + "; inspect project state and retained database volume before retry"
}

// StartNativeGatewayWithClient is an unactivated library orchestration method.
// Caller guard MUST enforce actor authority and a live exclusive writer lease;
// verifier MUST bind independently protected accepted revision and actual
// secret delivery; readiness MUST authenticate services without returning
// sensitive outputs. No production entry point supplies these yet.
//
// This fixed-name gateway topology currently supports *one project-owned*\n// gateway pair. The accepted revision is sourced from the project state\n// authority, while protected credential delivery is independently verified.\n// Environment-specific gateway instances require a separately designed\n// topology with unambiguous environment resource identities; do not relabel\n// this project-wide path or treat a host/root credential owner as its state\n// authority.\n//\n// Postgres is started and authenticated before LiteLLM; each subsequent
// operation rechecks all resource identities, lease and credential version.
// Existing Running states still require the authenticated readiness probe.
func StartNativeGatewayWithClient(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair,
	client NativeGatewayStartClient, guard func(context.Context) error,
	binding NativeGatewayRuntimeBinding, verifier NativeGatewayRuntimeVerifier, readiness NativeGatewayServiceReadiness,
) (NativeGatewayStartResult, error) {
	result := NativeGatewayStartResult{Project: top.Project}
	fail := func(stage, service string, attempted bool) (NativeGatewayStartResult, error) {
		return result, &NativeGatewayStartError{Stage: stage, Service: service, Attempted: attempted, Uncertain: attempted}
	}
	if guard == nil || verifier == nil || readiness == nil {
		return fail("authority", "gateway", false)
	}
	expected, err := PlanNativeGatewayInstances(top, devices, pair.Postgres.ImageAlias, pair.LiteLLM.ImageAlias)
	if err != nil || !sameGatewayPair(pair, expected) {
		return fail("intent", "gateway", false)
	}
	if err := binding.Validate(pair); err != nil {
		return fail("revision", "gateway", false)
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, client); err != nil {
			return err
		}
		observed, err := ObserveNativeGatewayInstancesWithReader(ctx, pair, client)
		if err != nil || len(observed) != 2 || observed[0].Status != NativeGatewayCompatible || observed[1].Status != NativeGatewayCompatible {
			return fmt.Errorf("native gateway instance ownership unverified")
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return fail("preflight", "gateway", false)
	}
	scope, _ := incusscope.NewProjectScope(top.Project)
	for _, spec := range []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM} {
		if err := guard(ctx); err != nil {
			return fail("authority", spec.Name, false)
		}
		if err := VerifyNativeGatewayRuntimeBinding(ctx, pair, binding, verifier); err != nil {
			return fail("credentials", spec.Name, false)
		}
		if err := check(); err != nil {
			return fail("mutation-boundary", spec.Name, false)
		}
		current, _, err := client.GetInstance(spec.Name)
		if err != nil || current == nil || current.Project != spec.Project || (current.Status != "Running" && current.Status != "Stopped") {
			return fail("state", spec.Name, false)
		}
		if current.Status == "Stopped" {
			transition, err := native.EnsureInstanceStateWithClient(ctx, native.InstanceLifecycleIntent{
				Scope: scope, Name: spec.Name, Resource: "gateway-instance", Role: spec.Role, State: native.InstanceRunning,
			}, client)
			if err != nil || !transition.Verified {
				return fail("transition", spec.Name, true)
			}
			if transition.Changed {
				result.Started = append(result.Started, spec.Name)
			}
		}
		if err := check(); err != nil {
			return fail("postcondition", spec.Name, current.Status == "Stopped")
		}
		// Require fresh credential evidence before checking authenticated
		// readiness; a rotated or revoked version blocks progression.
		if err := VerifyNativeGatewayRuntimeBinding(ctx, pair, binding, verifier); err != nil {
			return fail("credentials-post-start", spec.Name, current.Status == "Stopped")
		}
		if err := readiness.VerifyGatewayServiceReady(ctx, top.Project, spec.Role); err != nil {
			return fail("readiness", spec.Name, current.Status == "Stopped")
		}
		if err := ctx.Err(); err != nil {
			return fail("cancellation", spec.Name, current.Status == "Stopped")
		}
		result.Ready = append(result.Ready, spec.Name)
	}
	return result, nil
}
