package apply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	incusapply "github.com/abiosoft/incus-apply/apply"
	"sigs.k8s.io/yaml"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration"
)

type Config struct {
	Operation                incusapply.Operation `json:"operation,omitempty"`
	Project                  string               `json:"project,omitempty"`
	RequireExistingConfig    map[string]string    `json:"requireExistingConfig,omitempty"`
	RejectUnsupportedChanges bool                 `json:"rejectUnsupportedChanges,omitempty"`
	EnsureRunning            bool                 `json:"ensureRunning,omitempty"`
	Documents                []json.RawMessage    `json:"documents"`
}

type Driver struct {
	newClient func(incusapply.Options) applyClient
}

type applyClient interface {
	PlanContext(context.Context, io.Reader) (incusapply.Preview, error)
	ExecuteContext(context.Context, io.Reader) (incusapply.Result, error)
}

var _ orchestration.Driver = (*Driver)(nil)

func New() *Driver {
	return &Driver{
		newClient: func(options incusapply.Options) applyClient {
			return incusapply.NewNative(options)
		},
	}
}

func (d *Driver) SupportsDryRun() bool {
	return true
}

func (d *Driver) Validate(raw json.RawMessage) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	stream, err := renderDocuments(cfg.Documents)
	if err != nil {
		return err
	}
	client := d.newClient(clientOptions(cfg))
	if _, err := client.PlanContext(context.Background(), bytes.NewReader(stream)); err != nil {
		return fmt.Errorf("preflight incus-apply resources: %w", err)
	}
	return nil
}

// PlanScopedInstance is a read-only native Incus diff for exactly one
// explicitly project-bound, ownership-guarded existing workload instance.
// The pinned NewNative backend connects using ConnectIncusUnix and applies
// the project through UseProject. No mutation path is exposed by this method.
// A returned preview can still become stale or report a proposed creation;
// callers must independently reject unsafe actions and must never treat this
// result as permission to write.
func (d *Driver) PlanScopedInstance(ctx context.Context, raw json.RawMessage) (incusapply.Preview, error) {
	if err := ctx.Err(); err != nil {
		return incusapply.Preview{}, err
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		return incusapply.Preview{}, err
	}
	if cfg.Operation != incusapply.Upsert || !cfg.EnsureRunning ||
		!cfg.RejectUnsupportedChanges || len(cfg.Documents) != 1 ||
		cfg.RequireExistingConfig["user.aginctus.managed"] != "true" ||
		cfg.RequireExistingConfig["user.aginctus.resource"] != "workload" ||
		cfg.RequireExistingConfig["user.aginctus.role"] != "agent-workload" ||
		cfg.RequireExistingConfig["user.aginctus.workload"] == "" {
		return incusapply.Preview{}, fmt.Errorf("scoped instance requires explicit upsert and complete ownership guards")
	}
	if _, err := incusscope.NewProjectScope(cfg.Project); err != nil {
		return incusapply.Preview{}, fmt.Errorf("scoped instance project invalid")
	}
	var target struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Remote  string `json:"remote"`
		Project string `json:"project"`
	}
	if err := json.Unmarshal(cfg.Documents[0], &target); err != nil ||
		target.Kind != "instance" || target.Name == "" ||
		strings.ContainsAny(target.Name, ":/") || target.Remote != "" || target.Project != "" {
		return incusapply.Preview{}, fmt.Errorf("scoped instance target invalid or ambiguous")
	}
	stream, err := renderDocuments(cfg.Documents)
	if err != nil {
		return incusapply.Preview{}, err
	}
	// clientOptions passes Project, never Remote, to the pinned native
	// backend. This is read-only even when the diff reports "create".
	return d.newClient(clientOptions(cfg)).PlanContext(ctx, bytes.NewReader(stream))
}

func clientOptions(cfg Config) incusapply.Options {
	operation := cfg.Operation
	if operation == "" {
		operation = incusapply.Upsert
	}
	return incusapply.Options{
		Operation:                operation,
		Project:                  cfg.Project,
		FailFast:                 true,
		RequireExistingConfig:    cfg.RequireExistingConfig,
		RejectUnsupportedChanges: cfg.RejectUnsupportedChanges,
		EnsureRunning:            cfg.EnsureRunning,
	}
}

func (d *Driver) Execute(ctx context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply step canceled before preparation: %w", err)
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	stream, err := renderDocuments(cfg.Documents)
	if err != nil {
		return err
	}

	client := d.newClient(clientOptions(cfg))

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply step canceled before execution: %w", err)
	}
	if options.DryRun {
		if _, err := client.PlanContext(ctx, bytes.NewReader(stream)); err != nil {
			return fmt.Errorf("plan incus-apply resources: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("apply planning canceled: %w", err)
		}
		return nil
	}
	// The dependency observes cancellation between resources and during
	// supported native operation waits. Cancellation does not roll back
	// an Incus mutation that already reached the server.
	_, executeErr := client.ExecuteContext(ctx, bytes.NewReader(stream))
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply execution canceled (native operation may have completed): %w", err)
	}
	if executeErr != nil {
		return fmt.Errorf("apply incus resources: %w", executeErr)
	}
	return nil
}

func parseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode apply driver configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("decode apply driver configuration: unexpected trailing JSON")
		}
		return Config{}, fmt.Errorf("decode apply driver configuration trailing data: %w", err)
	}
	switch cfg.Operation {
	case "", incusapply.Upsert, incusapply.Delete, incusapply.Reset:
	default:
		return Config{}, fmt.Errorf("unsupported apply operation %q", cfg.Operation)
	}
	if len(cfg.Documents) == 0 {
		return Config{}, fmt.Errorf("apply driver requires at least one document")
	}
	for i, document := range cfg.Documents {
		trimmed := bytes.TrimSpace(document)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			return Config{}, fmt.Errorf("apply document %d must not be empty", i)
		}
		var value map[string]any
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return Config{}, fmt.Errorf("decode apply document %d: %w", i, err)
		}
		if len(value) == 0 {
			return Config{}, fmt.Errorf("apply document %d must be an object", i)
		}
	}
	return cfg, nil
}

func renderDocuments(documents []json.RawMessage) ([]byte, error) {
	var out bytes.Buffer
	for i, document := range documents {
		var value any
		if err := json.Unmarshal(document, &value); err != nil {
			return nil, fmt.Errorf("decode apply document %d: %w", i, err)
		}
		rendered, err := yaml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode apply document %d: %w", i, err)
		}
		if i > 0 {
			out.WriteString("---\n")
		}
		out.Write(rendered)
	}
	return out.Bytes(), nil
}
