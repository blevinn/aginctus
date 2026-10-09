package workload

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
)

type Spec struct {
	ID                string
	Name              string
	Runtime           string
	Isolation         string
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

func FromConfig(effective *config.Config, id string) (Spec, error) {
	if id == "" {
		return Spec{}, fmt.Errorf("workload id must not be empty")
	}
	prefix := "workloads." + id
	name, err := effective.String(prefix + ".instance.name")
	if err != nil {
		return Spec{}, err
	}
	runtimeType, err := effective.String(prefix + ".runtime.type")
	if err != nil {
		return Spec{}, err
	}
	isolation, err := effective.String(prefix + ".isolation")
	if err != nil {
		return Spec{}, err
	}
	imageAlias, err := effective.String(prefix + ".image.alias")
	if err != nil {
		return Spec{}, err
	}
	storagePool, err := effective.String(prefix + ".storage.pool")
	if err != nil {
		return Spec{}, err
	}
	network, err := managementnetwork.FromConfig(effective)
	if err != nil {
		return Spec{}, err
	}
	spec := Spec{
		ID:                id,
		Name:              name,
		Runtime:           runtimeType,
		Isolation:         isolation,
		ImageAlias:        imageAlias,
		StoragePool:       storagePool,
		ManagementNetwork: network.Name,
		Network:           network,
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func (s Spec) Validate() error {
	switch {
	case s.ID == "":
		return fmt.Errorf("workload id must not be empty")
	case s.Name == "":
		return fmt.Errorf("workload instance name must not be empty")
	case s.Runtime == "":
		return fmt.Errorf("workload runtime must not be empty")
	case s.ImageAlias == "":
		return fmt.Errorf("workload image alias must not be empty")
	case s.StoragePool == "":
		return fmt.Errorf("workload storage pool must not be empty")
	case s.ManagementNetwork == "":
		return fmt.Errorf("workload management network must not be empty")
	case s.Isolation != "container" && s.Isolation != "vm":
		return fmt.Errorf("workload isolation must be container or vm, got %q", s.Isolation)
	default:
		return nil
	}
}

func (s Spec) Plan(operation string, force bool) (orchestration.Plan, error) {
	if err := s.Validate(); err != nil {
		return orchestration.Plan{}, err
	}
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
local workload = import 'aginctus/workload.jsonnet';
local cfg = std.extVar('config');
local steps = if cfg.operation == 'delete'
  then [workload(cfg.workload)]
  else [network(cfg.network), workload(cfg.workload)];
ag.orchestration('workload-' + cfg.workload.id, steps)`,
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
			"workload": map[string]any{
				"id":                s.ID,
				"name":              s.Name,
				"runtime":           s.Runtime,
				"isolation":         s.Isolation,
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
		return fmt.Errorf("workload %q orchestration: %w", s.ID, err)
	}
	return nil
}
