package gateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ChatGPTModelRegistration produces the model configuration accepted by
// LiteLLM's model-management API. It never reads or exposes OAuth credentials.
// Registration is separate from activation until a writable private credential
// volume and explicit enrollment have been validated.
func ChatGPTModelRegistration(alias, model string) ([]byte, error) {
	safe := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}package gateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ChatGPTModelRegistration produces the model configuration accepted by
// LiteLLM's model-management API. It never reads or exposes OAuth credentials.
// Registration is separate from activation until a writable private credential
// volume and explicit enrollment have been validated.
func ChatGPTModelRegistration(alias, model string) ([]byte, error) {
	safe := regexp.MustCompile()
	if !safe.MatchString(alias) || strings.Contains(alias, "/") {
		return nil, fmt.Errorf("invalid model name")
	}
	if !strings.HasPrefix(model, "chatgpt/") || !safe.MatchString(strings.TrimPrefix(model, "chatgpt/")) {
		return nil, fmt.Errorf("expected a chatgpt/<model> provider model")
	}
	registration := struct {
		Name   string            `json:"model_name"`
		Params map[string]string `json:"litellm_params"`
		Info   map[string]string `json:"model_info"`
	}{alias, map[string]string{"model": model}, map[string]string{"mode": "responses"}}
	return json.MarshalIndent(registration, "", "  ")
}

// ChatGPTProviderRegistration previews a provider alias; authentication is never performed here.
func ChatGPTProviderRegistration(alias string) ([]byte, error) {
 safe := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
 if !safe.MatchString(alias) { return nil, fmt.Errorf("invalid provider alias") }
 return json.MarshalIndent(map[string]string{"provider":"chatgpt","alias":alias}, "", "  ")
}
