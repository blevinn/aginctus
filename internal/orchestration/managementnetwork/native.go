package managementnetwork

import (
	"context"
	"errors"
	"strconv"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)

func (s Spec) NativeIntent() (native.NetworkIntent, error) {
	scope, err := incusscope.NewHostSharedScope("management-network")
	if err != nil {
		return native.NetworkIntent{}, err
	}
	if s.IPv4DHCPGateway == "" {
		s.IPv4DHCPGateway = "none"
	}
	i := native.NetworkIntent{Scope: scope, Name: s.Name, Type: "bridge", Config: map[string]string{
		"ipv4.address": s.IPv4Address, "ipv4.nat": strconv.FormatBool(s.IPv4NAT),
		"ipv4.routing": strconv.FormatBool(s.IPv4Routing), native.NetworkPolicyKey: s.IPv4DHCPGateway,
		"ipv6.address": s.IPv6Address,
	}}
	return i, i.Validate()
}

// VerifyNative checks a pre-provisioned bridge via a separately supplied host
// reader. The environment engine is never handed a bridge mutation capability.
func (s Spec) VerifyNative(ctx context.Context, reader native.NetworkReader) error {
	i, err := s.NativeIntent()
	if err != nil {
		return err
	}
	p, err := native.PlanNetworkWithReader(ctx, i, reader)
	if err != nil || p.Action != native.Unchanged {
		return errors.New("host management bridge unavailable or not converged")
	}
	return nil
}

// ExecuteNativeHost is exclusively for independently authorized host callers.
// The caller supplies the host client and holds host writer exclusion. Dry runs
// do not authorize or submit effects; force/adoption is not part of this path.
func (s Spec) ExecuteNativeHost(ctx context.Context, opt Options, client native.NetworkClient, guard func(context.Context, native.NetworkIntent, native.Action) error) (native.NetworkResult, error) {
	i, err := s.NativeIntent()
	if err != nil || client == nil || opt.Force || !opt.DryRun && guard == nil {
		return native.NetworkResult{}, errors.New("explicit host bridge intent, client and authority required")
	}
	var action native.Action
	switch opt.Operation {
	case "", "upsert":
		p, err := native.PlanNetworkWithReader(ctx, i, client)
		if err != nil {
			return native.NetworkResult{}, err
		}
		action = p.Action
	case "delete":
		p, err := native.PlanNetworkRemovalWithReader(ctx, i, client)
		if err != nil {
			return native.NetworkResult{}, err
		}
		action = native.Unchanged
		if p.Present {
			action = native.Delete
		}
	default:
		return native.NetworkResult{}, errors.New("unsupported host bridge operation")
	}
	if opt.DryRun || action == native.Unchanged {
		return native.NetworkResult{Network: i.Name, Action: action, Verified: action == native.Unchanged}, nil
	}
	// Never let the authority callback retain or change the executed intent.
	checked, _ := s.NativeIntent()
	if err := guard(ctx, checked, action); err != nil {
		return native.NetworkResult{}, err
	}
	client = networkEffectClient{client, action}
	if action == native.Delete {
		return native.RemoveNetworkWithClient(ctx, i, client)
	}
	return native.EnsureNetworkWithClient(ctx, i, client)
}

type networkEffectClient struct {
	native.NetworkClient
	action native.Action
}

func (c networkEffectClient) CreateNetwork(v api.NetworksPost) error {
	if c.action != native.Create {
		return errors.New("unplanned management bridge creation")
	}
	return c.NetworkClient.CreateNetwork(v)
}

func (c networkEffectClient) UpdateNetwork(name string, v api.NetworkPut, etag string) error {
	if c.action != native.Update || etag == "" {
		return errors.New("unplanned or unfenced management bridge update")
	}
	return c.NetworkClient.UpdateNetwork(name, v, etag)
}

func (c networkEffectClient) DeleteNetwork(name string) error {
	if c.action != native.Delete {
		return errors.New("unplanned management bridge deletion")
	}
	return c.NetworkClient.DeleteNetwork(name)
}

// NativePrerequisite returns a read-only graph stage for this host capability.
func (s Spec) NativePrerequisite(id, revision string, reader native.NetworkReader, verifyHostBinding func(context.Context) error) (orchestration.NativeResource, orchestration.NativeResourceBinding, error) {
	if _, err := s.NativeIntent(); err != nil || reader == nil || verifyHostBinding == nil {
		return orchestration.NativeResource{}, orchestration.NativeResourceBinding{}, errors.New("explicit authorized host bridge observer required")
	}
	return BindNativePrerequisite(id, revision, func(ctx context.Context) error {
		if err := verifyHostBinding(ctx); err != nil {
			return err
		}
		return s.VerifyNative(ctx, reader)
	})
}

// BindNativePrerequisite binds an independently authorized read-only bridge check.
func BindNativePrerequisite(id, revision string, verify func(context.Context) error) (orchestration.NativeResource, orchestration.NativeResourceBinding, error) {
	return orchestration.NativeResource{ID: id, Kind: orchestration.NativeManagementNetwork}, orchestration.NativeResourceBinding{
		Validate: func() error { return nil },
		Preview: func(ctx context.Context) (orchestration.NativePreview, error) {
			return orchestration.NativePreview{Actions: []orchestration.NativeEffect{{Action: native.Unchanged, Target: id}}}, verify(ctx)
		},
		Apply: func(context.Context, orchestration.RunNativeEffect) error { return nil },
		Conditions: orchestration.StepConditions{Postconditions: []orchestration.BoundCheck{
			orchestration.NativeObservationCheck(id, "bridge-converged", "The approved management bridge is converged", revision, func(ctx context.Context) (string, error) { return "", verify(ctx) }),
		}},
	}, nil
}
