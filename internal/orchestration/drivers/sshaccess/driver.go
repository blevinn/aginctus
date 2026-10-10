package sshaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/blevinn/aginctus/internal/orchestration"
)

type Operation string

const (
	Reconcile Operation = "reconcile"
	Revoke    Operation = "revoke"
)

type Config struct {
	Operation      Operation `json:"operation"`
	ClientInstance string    `json:"clientInstance"`
	WorkloadID     string    `json:"workloadID"`
	TargetInstance string    `json:"targetInstance"`
	TargetAccount  string    `json:"targetAccount"`
}

type State struct {
	IdentityPresent      bool
	AuthorizationPresent bool
	AuthorizationMatches bool
	MachinePresent       bool
}

type Runtime interface {
	Inspect(context.Context, Config) (State, error)
	Reconcile(context.Context, Config) error
	Revoke(context.Context, Config) error
}

type Driver struct {
	runtime Runtime
}

var _ orchestration.Driver = (*Driver)(nil)

func New(runtime Runtime) *Driver {
	return &Driver{runtime: runtime}
}

func (d *Driver) SupportsDryRun() bool {
	return true
}

func (d *Driver) Validate(raw json.RawMessage) error {
	_, err := parseConfig(raw)
	return err
}

func (d *Driver) Execute(ctx context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	if d.runtime == nil {
		return fmt.Errorf("ssh-access runtime is not configured")
	}

	if options.DryRun {
		if _, err := d.runtime.Inspect(ctx, cfg); err != nil {
			return fmt.Errorf("inspect ssh access: %w", err)
		}
		return nil
	}

	switch cfg.Operation {
	case Reconcile:
		if err := d.runtime.Reconcile(ctx, cfg); err != nil {
			return fmt.Errorf("reconcile ssh access: %w", err)
		}
	case Revoke:
		if err := d.runtime.Revoke(ctx, cfg); err != nil {
			return fmt.Errorf("revoke ssh access: %w", err)
		}
	default:
		return fmt.Errorf("unsupported ssh-access operation %q", cfg.Operation)
	}
	return nil
}

var workloadIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func parseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode ssh-access driver configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("decode ssh-access driver configuration: unexpected trailing JSON")
		}
		return Config{}, fmt.Errorf("decode ssh-access driver configuration trailing data: %w", err)
	}

	switch cfg.Operation {
	case Reconcile, Revoke:
	default:
		return Config{}, fmt.Errorf("ssh-access operation must be %q or %q, got %q", Reconcile, Revoke, cfg.Operation)
	}
	if cfg.ClientInstance == "" {
		return Config{}, fmt.Errorf("ssh-access clientInstance must not be empty")
	}
	if !workloadIDPattern.MatchString(cfg.WorkloadID) {
		return Config{}, fmt.Errorf("ssh-access workloadID must be a safe identifier")
	}
	if cfg.TargetInstance == "" {
		return Config{}, fmt.Errorf("ssh-access targetInstance must not be empty")
	}
	if cfg.TargetAccount == "" {
		return Config{}, fmt.Errorf("ssh-access targetAccount must not be empty")
	}
	if cfg.TargetAccount == "root" {
		return Config{}, fmt.Errorf("ssh-access targetAccount must not be root")
	}
	return cfg, nil
}
