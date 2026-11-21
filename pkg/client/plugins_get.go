package client

import (
	"context"

	"github.com/blevinn/aginctus/pkg/api"
)

// GetPluginRequest addresses a compiled implementation by its exact identity,
// not a configured runtime instance or environment binding.
type GetPluginRequest struct {
	Name string
}

// Get reads a single compiled descriptor. It does not activate plugins,
// resolve credentials or infer a running workload.
func (s *PluginsService) Get(ctx context.Context, request GetPluginRequest) (api.PluginDetail, error) {
	if err := ctx.Err(); err != nil {
		return api.PluginDetail{}, err
	}
	if request.Name == "" {
		return api.PluginDetail{}, api.NewError(api.CodeInvalidArgument, "compiled plugin name is required")
	}
	list, err := s.List(ctx, ListPluginsRequest{})
	if err != nil {
		return api.PluginDetail{}, err
	}
	for _, item := range list.Items {
		if item.Name == request.Name {
			return api.PluginDetail{TypeMeta: api.NewTypeMeta("Plugin"), Plugin: item}, nil
		}
	}
	return api.PluginDetail{}, api.NewError(api.CodeNotFound, "compiled plugin not found")
}
