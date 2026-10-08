package apply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	incusapply "github.com/abiosoft/incus-apply/apply"
	"sigs.k8s.io/yaml"

	"github.com/blevinn/aginctus/internal/orchestration"
)

type Config struct {
	Operation             incusapply.Operation `json:"operation,omitempty"`
	Project               string               `json:"project,omitempty"`
	RequireExistingConfig   map[string]string    `json:"requireExistingConfig,omitempty"`
	RejectUnsupportedChanges bool                `json:"rejectUnsupportedChanges,omitempty"`
	EnsureRunning         bool                 `json:"ensureRunning,omitempty"`
	Documents             []json.RawMessage    `json:"documents"`
}

type Driver struct {
	newClient func(incusapply.Options) applyClient
}

type applyClient interface {
	Plan(io.Reader) (incusapply.Preview, error)
	Execute(io.Reader) (incusapply.Result, error)
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
	_, err := parseConfig(raw)
	return err
}

func (d *Driver) Execute(_ context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	cfg, err := parseConfig(raw)
	if err != nil {
		return err
	}
	stream, err := renderDocuments(cfg.Documents)
	if err != nil {
		return err
	}

	operation := cfg.Operation
	if operation == "" {
		operation = incusapply.Upsert
	}
	client := d.newClient(incusapply.Options{
		Operation:             operation,
		Project:               cfg.Project,
		FailFast:              true,
		RequireExistingConfig:    cfg.RequireExistingConfig,
		RejectUnsupportedChanges: cfg.RejectUnsupportedChanges,
		EnsureRunning:         cfg.EnsureRunning,
	})

	if options.DryRun {
		if _, err := client.Plan(bytes.NewReader(stream)); err != nil {
			return fmt.Errorf("plan incus-apply resources: %w", err)
		}
		return nil
	}
	if _, err := client.Execute(bytes.NewReader(stream)); err != nil {
		return fmt.Errorf("apply incus resources: %w", err)
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
