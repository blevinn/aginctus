package siwc

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

// Plugin binds enrollment and catalog discovery to one named provider instance.
// OAuth tokens stay in its private store, never in provider configuration.
func Plugin() gatewaycontrol.Plugin {
	return gatewaycontrol.Plugin{
		Name: "openai-siwc", DefaultProvider: "openai-siwc",
		Description:    "Sign in with ChatGPT using Aginctus dynamic registration and public Responses",
		Validate:       validatePluginConfiguration,
		Login:          loginProvider,
		DiscoverModels: discoverProviderModels,
	}
}

func validatePluginConfiguration(config gatewaycontrol.Configuration) error {
	if config.StateDir == "" {
		return fmt.Errorf("openai-siwc requires an explicit private state_dir")
	}
	if len(config.MCPs) != 0 {
		return fmt.Errorf("openai-siwc offers inference models, not provider-hosted MCP servers")
	}
	for name, model := range config.Models {
		if !modelPattern.MatchString(model.Source) || name != model.Source {
			return fmt.Errorf("SIWC provider models use exact catalog slugs; rename them in catalogs")
		}
	}
	return nil
}

func providerOwner(provider gatewaycontrol.Provider) (*Owner, error) {
	if provider.Plugin != "openai-siwc" || !ValidName(provider.Name) {
		return nil, Error("SIWC provider requires a simple account name (letters, digits, hyphen, underscore)")
	}
	return Open(provider.Config.StateDir)
}

func loginProvider(ctx context.Context, provider gatewaycontrol.Provider, options gatewaycontrol.LoginOptions) error {
	owner, err := providerOwner(provider)
	if err == nil {
		err = owner.Login(ctx, provider.Name, LoginOptions{Output: options.Output, EnablePlan: options.EnablePlan})
	}
	if err != nil {
		return errors.New(Diagnostic(err))
	}
	return nil
}

func discoverProviderModels(ctx context.Context, provider gatewaycontrol.Provider) ([]gatewaycontrol.Model, error) {
	owner, err := providerOwner(provider)
	if err != nil {
		return nil, errors.New(Diagnostic(err))
	}
	models, err := owner.Models(ctx, provider.Name)
	if err != nil {
		return nil, errors.New(Diagnostic(err))
	}
	result := make([]gatewaycontrol.Model, 0, len(models))
	for _, model := range models {
		result = append(result, gatewaycontrol.Model{Name: model.Slug, Source: model.Slug, DisplayName: model.DisplayName})
	}
	return result, nil
}
