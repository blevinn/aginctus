package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This fixed native Responses route matches the OpenCode Go pilot registrar.
// Other OpenCode Go models may use protocols outside Responses.
const openCodeGoResponsesBase = "https://opencode.ai/inference/go/openai/v1"

func supportedOpenCodeGoResponsesModel(model string) bool {
	switch model {
	case "grok-4.7", "gpt-6-luna", "gpt-5.6-luna",
		"muse-spark-1.3-contributor", "muse-spark-1.2-contributor":
		return true
	default:
		return false
	}
}

// OpenCodeGoModelRegistration previews a nonsecret LiteLLM Responses route.
// It never sets an API key, contacts the gateway, or activates a model.
func OpenCodeGoModelRegistration(name, source string) ([]byte, error) {
	if !registrationNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid model name")
	}
	if !strings.HasPrefix(source, "openai/") ||
		!supportedOpenCodeGoResponsesModel(strings.TrimPrefix(source, "openai/")) {
		return nil, fmt.Errorf("unsupported OpenCode Go native Responses model")
	}
	registration := struct {
		Name   string            `json:"model_name"`
		Params map[string]string `json:"litellm_params"`
		Info   map[string]string `json:"model_info"`
	}{
		Name:   name,
		Params: map[string]string{"model": source, "api_base": openCodeGoResponsesBase},
		Info:   map[string]string{"mode": "responses"},
	}
	return json.MarshalIndent(registration, "", "  ")
}

// OpenCodeGoProviderRegistration previews a local identity without authentication.
func OpenCodeGoProviderRegistration(alias string) ([]byte, error) {
	if !registrationNamePattern.MatchString(alias) {
		return nil, fmt.Errorf("invalid provider alias")
	}
	return json.MarshalIndent(map[string]string{
		"provider": "opencode-go",
		"alias":    alias,
	}, "", "  ")
}
