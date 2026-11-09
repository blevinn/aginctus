package cli

import (
	"fmt"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	siwc "github.com/blevinn/aginctus/internal/plugins/openai-siwc"
)

func checkGatewayProviderMutation(effective *config.Config, name string) error {
	value, _ := effective.Get("gateway.providers")
	providers, _ := value.(map[string]any)
	if _, exists := providers[name]; exists {
		return fmt.Errorf("provider is configuration-owned; edit its declaration")
	}
	return nil
}

func configuredGatewayStore(effective *config.Config) (*gatewaycontrol.Store, error) {
	store, err := gatewaycontrol.DefaultStore(gatewayPlugins())
	if err != nil {
		return nil, err
	}
	providers, _ := effective.Get("gateway.providers")
	catalogs, _ := effective.Get("gateway.catalogs")
	mcps, _ := effective.Get("mcp.servers")
	if err := store.Configure(providers, catalogs, mcps); err != nil {
		return nil, err
	}
	configureSIWCOperations(store, effective)
	return store, nil
}

func configuredGatewaySpec(effective *config.Config) (gateway.Spec, error) {
	spec, err := gateway.FromConfig(effective)
	if err != nil {
		return spec, err
	}
	store, err := configuredGatewayStore(effective)
	if err != nil {
		return spec, err
	}
	plan, err := siwc.Configure(store)
	if err != nil || len(plan.Providers) == 0 {
		return spec, err
	}
	public, err := plan.RuntimeConfig()
	if err != nil {
		return spec, err
	}
	err = spec.ConfigureSIWC(public)
	return spec, err
}
