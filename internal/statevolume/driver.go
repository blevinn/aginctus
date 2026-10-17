package statevolume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/orchestration"
)

// ProvisionConfig is a declarative, ensure-only prerequisite for a managed
// persistent system volume. Volume ownership and foreign-instance checks
// remain in EnsureSystem; the driver never deletes persistent state.
type ProvisionConfig struct {
	Operation string `json:"operation"`
	Pool      string `json:"pool"`
	Instance  string `json:"instance"`
	Class     string `json:"class"`
}

type Driver struct {
	ensureSystem func(context.Context, string, string) error
}

var _ orchestration.Driver = (*Driver)(nil)

func NewDriver() *Driver {
	return &Driver{ensureSystem: EnsureSystem}
}

func parseProvisionConfig(raw json.RawMessage) (ProvisionConfig, error) {
	var cfg ProvisionConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return ProvisionConfig{}, fmt.Errorf("decode state-volume configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ProvisionConfig{}, fmt.Errorf("state-volume configuration must contain one object")
	}
	if cfg.Operation != "ensure" || cfg.Class != "system" {
		return ProvisionConfig{}, fmt.Errorf("state-volume driver supports only ensure of system state")
	}
	if err := (Volume{Pool: cfg.Pool, Instance: cfg.Instance, Class: cfg.Class}).Validate(); err != nil {
		return ProvisionConfig{}, err
	}
	return cfg, nil
}

func (d *Driver) Validate(raw json.RawMessage) error {
	_, err := parseProvisionConfig(raw)
	return err
}

func (d *Driver) SupportsDryRun() bool { return true }

func (d *Driver) Execute(ctx context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := parseProvisionConfig(raw)
	if err != nil {
		return err
	}
	if options.DryRun {
		return nil
	}
	if err := d.ensureSystem(ctx, cfg.Pool, cfg.Instance); err != nil {
		return fmt.Errorf("ensure Herdr system state volume: %w", err)
	}
	return ctx.Err()
}
