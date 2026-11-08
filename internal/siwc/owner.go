package siwc

import (
	"context"
	"errors"
	"slices"
	"time"
)

type Owner struct {
	store    *Store
	provider *provider
}

func Open(root string) (*Owner, error) {
	store, err := OpenStore(root)
	if err != nil {
		return nil, err
	}
	return &Owner{store: store, provider: newProvider()}, nil
}

// mutate excludes the lifetime LiteLLM lease before acquiring the state lock.
func (o *Owner) mutate(ctx context.Context, action func(*State) error) error {
	release, err := o.store.lock(ctx, "runtime", 0)
	if err != nil {
		return err
	}
	defer release()
	releaseState, err := o.store.lock(ctx, "state", 20*time.Second)
	if err != nil {
		return err
	}
	defer releaseState()
	state, err := o.store.read()
	if err != nil {
		return err
	}
	return action(state)
}

func (o *Owner) Init(ctx context.Context) (string, error) {
	var host string
	err := o.mutate(ctx, func(state *State) error { host = state.HostID; return o.store.write(state) })
	return host, err
}

func (o *Owner) snapshot(ctx context.Context) (*State, error) {
	release, err := o.store.lock(ctx, "state", 20*time.Second)
	if err != nil {
		return nil, err
	}
	defer release()
	return o.store.read()
}

func (o *Owner) Status(ctx context.Context) (map[string]any, error) {
	state, err := o.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for label, record := range state.Accounts {
		status := "configured_not_remotely_verified"
		switch {
		case record.RefreshPending:
			status = "recovery_required"
		case record.AccessToken == "":
			status = "signed_out"
		case !slices.Contains(record.Scopes, Direct):
			status = "plan_disabled"
		case record.ExpiresAt <= float64(o.provider.now().Unix()):
			status = "expired"
		}
		var subject any
		if record.Subject != "" {
			subject = record.Subject
		}
		result[label] = map[string]any{"client_id": record.ClientID, "subject": subject, "state": status}
	}
	return result, nil
}

// access is only called with the runtime and state locks held by the CLI.
// The in-process LiteLLM owner uses the same durable refresh marker protocol.
func (o *Owner) access(ctx context.Context, state *State, account string) (string, error) {
	record := state.Accounts[account]
	if !ValidName(account) || record == nil || record.RefreshToken == "" {
		return "", Error("SIWC account is signed out; run gateway provider login")
	}
	if record.RefreshPending {
		return "", Error("SIWC refresh outcome uncertain; run gateway provider login")
	}
	if !slices.Contains(record.Scopes, Direct) {
		return "", Error("ChatGPT plan usage was not granted; explicitly enable it by signing in again")
	}
	now := float64(o.provider.now().Unix())
	if now < record.ExpiresAt-60 || (now < record.ExpiresAt && now < record.EarliestRefreshAt) {
		return record.AccessToken, nil
	}
	if now < record.EarliestRefreshAt {
		return "", Error("SIWC access expired before provider refresh window; retry later")
	}
	record.RefreshPending = true
	if err := o.store.write(state); err != nil {
		return "", err
	}
	replacement, err := o.provider.tokens(ctx, values("grant_type", "refresh_token", "client_id", record.ClientID, "refresh_token", record.RefreshToken, "resource", Resource), record, "")
	if err != nil {
		var provider *providerError
		if errors.As(err, &provider) {
			record.RefreshPending = false
			if slices.Contains(terminalCodes, provider.code) {
				clearTokens(record)
			}
			if writeErr := o.store.write(state); writeErr != nil {
				return "", writeErr
			}
		}
		return "", err
	}
	state.Accounts[account] = replacement
	if err := o.store.write(state); err != nil {
		return "", err
	}
	if !slices.Contains(replacement.Scopes, Direct) {
		return "", Error("ChatGPT plan usage is disabled on the refreshed grant")
	}
	return replacement.AccessToken, nil
}

type Model struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

func (o *Owner) models(ctx context.Context, state *State, account string) ([]Model, error) {
	token, err := o.access(ctx, state, account)
	if err != nil {
		return nil, err
	}
	var data struct {
		Models *[]struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if err := o.provider.request(ctx, Resource+"/models", nil, token, &data); err != nil {
		return nil, err
	}
	if data.Models == nil {
		return nil, Error("Invalid SIWC model catalog")
	}
	models := []Model{}
	for _, item := range *data.Models {
		if item.Visibility == "list" && modelPattern.MatchString(item.Slug) {
			if item.DisplayName == "" {
				item.DisplayName = item.Slug
			}
			models = append(models, Model{item.Slug, item.DisplayName})
		}
	}
	return models, nil
}

func (o *Owner) Models(ctx context.Context, account string) ([]Model, error) {
	var result []Model
	err := o.mutate(ctx, func(state *State) error { var err error; result, err = o.models(ctx, state, account); return err })
	return result, err
}

func (o *Owner) Logout(ctx context.Context, account string) (bool, error) {
	confirmed := false
	err := o.mutate(ctx, func(state *State) error {
		record := state.Accounts[account]
		if record == nil {
			return Error("Unknown SIWC account")
		}
		confirmed = record.RefreshToken == ""
		for attempt := 0; !confirmed && attempt < 3; attempt++ {
			discovery, err := o.provider.discover(ctx)
			if err == nil {
				var result map[string]any
				err = o.provider.request(ctx, discovery.Revocation, values("token", record.RefreshToken, "token_type_hint", "refresh_token", "client_id", record.ClientID), "", &result)
			}
			if err == nil {
				confirmed = true
				break
			}
			var provider *providerError
			if errors.As(err, &provider) && provider.status < 500 {
				break
			}
			if ctx.Err() != nil {
				break
			}
			if attempt < 2 {
				select {
				case <-ctx.Done():
				case <-time.After(200 * time.Millisecond * time.Duration(1<<attempt)):
				}
			}
		}
		clearTokens(record)
		return o.store.write(state)
	})
	return confirmed, err
}

// Diagnostic never formats arbitrary network, JWT, filesystem or context errors.
func Diagnostic(err error) string {
	var safe Error
	var provider *providerError
	if errors.As(err, &safe) || errors.As(err, &provider) {
		return err.Error()
	}
	return "SIWC operation interrupted or private state unavailable"
}
