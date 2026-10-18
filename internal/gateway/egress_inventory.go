package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
)

// auditEgressInstances inspects instance NIC configurations in every Incus
// project. Missing projects or device inventory is a hard failure, not proof
// that an IPv4 address is unassigned.
func auditEgressInstances(ctx context.Context, network string, run func(context.Context, ...string) ([]byte, error)) ([]netip.Addr, error) {
	raw, err := run(ctx, "query", "/1.0/projects?recursion=1")
	if err != nil {
		return nil, fmt.Errorf("inspect Incus projects: %w", err)
	}
	var projects []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &projects); err != nil || len(projects) == 0 {
		return nil, fmt.Errorf("incomplete Incus project inventory")
	}
	var used []netip.Addr
	for _, project := range projects {
		if !gatewayIDPattern.MatchString(project.Name) {
			return nil, fmt.Errorf("invalid Incus project inventory")
		}
		raw, err := run(ctx, "query", "/1.0/instances?recursion=2&project="+project.Name)
		if err != nil {
			return nil, fmt.Errorf("inspect instances in project %q: %w", project.Name, err)
		}
		var instances []struct {
			Devices map[string]map[string]string `json:"devices"`
			ExpandedDevices map[string]map[string]string `json:"expanded_devices"`
		}
		if err := json.Unmarshal(raw, &instances); err != nil {
			return nil, fmt.Errorf("decode instance inventory for project %q: %w", project.Name, err)
		}
		for _, instance := range instances {
			if instance.ExpandedDevices == nil {
				return nil, fmt.Errorf("instance inventory has no expanded device information in project %q", project.Name)
			}
			for _, device := range instance.ExpandedDevices {
				if device["type"] != "nic" || device["network"] != network {
					continue
				}
				value := device["ipv4.address"]
				if value == "" {
					// DHCP, dynamic or opaque configurations cannot establish
					// complete static-address inventory.
					return nil, fmt.Errorf("egress NIC without explicit IPv4 address found in project %q", project.Name)
				}
				addr, err := netip.ParseAddr(value)
				if err != nil {
					prefix, prefixErr := netip.ParsePrefix(value)
					if prefixErr != nil {
						return nil, fmt.Errorf("invalid egress NIC address in project %q", project.Name)
					}
					addr = prefix.Addr()
				}
				if !addr.Is4() {
					return nil, fmt.Errorf("non-IPv4 egress NIC assignment in project %q", project.Name)
				}
				used = append(used, addr)
			}
		}
	}
	return used, nil
}
