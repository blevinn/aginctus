package firewall

import (
	"context"
	"fmt"
)

// AuthorizedEgressActions supplies host privilege, an exclusive process lock,
// exact bridge configuration and independently discovered network authority.
// Configuration/route claims never substitute for effective root authority.
type AuthorizedEgressActions struct {
	EffectiveUID func() int
	Lock func(string) (func(), error)
	Bridge func() (string, error)
}

// ReconcileAuthorizedEgress owns the privileged host sequence. Retain the
// lock while both independent discovery and the runtime D-Bus transaction
// complete; an explicit revocation never invokes discovery.
func ReconcileAuthorizedEgress(ctx context.Context, revoke bool, actions AuthorizedEgressActions, discover func(context.Context) (EgressGrant, error)) error {
	if actions.EffectiveUID == nil || actions.EffectiveUID() != 0 {
		return fmt.Errorf("egress firewall reconciliation requires root; no authorization prompt attempted")
	}
	if actions.Lock == nil || actions.Bridge == nil || discover == nil {
		return fmt.Errorf("egress firewall: required host capabilities unavailable")
	}
	unlock, err := actions.Lock("/run/aginctus-firewall")
	if err != nil { return fmt.Errorf("egress firewall: exclusive host lock unavailable") }
	if unlock == nil { return fmt.Errorf("egress firewall: lock release unavailable") }
	defer unlock()
	bridge, err := actions.Bridge()
	if err != nil || bridge == "" { return fmt.Errorf("configured egress bridge required") }
	grant := discover
	if revoke {
		grant = func(context.Context) (EgressGrant, error) { return EgressGrant{}, nil }
	}
	if err := ReconcileEgress(ctx, Busctl{}, EgressStateFile("/run/aginctus-firewall/state.json"), bridge, grant); err != nil {
		return fmt.Errorf("egress firewall: %w", err)
	}
	return nil
}
