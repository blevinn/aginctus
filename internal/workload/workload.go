package workload

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	incusscope "github.com/blevinn/aginctus/internal/incus"
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
				"force":       false, // Workload force must not adopt the shared management network.
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

// PlanScopedExisting generates a read-only review artifact for the owned
// project's *existing* workload. It intentionally removes the host-managed
// bridge step; a project-local reconciliation must never create/change it.
//
// This does NOT grant mutation authority, prove the Incus driver's connection
// is local, or prevent incus-apply from creating a missing instance. A future
// write path must enforce those guarantees independently at mutation time.
func (s Spec) PlanScopedExisting(scope incusscope.ProjectScope) (orchestration.Plan, error) {
	if _, err := incusscope.NewProjectScope(scope.Name); err != nil {
		return orchestration.Plan{}, err
	}
	plan, err := s.Plan("upsert", false)
	if err != nil {
		return orchestration.Plan{}, err
	}
	if len(plan.Steps) != 2 ||
		plan.Steps[0].ID != "management-network" ||
		plan.Steps[1].ID != "workload-"+s.ID || plan.Steps[1].Driver != "apply" {
		return orchestration.Plan{}, fmt.Errorf("unexpected workload orchestration structure")
	}
	step := plan.Steps[1]
	var cfg applydriver.Config
	if err := json.Unmarshal(step.Configuration, &cfg); err != nil {
		return orchestration.Plan{}, fmt.Errorf("invalid workload apply configuration")
	}
	if cfg.Operation != "upsert" || cfg.Project != "" || !cfg.EnsureRunning ||
		!cfg.RejectUnsupportedChanges || len(cfg.Documents) != 1 ||
		cfg.RequireExistingConfig["user.aginctus.managed"] != "true" ||
		cfg.RequireExistingConfig["user.aginctus.resource"] != "workload" ||
		cfg.RequireExistingConfig["user.aginctus.workload"] != s.ID {
		return orchestration.Plan{}, fmt.Errorf("unsafe workload reconciliation template")
	}
	var target struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(cfg.Documents[0], &target); err != nil ||
		target.Kind != "instance" || target.Name != s.Name {
		return orchestration.Plan{}, fmt.Errorf("unexpected scoped workload target")
	}
	cfg.Project = scope.Name
	cfg.RequireExistingConfig["user.aginctus.role"] = "agent-workload"
	step.Configuration, err = json.Marshal(cfg)
	if err != nil {
		return orchestration.Plan{}, fmt.Errorf("cannot encode scoped workload plan")
	}
	plan.Steps = []orchestration.Step{step}
	if err := plan.Validate(); err != nil {
		return orchestration.Plan{}, err
	}
	return plan, nil
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
