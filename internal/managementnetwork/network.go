package managementnetwork

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
	nativedriver "github.com/blevinn/aginctus/internal/orchestration/drivers/native"
)

// Backend selects the reconciliation implementation for the management bridge.
// Exactly one backend may reconcile the bridge at a time.
type Backend string

const (
	// BackendLegacy reconciles through the incus-apply driver.
	BackendLegacy Backend = "legacy"
	// BackendNative reconciles through the Aginctus-owned Incus SDK driver.
	BackendNative Backend = "native"
)

// backendKey is the configuration key that selects the management bridge
// reconciliation backend.
const backendKey = "incus.management.network.backend"

type Spec struct {
	Name            string
	IPv4Address     string
	IPv4NAT         bool
	IPv4Routing     bool
	IPv4DHCPGateway string
	IPv6Address     string
	Backend         Backend
}

type Options struct {
	DryRun    bool
	Force     bool
	Operation string
}

// BackendFromConfig resolves the reconciliation backend from the effective
// configuration. An unknown backend is refused instead of falling back.
func BackendFromConfig(effective *config.Config) (Backend, error) {
	selected, err := effective.String(backendKey)
	if err != nil {
		return "", err
	}
	switch Backend(selected) {
	case BackendLegacy, BackendNative:
		return Backend(selected), nil
	default:
		return "", fmt.Errorf("configuration key %q must be %q or %q", backendKey, BackendLegacy, BackendNative)
	}
}

func FromConfig(effective *config.Config) (Spec, error) {
	name, err := effective.String("incus.management.network.name")
	if err != nil {
		return Spec{}, err
	}
	ipv4Address, err := effective.String("incus.management.network.ipv4.address")
	if err != nil {
		return Spec{}, err
	}
	ipv4NAT, err := effective.Bool("incus.management.network.ipv4.nat")
	if err != nil {
		return Spec{}, err
	}
	ipv4Routing, err := effective.Bool("incus.management.network.ipv4.routing")
	if err != nil {
		return Spec{}, err
	}
	ipv4DHCPGateway, err := effective.String("incus.management.network.ipv4.dhcp.gateway")
	if err != nil {
		return Spec{}, err
	}
	ipv6Address, err := effective.String("incus.management.network.ipv6.address")
	if err != nil {
		return Spec{}, err
	}
	backend, err := BackendFromConfig(effective)
	if err != nil {
		return Spec{}, err
	}
	if name == "" {
		return Spec{}, fmt.Errorf("configuration key %q must not be empty", "incus.management.network.name")
	}
	return Spec{
		Name:            name,
		IPv4Address:     ipv4Address,
		IPv4NAT:         ipv4NAT,
		IPv4Routing:     ipv4Routing,
		IPv4DHCPGateway: ipv4DHCPGateway,
		IPv6Address:     ipv6Address,
		Backend:         backend,
	}, nil
}

// ExtVars renders the shared management-network extVars. Every plan that
// reconciles the bridge uses them so the selected backend is identical.
// An unset backend selects the legacy driver; an unknown one is refused.
func (s Spec) ExtVars(operation string, force bool) (map[string]any, error) {
	backend := s.Backend
	switch backend {
	case "":
		backend = BackendLegacy
	case BackendLegacy, BackendNative:
	default:
		return nil, fmt.Errorf("unsupported management network backend %q", backend)
	}
	return map[string]any{
		"name":            s.Name,
		"ipv4Address":     s.IPv4Address,
		"ipv4Nat":         s.IPv4NAT,
		"ipv4Routing":     s.IPv4Routing,
		"ipv4DHCPGateway": s.IPv4DHCPGateway,
		"ipv6Address":     s.IPv6Address,
		"operation":       operation,
		"force":           force,
		"backend":         string(backend),
	}, nil
}

func (s Spec) Plan(operation string, force bool) (orchestration.Plan, error) {
	if operation == "" {
		operation = "upsert"
	}
	extVars, err := s.ExtVars(operation, force)
	if err != nil {
		return orchestration.Plan{}, err
	}
	generator, err := orchestration.NewGenerator()
	if err != nil {
		return orchestration.Plan{}, err
	}
	return generator.Evaluate(
		"local ag = import 'aginctus/orchestration.libsonnet'; local network = import 'aginctus/management-network.jsonnet'; ag.orchestration('management-network', [network(std.extVar('config'))])",
		extVars,
	)
}

func (s Spec) Execute(ctx context.Context, options Options) error {
	plan, err := s.Plan(options.Operation, options.Force)
	if err != nil {
		return err
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{
		"apply":  applydriver.New(),
		"native": nativedriver.NewDriver(),
	})
	if _, err := engine.Execute(ctx, plan, orchestration.ExecuteOptions{DryRun: options.DryRun}); err != nil {
		return fmt.Errorf("management network orchestration: %w", err)
	}
	return nil
}
