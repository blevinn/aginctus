package herdrclient

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
)

type Spec struct {
	Name              string
	ImageAlias        string
	StoragePool       string
	ManagementNetwork string
	Network           managementnetwork.Spec
}

type Options struct {
	DryRun    bool
	Force     bool
	Operation string
}

func FromConfig(effective *config.Config) (Spec, error) {
	name, err := effective.String("infrastructure.herdr.name")
	if err != nil {
		return Spec{}, err
	}
	imageAlias, err := effective.String("infrastructure.herdr.image.alias")
	if err != nil {
		return Spec{}, err
	}
	storagePool, err := effective.String("infrastructure.herdr.storage.pool")
	if err != nil {
		return Spec{}, err
	}
	network, err := managementnetwork.FromConfig(effective)
	if err != nil {
		return Spec{}, err
	}
	if name == "" || imageAlias == "" || storagePool == "" || network.Name == "" {
		return Spec{}, fmt.Errorf("Herdr client name, image alias, storage pool, and management network must not be empty")
	}
	return Spec{
		Name:              name,
		ImageAlias:        imageAlias,
		StoragePool:       storagePool,
		ManagementNetwork: network.Name,
		Network:           network,
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
		`local ag = import 'aginctus/orchestration.libsonnet';
local network = import 'aginctus/management-network.jsonnet';
local herdr = import 'aginctus/herdr-client.jsonnet';
local cfg = std.extVar('config');
local steps = if cfg.operation == 'delete'
  then [herdr(cfg.client)]
  else [network(cfg.network), herdr(cfg.client)];
ag.orchestration('herdr-client', steps)`,
		map[string]any{
			"operation": operation,
			"network": map[string]any{
				"name":        s.Network.Name,
				"ipv4Address": s.Network.IPv4Address,
				"ipv4Nat":     s.Network.IPv4NAT,
				"ipv4Routing": s.Network.IPv4Routing,
				"ipv6Address": s.Network.IPv6Address,
				"operation":   "upsert",
				"force":       force,
			},
			"client": map[string]any{
				"name":              s.Name,
				"imageAlias":        s.ImageAlias,
				"storagePool":       s.StoragePool,
				"managementNetwork": s.ManagementNetwork,
				"operation":         operation,
				"force":             force,
			},
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
		return fmt.Errorf("Herdr client orchestration: %w", err)
	}
	return nil
}
