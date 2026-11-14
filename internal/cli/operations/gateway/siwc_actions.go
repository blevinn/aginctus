package gatewayops

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

// Credential exchange is injected so command execution never picks an
// implicit transport; authorization and consent rules stay in this operation.
func SIWCAttachment(effective *config.Config) (gateway.CredentialAttachment, error) {
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

func RunManagedSIWC(ctx context.Context, effective *config.Config, store *gatewaycontrol.Store, operation siwc.Operation, exchange func(context.Context, gateway.CredentialAttachment, gateway.CredentialUpdate) error) (siwc.Result, error) {
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
	attachment, err := SIWCAttachment(effective)
	if err != nil {
		return result, err
	}
	err = exchange(ctx, attachment, func(state json.RawMessage) (json.RawMessage, error) {
		updated, value, err := siwc.Manage(ctx, state, plan, operation)
		result = value
		if err != nil {
			err = errors.New(siwc.Diagnostic(err))
		}
		return json.RawMessage(updated), err
	})
	return result, err
}

func ConfigureSIWCOperations(store *gatewaycontrol.Store, effective *config.Config, exchange func(context.Context, gateway.CredentialAttachment, gateway.CredentialUpdate) error) {
	plugin := store.Plugins["openai-siwc"]
	plugin.Login = func(ctx context.Context, provider gatewaycontrol.Provider, options gatewaycontrol.LoginOptions) error {
		_, err := RunManagedSIWC(ctx, effective, store, siwc.Operation{Action: "login", Account: provider.Name, Login: siwc.LoginOptions{Output: options.Output, EnablePlan: options.EnablePlan}, ConsentConnectedApp: options.ConsentConnectedApp, ConsentBackground: options.ConsentBackground}, exchange)
		if err == nil && options.Output != nil {
			fmt.Fprintln(options.Output, "SIWC credentials and configured routes committed to the managed gateway.")
		}
		return err
	}
	plugin.DiscoverModels = func(ctx context.Context, provider gatewaycontrol.Provider) ([]gatewaycontrol.Model, error) {
		result, err := RunManagedSIWC(ctx, effective, store, siwc.Operation{Action: "models", Account: provider.Name}, exchange)
		models := make([]gatewaycontrol.Model, 0, len(result.Models))
		for _, model := range result.Models {
			models = append(models, gatewaycontrol.Model{Name: model.Slug, Source: model.Slug, DisplayName: model.DisplayName})
		}
		return models, err
	}
	plugin.Status = func(ctx context.Context, provider gatewaycontrol.Provider) (any, error) {
		result, err := RunManagedSIWC(ctx, effective, store, siwc.Operation{Action: "status", Account: provider.Name}, exchange)
		return map[string]any{provider.Name: result.Status[provider.Name]}, err
	}
	plugin.Logout = func(ctx context.Context, provider gatewaycontrol.Provider) error {
		result, err := RunManagedSIWC(ctx, effective, store, siwc.Operation{Action: "logout", Account: provider.Name}, exchange)
		if err == nil && !result.Revoked {
			return errors.New("local tokens cleared; remote revocation not confirmed; disconnect Aginctus in ChatGPT Settings")
		}
		return err
	}
	store.Plugins["openai-siwc"] = plugin
}

func SyncGatewaySIWC(ctx context.Context, effective *config.Config,
	prepare func(context.Context, gateway.CredentialAttachment) error,
	store func(*config.Config) (*gatewaycontrol.Store, error),
	manage func(context.Context, *config.Config, *gatewaycontrol.Store, siwc.Operation) (siwc.Result, error),
) error {
	attachment, err := SIWCAttachment(effective)
	if err != nil { return err }
	if err := prepare(ctx, attachment); err != nil { return err }
	registered, err := store(effective)
	if err != nil { return err }
	_, err = manage(ctx, effective, registered, siwc.Operation{Action: "sync"})
	return err
}
