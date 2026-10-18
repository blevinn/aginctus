package gateway

import (
	"context"
	"fmt"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

// EgressInstanceEditor updates only the existing gateway instance, under
// Incus's optimistic-concurrency token. The caller must ensure it is stopped.
type EgressInstanceEditor interface {
	GetInstance(string) (*api.Instance, string, error)
	UpdateInstance(string, api.InstancePut, string) (incusclient.Operation, error)
}

func reconcileStoppedEgressNICs(ctx context.Context, spec Spec, read func(context.Context, ...string) ([]byte, error), editor EgressInstanceEditor) error {
	// Reuse all bridge, lease, cross-project NIC, topology and ownership
	// guards already enforced by the read-only reconciliation planner.
	if _, err := PlanEgressReconciliation(ctx, spec, read); err != nil {
		return err
	}
	instance, etag, err := editor.GetInstance("litellm-1")
	if err != nil {
		return fmt.Errorf("inspect LiteLLM instance: %w", err)
	}
	if etag == "" || instance == nil {
		return fmt.Errorf("LiteLLM instance ETag unavailable")
	}
	if instance.Name != "litellm-1" || !strings.EqualFold(instance.Status, "Stopped") {
		return fmt.Errorf("LiteLLM must already be stopped for guarded NIC update")
	}
	if instance.Config["user.aginctus.managed"] != "true" ||
		instance.Config["user.aginctus.resource"] != "compose-instance" ||
		instance.Config["user.aginctus.deployment"] != spec.Project {
		return fmt.Errorf("LiteLLM ownership changed")
	}
	management, egress := "", ""
	for name, dev := range instance.ExpandedDevices {
		if dev["type"] != "nic" {
			continue
		}
		switch dev["network"] {
		case spec.Network:
			if management != "" {
				return fmt.Errorf("multiple management NICs")
			}
			management = name
		case spec.EgressNetwork:
			if egress != "" {
				return fmt.Errorf("multiple egress NICs")
			}
			egress = name
		}
	}
	if management == "" || egress == "" || instance.Devices[management]["network"] != spec.Network ||
		instance.Devices[egress]["network"] != spec.EgressNetwork {
		return fmt.Errorf("LiteLLM NIC topology drift")
	}
	if addr := instance.Devices[egress]["ipv4.address"]; addr != "" && addr != spec.EgressIPv4Address {
		return fmt.Errorf("conflicting LiteLLM egress address")
	}
	if gw := instance.Devices[management]["ipv4.gateway"]; gw != "" && gw != "none" {
		return fmt.Errorf("conflicting management gateway setting")
	}
	put := instance.Writable()
	put.Devices = make(map[string]map[string]string, len(instance.Devices))
	for name, device := range instance.Devices {
		copied := make(map[string]string, len(device))
		for key, value := range device {
			copied[key] = value
		}
		put.Devices[name] = copied
	}
	put.Devices[management]["ipv4.gateway"] = "none"
	put.Devices[egress]["ipv4.address"] = spec.EgressIPv4Address
	// One instance PUT, not sequential device writes. ETag rejects
	// intervening instance mutations. It does NOT reserve the IP globally.
	op, err := editor.UpdateInstance("litellm-1", put, etag)
	if err != nil {
		return fmt.Errorf("guarded LiteLLM NIC update: %w", err)
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("LiteLLM NIC update completion uncertain; inspect before retry: %w", err)
	}
	return nil
}

// ReconcileStoppedEgressNICs is intentionally not wired into gateway apply.
// The explicit CLI invocation requires the operator to stop LiteLLM first.
func ReconcileStoppedEgressNICs(ctx context.Context, spec Spec, read func(context.Context, ...string) ([]byte, error)) error {
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect local Incus: %w", err)
	}
	return reconcileStoppedEgressNICs(ctx, spec, read, server.UseProject(spec.Project))
}
