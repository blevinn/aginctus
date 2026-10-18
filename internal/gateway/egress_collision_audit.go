package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
)

// auditRequestedEgressAddress refuses addresses with existing DHCP leases or
// static assignments. An inventory snapshot is not an atomic reservation.
func auditRequestedEgressAddress(ctx context.Context, network, project string, requested netip.Addr, query func(context.Context, ...string) ([]byte, error)) error {
	raw, err := query(ctx, "query", "/1.0/networks/"+network+"/leases")
	if err != nil {
		return fmt.Errorf("inspect egress address leases: %w", err)
	}
	if string(raw) == "null" {
		return fmt.Errorf("egress lease inventory unavailable")
	}
	var leases []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(raw, &leases); err != nil || leases == nil {
		return fmt.Errorf("incomplete egress lease inventory")
	}
	for _, lease := range leases {
		assigned, err := netip.ParseAddr(lease.Address)
		if err != nil {
			return fmt.Errorf("invalid egress lease inventory")
		}
		if assigned == requested {
			return fmt.Errorf("requested egress address has an existing lease")
		}
	}
	projectsRaw, err := query(ctx, "query", "/1.0/projects?recursion=1")
	if err != nil {
		return fmt.Errorf("inspect Incus projects: %w", err)
	}
	var projects []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(projectsRaw, &projects); err != nil || len(projects) == 0 {
		return fmt.Errorf("incomplete project inventory")
	}
	for _, p := range projects {
		if !gatewayIDPattern.MatchString(p.Name) {
			return fmt.Errorf("invalid project inventory")
		}
		raw, err := query(ctx, "query", "/1.0/instances?recursion=2&project="+p.Name)
		if err != nil {
			return fmt.Errorf("inspect project %q instances: %w", p.Name, err)
		}
		var instances []struct {
			Name            string                       `json:"name"`
			Config          map[string]string            `json:"config"`
			ExpandedDevices map[string]map[string]string `json:"expanded_devices"`
		}
		if err := json.Unmarshal(raw, &instances); err != nil || instances == nil {
			return fmt.Errorf("incomplete instances in project %q", p.Name)
		}
		for _, instance := range instances {
			if instance.ExpandedDevices == nil {
				return fmt.Errorf("missing expanded device inventory")
			}
			for _, device := range instance.ExpandedDevices {
				if device["type"] != "nic" || device["network"] != network {
					continue
				}
				value := device["ipv4.address"]
				// Only the known, owned LiteLLM egress NIC may lack a static
				// address while preparing its assignment.
				if value == "" && p.Name == project && instance.Name == "litellm-1" &&
					instance.Config["user.aginctus.managed"] == "true" &&
					instance.Config["user.aginctus.resource"] == "compose-instance" &&
					instance.Config["user.aginctus.deployment"] == project {
					continue
				}
				if value == "" {
					return fmt.Errorf("another egress NIC has no explicit IPv4 address")
				}
				addr, err := netip.ParseAddr(value)
				if err != nil {
					prefix, prefixErr := netip.ParsePrefix(value)
					if prefixErr != nil {
						return fmt.Errorf("invalid static IPv4 device assignment")
					}
					addr = prefix.Addr()
				}
				if !addr.Is4() {
					return fmt.Errorf("unexpected non-IPv4 egress assignment")
				}
				if addr == requested {
					// An already configured address on the exact, owned
					// LiteLLM NIC is our desired state, not a collision.
					// Never exempt another NIC, even on the same instance.
					if p.Name == project && instance.Name == "litellm-1" &&
						instance.Config["user.aginctus.managed"] == "true" &&
						instance.Config["user.aginctus.resource"] == "compose-instance" &&
						instance.Config["user.aginctus.deployment"] == project &&
						device["network"] == network {
						continue
					}
					return fmt.Errorf("requested egress address is already assigned")
				}
			}
		}
	}
	return nil
}
