package managementnetwork

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
)

type Spec struct {
	Name        string
	IPv4Address string
	IPv4NAT     bool
	IPv4Routing bool
	IPv6Address string
}

type Options struct {
	DryRun    bool
	Force     bool
	Operation string
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
	ipv6Address, err := effective.String("incus.management.network.ipv6.address")
	if err != nil {
		return Spec{}, err
	}
	if name == "" {
		return Spec{}, fmt.Errorf("configuration key %q must not be empty", "incus.management.network.name")
	}
	return Spec{
		Name:        name,
		IPv4Address: ipv4Address,
		IPv4NAT:     ipv4NAT,
		IPv4Routing: ipv4Routing,
		IPv6Address: ipv6Address,
	}, nil
}

func (s Spec) Plan(operation string, force bool) (orchestration.Plan, error) {
	if operation == "" {
		operation = "upsert"
	}
	generator, err := orchestration.NewGenerator()
	if err != nil {
		return orchestration.Plan{}, err
	}
	return generator.Evaluate(
		"local ag = import 'aginctus/orchestration.libsonnet'; local network = import 'aginctus/management-network.jsonnet'; ag.orchestration('management-network', [network(std.extVar('config'))])",
		map[string]any{
			"name":        s.Name,
			"ipv4Address": s.IPv4Address,
			"ipv4Nat":     s.IPv4NAT,
			"ipv4Routing": s.IPv4Routing,
			"ipv6Address": s.IPv6Address,
			"operation":   operation,
			"force":       force,
		},
	)
}

func (s Spec) Execute(ctx context.Context, options Options) error {
	plan, err := s.Plan(options.Operation, options.Force)
	if err != nil {
		return err
	}
	engine := orchestration.NewEngine(map[string]orchestration.Driver{
		"apply": applydriver.New(),
	})
	if _, err := engine.Execute(ctx, plan, orchestration.ExecuteOptions{DryRun: options.DryRun}); err != nil {
		return fmt.Errorf("management network orchestration: %w", err)
	}
	return nil
}
