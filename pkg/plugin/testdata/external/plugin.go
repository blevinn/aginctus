// Package externalplugin demonstrates discovery and pure configuration checking
// without an Aginctus internal import, container, credential or live service.
package externalplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/blevinn/aginctus/pkg/orchestration"
	"github.com/blevinn/aginctus/pkg/plugin"
)

type Example struct{}

var _ plugin.Plugin = Example{}
var _ plugin.TargetContributor = Example{}

func (Example) Descriptor() plugin.Descriptor {
	return plugin.Descriptor{
		APIVersion: plugin.APIVersion,
		Name:       "external-example",
		Version:    "fixture-1",
		Capabilities: []plugin.Capability{
			{Name: "models", Kind: plugin.ModelProvider},
			{Name: "tools", Kind: plugin.MCP},
			{Name: "service", Kind: plugin.Gateway},
		},
	}
}

type configuration struct {
	Model         string  `json:"model"`
	Image         *string `json:"image,omitempty"`
	DatabaseImage *string `json:"database_image,omitempty"`
}

func parseConfiguration(raw json.RawMessage) (configuration, error) {
	var configuration configuration
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&configuration); err != nil {
		return configuration, fmt.Errorf("expected documented model/image configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return configuration, fmt.Errorf("expected one configuration object")
	}
	return configuration, nil
}

func (Example) ValidateConfiguration(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configuration, err := parseConfiguration(raw)
	if err != nil {
		return err
	}
	if configuration.Model != "fixture-model" {
		return fmt.Errorf("unsupported fixture model")
	}
	for _, pointer := range []*string{configuration.Image, configuration.DatabaseImage} {
		if pointer != nil {
			if err := (orchestration.ImageReference{Format: orchestration.IncusImage, Pointer: *pointer}).Validate(); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func (Example) Targets(ctx context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	configuration, err := parseConfiguration(raw)
	if err != nil {
		return nil, err
	}
	image, database := "images:fixture-helper", "images:fixture-store"
	if configuration.Image != nil {
		image = *configuration.Image
	}
	if configuration.DatabaseImage != nil {
		database = *configuration.DatabaseImage
	}
	return []orchestration.Target{
		{ID: "catalog", Capability: "models", Preconditions: []orchestration.Check{{
			ID: "configuration", Claim: "model configuration satisfies the fixture schema", Scope: "supplied candidate only",
			Required: true, Effect: orchestration.Offline, Timeout: time.Second,
		}}, Postconditions: []orchestration.Check{{
			ID: "inference", Claim: "selected remote model answers a request", Scope: "configured provider route",
			Required: true, Effect: orchestration.ActiveProbe,
			Access: []orchestration.Access{orchestration.NetworkAccess, orchestration.CredentialAccess, orchestration.ModelAccess},
			OptIns: []string{"live-inference"}, Timeout: 10 * time.Second,
		}}},
		{ID: "helper", Capability: "service", DependsOn: []string{"catalog"}, Containers: []orchestration.Container{
			{ID: "helper", Image: orchestration.ImageReference{Format: orchestration.IncusImage, Pointer: image},
				Contract: orchestration.GuestContract{ID: "helper", Version: "v1", Requirements: []string{"Linux guest with fixture-helper v1 at /usr/bin/fixture-helper"}}},
			{ID: "store", Image: orchestration.ImageReference{Format: orchestration.IncusImage, Pointer: database},
				Contract: orchestration.GuestContract{ID: "store", Version: "v1", Requirements: []string{"Linux guest with fixture-store v1 service"}}},
		}, Postconditions: []orchestration.Check{{
			ID: "guest-ready", Claim: "helper protocol v1 is ready", Scope: "selected helper guest",
			Required: true, Effect: orchestration.ReadOnly, Access: []orchestration.Access{orchestration.GuestAccess},
			Timeout: 10 * time.Second, RetryInterval: time.Second,
		}}},
	}, nil
}
