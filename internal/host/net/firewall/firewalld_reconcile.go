package firewall

import (
	"context"
	"fmt"
	"strings"
)

// FirewalldReconcileActions expose only the host capabilities needed for the
// dedicated DROP zone. This domain operation never reads CLI arguments.
type FirewalldReconcileActions struct {
	EffectiveUID func() int
	Command func(context.Context, string, string, ...string) (string, error)
	Discover func(context.Context) (Policy, error)
}

type FirewalldResult struct {
	Interface string
	AlreadyReconciled bool
}

// ReconcileOwnedFirewalld checks the permanent and effective DROP target,
// rejects foreign grants and clears stale owned exceptions before installing
// only DNS/DHCP permits. No broad chain/table flush or network adoption.
func ReconcileOwnedFirewalld(ctx context.Context, zoneName string, actions FirewalldReconcileActions) (FirewalldResult, error) {
	if actions.EffectiveUID == nil || actions.EffectiveUID() != 0 {
		return FirewalldResult{}, fmt.Errorf("firewall reconciliation requires root (host INPUT mutation)")
	}
	if actions.Command == nil || actions.Discover == nil {
		return FirewalldResult{}, fmt.Errorf("missing host firewall inspection capabilities")
	}
	run := func(args ...string) (string, error) {
		output, err := actions.Command(ctx, "", "firewall-cmd", args...)
		return strings.TrimSpace(output), err
	}
	fail := func(message string) (FirewalldResult, error) {
		return FirewalldResult{}, fmt.Errorf("%s", message)
	}

	if ValidateFirewalldZone(zoneName) != nil {
		return fail("invalid configured Aginctus firewalld zone")
	}
	zones, err := run("--get-zones")
	if err != nil || !ContainsWord(zones, zoneName) {
		return fail("configured Aginctus-managed firewalld zone is missing; activate the NixOS module")
	}
	zone := "--zone=" + zoneName
	// firewalld exposes --get-target only in permanent configuration.
	// Also inspect the active runtime zone: a permanent DROP alone does not
	// prove that the currently enforced target is still restrictive.
	target, err := run("--permanent", zone, "--get-target")
	if err != nil || target != "DROP" {
		return fail("Aginctus firewalld permanent zone must have the DROP target")
	}
	activeZone, err := run(zone, "--list-all")
	if err != nil || !HasFirewalldDropTarget(activeZone) {
		return fail("Aginctus firewalld runtime zone must have the DROP target")
	}
	// We own this dedicated zone. Refuse unexpected runtime configuration
	// instead of accidentally extending somebody else's host exceptions.
	for _, flag := range []string{"--list-services", "--list-ports", "--list-sources", "--list-protocols", "--list-forward-ports"} {
		output, err := run(zone, flag)
		if err != nil || output != "" {
			return fail("Aginctus firewalld zone contains unexpected permissions")
		}
	}
	current, err := run(zone, "--list-rich-rules")
	if err != nil {
		return fail("cannot inspect Aginctus firewalld rich rules")
	}
	owned, discoveryErr := actions.Discover(ctx)
	var rules []string
	if discoveryErr == nil {
		rules, err = FirewalldRules(&owned)
		if err != nil {
			return fail("invalid owned-bridge firewalld policy")
		}
	}
	interfaces, err := run(zone, "--list-interfaces")
	if err != nil {
		return fail("cannot inspect Aginctus firewalld interface bindings")
	}
	// Leave an unchanged, verified runtime policy intact. The periodic timer
	// must not interrupt active DNS connections every 30 seconds.
	if discoveryErr == nil && SameRichRules(current, rules) &&
		strings.TrimSpace(interfaces) == owned.Interface {
		return FirewalldResult{Interface: owned.Interface, AlreadyReconciled: true}, nil
	}

	// Remove prior exceptions before admitting any new identity. If network
	// discovery failed, this clears old access and leaves the DROP zone.
	for _, rule := range strings.Split(current, "\n") {
		if rule == "" {
			continue
		}
		if _, err := run(zone, "--remove-rich-rule="+rule); err != nil {
			return fail("cannot clear stale Aginctus firewalld exceptions")
		}
	}
	if discoveryErr != nil {
		return fail(fmt.Sprintf("Aginctus firewalld exceptions cleared (fail closed): %v", discoveryErr))
	}
	for _, iface := range strings.Fields(interfaces) {
		if iface != owned.Interface {
			return fail("Aginctus firewalld zone binds an unexpected interface; exceptions remain cleared")
		}
	}
	// Add the narrow permissions before binding the verified bridge. The
	// zone is dedicated to Aginctus; no non-owned ingress is admitted.
	for _, rule := range rules {
		if _, err := run(zone, "--add-rich-rule="+rule); err != nil {
			// Best-effort rollback of any partial additions. The DROP zone
			// remains in effect even if the D-Bus backend is unavailable.
			for _, prior := range rules {
				_, _ = run(zone, "--remove-rich-rule="+prior)
			}
			return fail("cannot install Aginctus firewalld DNS/DHCP permissions")
		}
	}
	if !ContainsWord(interfaces, owned.Interface) {
		if _, err := run(zone, "--add-interface="+owned.Interface); err != nil {
			for _, rule := range rules {
				_, _ = run(zone, "--remove-rich-rule="+rule)
			}
			return fail("cannot bind verified Incus bridge to Aginctus firewalld zone")
		}
	}
	return FirewalldResult{Interface: owned.Interface}, nil
}

