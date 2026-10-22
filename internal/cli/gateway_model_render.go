package cli

import (
	"fmt"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

// renderRegisteredModel resolves a stored nonsecret intent to a read-only
// LiteLLM Responses model definition. It never touches gateway credentials.
func renderRegisteredModel(name string) ([]byte, error) {
	path, err := modelregistry.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve local model registry: %w", err)
	}
	models, err := modelregistry.Load(path)
	if err != nil {
		return nil, fmt.Errorf("read private model registry: %w", err)
	}
	var selected *modelregistry.Model
	for i := range models.Models {
		if models.Models[i].Name == name {
			selected = &models.Models[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("model alias %q is not registered locally", name)
	}

	providerPath, err := providerregistry.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve local provider registry: %w", err)
	}
	providers, err := providerregistry.Load(providerPath)
	if err != nil {
		return nil, fmt.Errorf("read private provider registry: %w", err)
	}
	var owner *providerregistry.Provider
	nativeChatGPTOwners := 0
	for i := range providers.Providers {
		p := &providers.Providers[i]
		if p.Type == "chatgpt" {
			nativeChatGPTOwners++
		}
		if p.Alias == selected.Provider {
			owner = p
		}
	}
	if owner == nil {
		return nil, fmt.Errorf("registered model provider identity does not match any local provider")
	}
	switch owner.Type {
	case "chatgpt":
		// Pinned LiteLLM uses one native CHATGPT_TOKEN_DIR/auth.json, not
		// a per-model credential. Other provider types are independent.
		if nativeChatGPTOwners != 1 {
			return nil, fmt.Errorf("native ChatGPT rendering requires exactly one locally registered ChatGPT identity")
		}
		return gateway.ChatGPTModelRegistration(selected.Name, selected.Source)
	case "opencode-go":
		return gateway.OpenCodeGoModelRegistration(selected.Name, selected.Source)
	default:
		return nil, fmt.Errorf("unsupported model provider type")
	}
}
