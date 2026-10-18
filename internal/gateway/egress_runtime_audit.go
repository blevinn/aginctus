package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// AuditEgressRuntime checks existing LiteLLM networking without changing
// instance state. It intentionally does not attempt address allocation.
func AuditEgressRuntime(ctx context.Context, spec Spec, query func(context.Context, ...string) ([]byte, error)) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if spec.EgressNetwork == "" {
		return fmt.Errorf("gateway egress network is not configured")
	}
	raw, err := query(ctx, "query", "/1.0/instances/litellm-1?project="+spec.Project+"&recursion=1")
	if err != nil {
		return fmt.Errorf("inspect LiteLLM instance: %w", err)
	}
	var instance struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Config map[string]string `json:"config"`
		ExpandedDevices map[string]map[string]string `json:"expanded_devices"`
	}
	if err := json.Unmarshal(raw, &instance); err != nil {
		return fmt.Errorf("decode LiteLLM instance metadata: %w", err)
	}
	if instance.Name != "litellm-1" ||
		instance.Config["user.aginctus.managed"] != "true" ||
		instance.Config["user.aginctus.resource"] != "compose-instance" ||
		instance.Config["user.aginctus.deployment"] != spec.Project {
		return fmt.Errorf("LiteLLM instance ownership is not verified")
	}
	if len(instance.ExpandedDevices) == 0 {
		return fmt.Errorf("LiteLLM expanded network devices are unavailable")
	}
	var management, egress int
	for _, device := range instance.ExpandedDevices {
		if device["type"] != "nic" {
			continue
		}
		switch device["network"] {
		case spec.Network:
			management++
			if device["ipv4.gateway"] != "none" {
				return fmt.Errorf("LiteLLM management NIC default gateway is not suppressed")
			}
		case spec.EgressNetwork:
			egress++
			ip := device["ipv4.address"]
			prefix, err := netip.ParsePrefix(ip)
			if err != nil || !prefix.Addr().Is4() || prefix.Addr() == prefix.Masked().Addr() {
				return fmt.Errorf("LiteLLM egress NIC lacks a valid static IPv4 host CIDR")
			}
			if spec.EgressIPv4Address != "" && ip != spec.EgressIPv4Address {
				return fmt.Errorf("LiteLLM egress IPv4 address differs from configured CIDR")
			}
			if strings.EqualFold(device["ipv4.gateway"], "none") {
				return fmt.Errorf("LiteLLM egress NIC default gateway is disabled")
			}
		}
	}
	if management != 1 || egress != 1 {
		return fmt.Errorf("LiteLLM requires exactly one management and one egress NIC")
	}
	// This verifies declarative configuration only, not live OCI routes or
	// reachability. Caller should run in-guest checks before inference.
	return nil
}
