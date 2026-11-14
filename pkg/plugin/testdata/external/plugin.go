// Package externalplugin demonstrates discovery and pure configuration checking
// without an Aginctus internal import, container, credential or live service.
package externalplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/pkg/plugin"
)

type Example struct{}

var _ plugin.Plugin = Example{}

func (Example) Descriptor() plugin.Descriptor {
	return plugin.Descriptor{
		APIVersion: plugin.APIVersion,
		Name:       "external-example",
		Version:    "fixture-1",
		Capabilities: []plugin.Capability{
			{Name: "models", Kind: plugin.ModelProvider},
			{Name: "tools", Kind: plugin.MCP},
		},
	}
}

func (Example) ValidateConfiguration(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var configuration struct {
		Model string `json:"model"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&configuration); err != nil {
		return fmt.Errorf("expected documented model configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one configuration object")
	}
	if configuration.Model != "fixture-model" {
		return fmt.Errorf("unsupported fixture model")
	}
	return ctx.Err()
}
