package firewall

import (
	"context"
	"fmt"
	"net/netip"
)

// EgressRouteActions are narrow host observations. The gateway domain
// identifies the two forbidden guest bridges; host routing and effective
// firewalld zone policy are verified here without granting write authority.
type EgressRouteActions struct {
	Command func(context.Context, string, string, ...string) (string, error)
	EffectiveZone func(context.Context, string) (string, error)
}

// VerifiedEgressRoute refuses ambiguous policy routing, a guest bridge as
// default uplink, or an effective zone different from the configured uplink.
// The result remains observational; no firewall mutation occurs here.
func VerifiedEgressRoute(ctx context.Context, actions EgressRouteActions, uplink, egressBridge, managementBridge string, allowTailscaleRouting bool) (string, []netip.Prefix, error) {
	if actions.Command == nil || actions.EffectiveZone == nil || uplink == "" || egressBridge == "" || managementBridge == "" {
		return "", nil, fmt.Errorf("missing verified egress routing capability or boundary")
	}
	rules, err := actions.Command(ctx, "", "ip", "-j", "-4", "rule", "show")
	if err != nil { return "", nil, err }
	routes, err := actions.Command(ctx, "", "ip", "-j", "-4", "route", "show", "table", "all")
	if err != nil { return "", nil, err }
	iface, blocked, err := EgressRouting(rules, routes, allowTailscaleRouting)
	if err != nil { return "", nil, err }
	if iface == egressBridge || iface == managementBridge {
		return "", nil, fmt.Errorf("unsafe default route")
	}
	zone, err := actions.EffectiveZone(ctx, iface)
	if err != nil || zone != uplink {
		return "", nil, fmt.Errorf("default-route interface does not belong to configured uplink zone")
	}
	return iface, blocked, nil
}
