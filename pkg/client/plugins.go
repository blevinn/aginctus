package client

import (
	"context"

	"github.com/blevinn/aginctus/pkg/api"
)

// ListPluginsRequest intentionally has no environment selector: compiled
// inventory is process-wide and not proof of a selected runtime instance.
type ListPluginsRequest struct{}

// PluginsService exposes compiled plugin discovery without executing plugin
// callbacks or interpreting runtime configuration.
type PluginsService struct {
	inventory PluginInventory
}

// List returns a detached public projection in registry-defined dependency
// order. Empty inventories encode as [] rather than null.
func (s *PluginsService) List(ctx context.Context, _ ListPluginsRequest) (api.PluginList, error) {
	result := api.PluginList{TypeMeta: api.NewTypeMeta("PluginList"), Items: []api.Plugin{}}
	if err := ctx.Err(); err != nil {
		return api.PluginList{}, err
	}
	if s == nil || s.inventory == nil {
		return result, nil
	}
	for _, descriptor := range s.inventory.Inventory() {
		if err := ctx.Err(); err != nil {
			return api.PluginList{}, err
		}
		item := api.Plugin{
			Name: descriptor.Name, Version: descriptor.Version,
			APIVersion: descriptor.APIVersion,
			Capabilities: make([]api.PluginCapability, 0, len(descriptor.Capabilities)),
			Dependencies: append([]string{}, descriptor.Dependencies...),
		}
		for _, cap := range descriptor.Capabilities {
			item.Capabilities = append(item.Capabilities, api.PluginCapability{
				Name: cap.Name, Kind: string(cap.Kind),
			})
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
