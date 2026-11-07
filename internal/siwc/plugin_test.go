package siwc

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

func TestPluginValidationAndCanceledProviderEnrollment(t *testing.T) {
	plugin := Plugin()
	if plugin.Name != "openai-siwc" || plugin.Login == nil || plugin.DiscoverModels == nil {
		t.Fatal("missing compiled SIWC hooks")
	}
	for _, config := range []gatewaycontrol.Configuration{
		{},
		{StateDir: "/private/state", Models: map[string]gatewaycontrol.Model{"alias": {Source: "fixture-model"}}},
	} {
		if err := plugin.Validate(config); err == nil {
			t.Fatal("accepted invalid SIWC provider configuration")
		}
	}
	f := newFixture(t)
	f.login()
	before := f.state().Accounts["personal"]
	provider := gatewaycontrol.Provider{Name: "personal", Plugin: "openai-siwc", Config: gatewaycontrol.Configuration{StateDir: f.owner.store.Root}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := plugin.Login(ctx, provider, gatewaycontrol.LoginOptions{}); err == nil {
		t.Fatal("canceled login succeeded")
	}
	if !reflect.DeepEqual(before, f.state().Accounts["personal"]) {
		t.Fatal("provider login destroyed prior session")
	}
	provider.Name = "../foreign"
	provider.Config.StateDir = filepath.Join(t.TempDir(), "absent")
	if _, err := providerOwner(provider); err == nil {
		t.Fatal("accepted invalid provider account identity")
	}
}
