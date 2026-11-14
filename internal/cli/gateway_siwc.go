package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	siwc "github.com/blevinn/aginctus/internal/plugins/openai-siwc"
)

var exchangeGatewaySIWC = gateway.ExchangeSIWC
var prepareGatewaySIWC = gateway.PrepareSIWC

func siwcAttachment(effective *config.Config) (gateway.CredentialAttachment, error) {
	attachment := gateway.CredentialAttachment{}
	for _, key := range []struct {
		path  string
		value *string
	}{
		{"gateway.id", &attachment.GatewayID}, {"gateway.compose.project", &attachment.ComposeProject}, {"gateway.credentials.storage.pool", &attachment.Pool},
	} {
		value, err := effective.String(key.path)
		if err != nil {
			return attachment, err
		}
		*key.value = value
	}
	return attachment, attachment.Validate()
}

func runManagedSIWC(ctx context.Context, effective *config.Config, store *gatewaycontrol.Store, operation siwc.Operation) (siwc.Result, error) {
	result := siwc.Result{}
	plan := &siwc.Plan{Providers: map[string]bool{operation.Account: true}}
	if operation.Action == "login" && (!operation.ConsentConnectedApp || !operation.ConsentBackground) {
		return result, errors.New("SIWC login requires --consent-connected-app and --consent-background")
	}
	if operation.Action == "sync" || operation.Action == "login" {
		var err error
		plan, err = siwc.Configure(store)
		if err != nil {
			return result, err
		}
	}
	attachment, err := siwcAttachment(effective)
	if err != nil {
		return result, err
	}
	err = exchangeGatewaySIWC(ctx, attachment, func(state json.RawMessage) (json.RawMessage, error) {
		updated, value, err := siwc.Manage(ctx, state, plan, operation)
		result = value
		if err != nil {
			err = errors.New(siwc.Diagnostic(err))
		}
		return json.RawMessage(updated), err
	})
	return result, err
}

func configureSIWCOperations(store *gatewaycontrol.Store, effective *config.Config) {
	plugin := store.Plugins["openai-siwc"]
	plugin.Login = func(ctx context.Context, provider gatewaycontrol.Provider, options gatewaycontrol.LoginOptions) error {
		_, err := runManagedSIWC(ctx, effective, store, siwc.Operation{Action: "login", Account: provider.Name, Login: siwc.LoginOptions{Output: options.Output, EnablePlan: options.EnablePlan}, ConsentConnectedApp: options.ConsentConnectedApp, ConsentBackground: options.ConsentBackground})
		if err == nil && options.Output != nil {
			fmt.Fprintln(options.Output, "SIWC credentials and configured routes committed to the managed gateway.")
		}
		return err
	}
	plugin.DiscoverModels = func(ctx context.Context, provider gatewaycontrol.Provider) ([]gatewaycontrol.Model, error) {
		result, err := runManagedSIWC(ctx, effective, store, siwc.Operation{Action: "models", Account: provider.Name})
		models := make([]gatewaycontrol.Model, 0, len(result.Models))
		for _, model := range result.Models {
			models = append(models, gatewaycontrol.Model{Name: model.Slug, Source: model.Slug, DisplayName: model.DisplayName})
		}
		return models, err
	}
	plugin.Status = func(ctx context.Context, provider gatewaycontrol.Provider) (any, error) {
		result, err := runManagedSIWC(ctx, effective, store, siwc.Operation{Action: "status", Account: provider.Name})
		return map[string]any{provider.Name: result.Status[provider.Name]}, err
	}
	plugin.Logout = func(ctx context.Context, provider gatewaycontrol.Provider) error {
		result, err := runManagedSIWC(ctx, effective, store, siwc.Operation{Action: "logout", Account: provider.Name})
		if err == nil && !result.Revoked {
			return errors.New("local tokens cleared; remote revocation not confirmed; disconnect Aginctus in ChatGPT Settings")
		}
		return err
	}
	store.Plugins["openai-siwc"] = plugin
}

// syncGatewaySIWC preserves the existing managed credential preparation and
// synchronization sequence; the lifecycle operation invokes it after deploy.
func syncGatewaySIWC(ctx context.Context, effective *config.Config) error {
	attachment, err := siwcAttachment(effective)
	if err != nil {
		return err
	}
	if err := prepareGatewaySIWC(ctx, attachment); err != nil {
		return err
	}
	store, err := configuredGatewayStore(effective)
	if err != nil {
		return err
	}
	_, err = runManagedSIWC(ctx, effective, store, siwc.Operation{Action: "sync"})
	return err
}
