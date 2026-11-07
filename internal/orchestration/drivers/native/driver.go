// Package native exposes Aginctus-owned Incus reconciliation as an
// orchestration driver. The driver carries the typed intent of one
// resource; the reconciler observes, plans, guards and verifies it.
package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/orchestration"
	reconciler "github.com/blevinn/aginctus/internal/orchestration/native"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// DriverName is the orchestration driver identifier of the native backend.
const DriverName = "native"

// Config is the typed configuration of a native reconciliation step. It
// describes the desired state of exactly one resource.
type Config struct {
	Operation string            `json:"operation"`
	Resource  string            `json:"resource"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Config    map[string]string `json:"config"`
	Adopt     bool              `json:"adopt"`
}

// Driver reconciles native Incus resources. The reconciler functions are
// injected so hermetic tests never reach a daemon.
type Driver struct {
	planEnsure func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkPlan, error)
	planRemove func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkRemovalPlan, error)
	ensure     func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error)
	remove     func(context.Context, reconciler.NetworkIntent) (reconciler.NetworkResult, error)
}

var _ orchestration.Driver = (*Driver)(nil)

func NewDriver() *Driver {
	return &Driver{
		planEnsure: reconciler.PlanLocalNetwork,
		planRemove: reconciler.PlanLocalNetworkRemoval,
		ensure:     reconciler.EnsureLocalNetwork,
		remove:     reconciler.RemoveLocalNetwork,
	}
}

// intent converts the step configuration into the typed reconciler intent.
// Only resources with an explicit Incus scope may be reconciled.
func (cfg Config) intent() (reconciler.NetworkIntent, error) {
	scope, err := incusscope.NewHostSharedScope(cfg.Resource)
	if err != nil {
		return reconciler.NetworkIntent{}, err
	}
	intent := reconciler.NetworkIntent{
		Scope:  scope,
		Name:   cfg.Name,
		Type:   cfg.Type,
		Config: cfg.Config,
		Adopt:  reconciler.NetworkAdoption{Authorized: cfg.Adopt},
	}
	if err := intent.Validate(); err != nil {
		return reconciler.NetworkIntent{}, err
	}
	return intent, nil
}

func parseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode native driver configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("native driver configuration must contain one object")
	}
	switch cfg.Operation {
	case "upsert", "delete":
	default:
		return Config{}, fmt.Errorf("unsupported native operation %q", cfg.Operation)
	}
	if cfg.Name == "" {
		return Config{}, fmt.Errorf("native driver configuration requires a resource name")
	}
	return cfg, nil
}

// Validate checks the step configuration without contacting Incus.
func (d *Driver) Validate(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	_, err = cfg.intent()
	return err
}

func (d *Driver) SupportsDryRun() bool { return true }

// Execute reconciles the resource. A dry run only plans and never mutates.
func (d *Driver) Execute(ctx context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	intent, err := cfg.intent()
	if err != nil {
		return err
	}
	switch cfg.Operation {
	case "upsert":
		if options.DryRun {
			if _, err := d.planEnsure(ctx, intent); err != nil {
				return fmt.Errorf("plan native resource: %w", err)
			}
			return ctx.Err()
		}
		if _, err := d.ensure(ctx, intent); err != nil {
			return fmt.Errorf("reconcile native resource: %w", err)
		}
		return ctx.Err()
	case "delete":
		if options.DryRun {
			if _, err := d.planRemove(ctx, intent); err != nil {
				return fmt.Errorf("plan native resource removal: %w", err)
			}
			return ctx.Err()
		}
		if _, err := d.remove(ctx, intent); err != nil {
			return fmt.Errorf("remove native resource: %w", err)
		}
		return ctx.Err()
	}
	return fmt.Errorf("unsupported native operation %q", cfg.Operation)
}
