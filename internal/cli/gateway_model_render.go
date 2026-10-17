package cli

import (
	"fmt"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

// renderRegisteredChatGPTModel resolves a previously stored *local intent*
// to LiteLLM's native Responses model shape. It is strictly read-only:
// registration with a running gateway, credentials, and OAuth are untouched.
//
// LiteLLM v1.103.0 uses one CHATGPT_TOKEN_DIR/auth.json for its native
// authenticator, rather than a per-route credential identity. Refuse multiple
// local identities until the account-to-route binding is explicitly designed.
func renderRegisteredChatGPTModel(name string) ([]byte, error) {
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
	if len(providers.Providers) != 1 {
		return nil, fmt.Errorf("native ChatGPT rendering requires exactly one locally registered provider identity")
	}
	p := providers.Providers[0]
	if p.Type != "chatgpt" || p.Alias != selected.Provider {
		return nil, fmt.Errorf("registered model provider identity does not match the sole native ChatGPT credential owner")
	}
	return gateway.ChatGPTModelRegistration(selected.Name, selected.Source)
}
