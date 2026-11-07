package cli

import (
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

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
	return store, nil
}
