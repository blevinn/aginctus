package openaisiwc

import (
	"context"
	"slices"
)

type Operation struct {
	Action, Account                        string
	Login                                  LoginOptions
	ConsentConnectedApp, ConsentBackground bool
}

type Result struct {
	Models  []Model
	Status  map[string]any
	Revoked bool
}

// Manage uses an in-memory copy under the gateway's authenticated exec lease.
// Return updated state even on failure to preserve registration and refresh markers.
func Manage(ctx context.Context, input []byte, plan *Plan, operation Operation) ([]byte, Result, error) {
	return manage(ctx, input, plan, operation, newProvider())
}

func manage(ctx context.Context, input []byte, plan *Plan, operation Operation, provider *provider) ([]byte, Result, error) {
	result := Result{}
	if operation.Action != "sync" && (!ValidName(operation.Account) || !plan.Providers[operation.Account]) {
		return nil, result, Error("Select a configured SIWC provider")
	}
	if operation.Action == "login" && (!operation.ConsentConnectedApp || !operation.ConsentBackground) {
		return nil, result, Error("SIWC login requires connected-application and background-use consent")
	}
	if len(input) == 0 || len(input) > maxState {
		return nil, result, Error("Invalid managed SIWC state size")
	}
	owner := &Owner{store: &Store{memory: slices.Clone(input)}, provider: provider}
	if _, err := owner.store.read(); err != nil {
		return nil, result, err
	}
	var err error
	switch operation.Action {
	case "login":
		err = owner.Login(ctx, operation.Account, operation.Login)
		if err == nil {
			state, readErr := owner.store.read()
			if readErr != nil {
				return owner.store.memory, result, readErr
			}
			if state.Consents == nil {
				state.Consents = map[string]Consent{}
			}
			record := state.Accounts[operation.Account]
			state.Consents[operation.Account] = Consent{ClientID: record.ClientID, Subject: record.Subject}
			err = owner.store.write(state)
			if err == nil {
				err = owner.configureRoutes(ctx, plan, operation.Account)
			}
		}
	case "sync":
		err = owner.configureRoutes(ctx, plan, "")
	case "models":
		result.Models, err = owner.Models(ctx, operation.Account)
	case "status":
		result.Status, err = owner.Status(ctx)
	case "logout":
		result.Revoked, err = owner.Logout(ctx, operation.Account)
		if err == nil {
			state, readErr := owner.store.read()
			if readErr != nil {
				return owner.store.memory, result, readErr
			}
			delete(state.Consents, operation.Account)
			for alias, route := range state.Routes {
				if route.Account == operation.Account {
					delete(state.Routes, alias)
				}
			}
			err = owner.store.write(state)
		}
	default:
		err = Error("Unknown managed SIWC operation")
	}
	return owner.store.memory, result, err
}

func (o *Owner) configureRoutes(ctx context.Context, plan *Plan, selected string) error {
	state, err := o.store.read()
	if err != nil {
		return err
	}
	routes := map[string]Route{}
	if selected != "" {
		for alias, route := range state.Routes {
			if route.Account != selected {
				routes[alias] = route
			}
		}
	}
	catalogs := map[string][]Model{}
	for alias, intent := range plan.Routes {
		if selected != "" && intent.Account != selected {
			continue
		}
		record := state.Accounts[intent.Account]
		consent, consented := state.Consents[intent.Account]
		if !consented || record == nil || record.RefreshToken == "" || !slices.Contains(record.Scopes, Direct) || record.RefreshPending {
			continue
		}
		if consent.ClientID != record.ClientID || consent.Subject != record.Subject {
			return Error("SIWC consent identity changed; sign in again")
		}
		models, ok := catalogs[intent.Account]
		if !ok {
			models, err = o.Models(ctx, intent.Account)
			if err != nil {
				return err
			}
			catalogs[intent.Account] = models
		}
		if !slices.ContainsFunc(models, func(model Model) bool { return model.Slug == intent.Model }) {
			return Error("Configured SIWC model is not in this account's displayed catalog")
		}
		routes[alias] = Route{Account: intent.Account, Model: intent.Model, ClientID: record.ClientID, Subject: record.Subject, KeyHashes: []string{intent.KeyHash}, ConsentBackground: true, ConsentConnectedApp: true}
	}
	// Model discovery may rotate credentials, so publish against the latest state.
	state, err = o.store.read()
	if err != nil {
		return err
	}
	state.Routes = routes
	return o.store.write(state)
}
