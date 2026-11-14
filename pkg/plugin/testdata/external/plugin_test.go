package externalplugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	externalplugin "example.com/aginctus-external-plugin"
	"github.com/blevinn/aginctus/pkg/plugin"
)

func TestExternalConsumer(t *testing.T) {
	var implementation plugin.Plugin = externalplugin.Example{}
	descriptor := implementation.Descriptor()
	if descriptor.Name != "external-example" || len(descriptor.Capabilities) != 2 {
		t.Fatalf("unexpected discovered capabilities: %+v", descriptor)
	}
	if err := plugin.Validate(context.Background(), implementation, json.RawMessage(`{"model":"fixture-model"}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `{"model":"unknown"}`, `{"model":"fixture-model","unexpected":true}`} {
		if err := plugin.Validate(context.Background(), implementation, json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid plugin configuration accepted: %s", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := plugin.Validate(ctx, implementation, json.RawMessage(`{"model":"fixture-model"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}
