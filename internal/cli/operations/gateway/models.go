package gatewayops

import (
	"fmt"

	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

// RenderProviderModel fails closed on ambiguous providers and requires one
// native ChatGPT credential owner for LiteLLM model rendering.
func RenderProviderModel(store *gatewaycontrol.Store, provider, name string) ([]byte, error) {
	providers, err := store.Providers()
	if err != nil {
		return nil, err
	}
	var owner gatewaycontrol.Provider
	var model gatewaycontrol.Model
	matches, nativeChatGPTOwners := 0, 0
	for key, p := range providers {
		if p.Plugin == "chatgpt" {
			nativeChatGPTOwners++
		}
		if provider != "" && provider != key {
			continue
		}
		if m, ok := p.Config.Models[name]; ok {
			owner, model = p, m
			model.Name = name
			matches++
		}
	}
	if matches != 1 {
		return nil, fmt.Errorf("model not registered or ambiguous; select its provider explicitly")
	}
	// The native LiteLLM ChatGPT path has one shared token directory.
	if owner.Plugin == "chatgpt" && nativeChatGPTOwners != 1 {
		return nil, fmt.Errorf("native ChatGPT rendering requires exactly one locally registered ChatGPT identity")
	}
	plugin := store.Plugins[owner.Plugin]
	if plugin.RenderModel == nil {
		return nil, fmt.Errorf("this plugin does not implement model rendering")
	}
	return plugin.RenderModel(owner, model)
}
