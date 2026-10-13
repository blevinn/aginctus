package gateway

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var registrationNamePattern = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")

// ChatGPTModelRegistration previews a native LiteLLM Responses model.
// It does not read credentials or register anything with a live gateway.
func ChatGPTModelRegistration(name, source string) ([]byte, error) {
	validName := registrationNamePattern.MatchString(name) ||
		(strings.HasPrefix(name, "chatgpt/") &&
			registrationNamePattern.MatchString(strings.TrimPrefix(name, "chatgpt/")))
	if !validName {
		return nil, fmt.Errorf("invalid model name")
	}
	if !strings.HasPrefix(source, "chatgpt/") ||
		!registrationNamePattern.MatchString(strings.TrimPrefix(source, "chatgpt/")) {
		return nil, fmt.Errorf("expected a chatgpt/<model> provider model")
	}
	registration := struct {
		Name   string            `json:"model_name"`
		Params map[string]string `json:"litellm_params"`
		Info   map[string]string `json:"model_info"`
	}{
		Name:   name,
		Params: map[string]string{"model": source},
		Info:   map[string]string{"mode": "responses"},
	}
	return json.MarshalIndent(registration, "", "  ")
}

// ChatGPTProviderRegistration previews a provider alias without authentication.
func ChatGPTProviderRegistration(alias string) ([]byte, error) {
	if !registrationNamePattern.MatchString(alias) {
		return nil, fmt.Errorf("invalid provider alias")
	}
	return json.MarshalIndent(map[string]string{
		"provider": "chatgpt",
		"alias":    alias,
	}, "", "  ")
}
