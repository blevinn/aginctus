package siwc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
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
		return "", Error("SIWC account is signed out; stop the gateway and sign in again")
	}
	if record.RefreshPending {
		return "", Error("SIWC refresh outcome uncertain; stop the gateway and reauthorize")
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

type Binding struct {
	Account, Route, Model, WorkloadKeyFile string
	ConsentConnectedApp, ConsentBackground bool
}

func (o *Owner) Bind(ctx context.Context, binding Binding) error {
	if !binding.ConsentConnectedApp || !binding.ConsentBackground {
		return Error("Route binding requires connected-application and background-use consent")
	}
	if !ValidName(binding.Route) || !strings.HasPrefix(binding.Route, "siwc-") {
		return Error("SIWC route aliases must start with siwc- to select the OpenCode request profile")
	}
	key, err := privateRead(binding.WorkloadKeyFile, 4096)
	if err != nil {
		return err
	}
	key = bytes.TrimSpace(key)
	if !bytes.HasPrefix(key, []byte("sk-")) || len(key) < 12 || slices.ContainsFunc(key, func(c byte) bool { return c < 33 || c > 126 }) {
		return Error("Expected a private LiteLLM workload key file")
	}
	hash := sha256.Sum256(key)
	return o.mutate(ctx, func(state *State) error {
		models, err := o.models(ctx, state, binding.Account)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(models, func(m Model) bool { return m.Slug == binding.Model }) {
			return Error("Selected model is not in this account's displayed catalog")
		}
		record := state.Accounts[binding.Account]
		route := Route{Account: binding.Account, ClientID: record.ClientID, Subject: record.Subject, Model: binding.Model, KeyHashes: []string{hex.EncodeToString(hash[:])}, ConsentBackground: true, ConsentConnectedApp: true}
		if existing, ok := state.Routes[binding.Route]; ok && !reflect.DeepEqual(existing, route) {
			return Error("Route binding already exists; remove it before changing account, model or caller")
		}
		state.Routes[binding.Route] = route
		return o.store.write(state)
	})
}

func (o *Owner) Unbind(ctx context.Context, route string) error {
	return o.mutate(ctx, func(state *State) error { delete(state.Routes, route); return o.store.write(state) })
}

func (o *Owner) Render(ctx context.Context) (any, error) {
	state, err := o.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	models := []any{}
	aliases := make([]string, 0, len(state.Routes))
	for alias := range state.Routes {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	for _, alias := range aliases {
		route := state.Routes[alias]
		models = append(models, map[string]any{"model_name": alias, "model_info": map[string]string{"mode": "responses"}, "litellm_params": map[string]string{"model": "openai/" + route.Model, "api_base": Resource, "api_key": "aginctus-siwc:" + alias}})
	}
	return map[string]any{"model_list": models, "litellm_settings": map[string]any{"callbacks": []string{"litellm_siwc.proxy_handler_instance"}, "turn_off_message_logging": true}, "router_settings": map[string]any{"num_retries": 0, "fallbacks": []string{}}}, nil
}

type capsule struct {
	Version   int              `json:"version"`
	HandoffID string           `json:"handoff_id"`
	HostID    string           `json:"host_id"`
	Account   string           `json:"account"`
	Record    *Record          `json:"record"`
	Routes    map[string]Route `json:"routes"`
}

func (o *Owner) Export(ctx context.Context, account, target, path string) error {
	if !validHost(target) {
		return Error("Initialize the target SIWC store and supply its host ID")
	}
	if !filepath.IsAbs(path) || filepath.Dir(path) != o.store.Root || filepath.Clean(path) != path {
		return Error("Write the handoff capsule directly inside the private SIWC state directory")
	}
	if slices.Contains([]string{"state.json", "state.flock", "runtime.flock"}, filepath.Base(path)) {
		return Error("SIWC handoff destination must be a new capsule file")
	}
	return o.mutate(ctx, func(state *State) error {
		record := state.Accounts[account]
		if record == nil || record.RefreshToken == "" || record.RefreshPending {
			return Error("No renewable SIWC session available for exclusive transfer")
		}
		copyRecord := *record
		handoff := capsule{Version: 1, HandoffID: randomHex(), HostID: target, Account: account, Record: &copyRecord, Routes: map[string]Route{}}
		for alias, route := range state.Routes {
			if route.Account == account {
				handoff.Routes[alias] = route
			}
		}
		data, err := json.Marshal(handoff)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return Error("SIWC handoff destination must be a new capsule file")
		}
		defer file.Close()
		// Reserve first, disarm durably, then publish a usable capsule.
		clearTokens(record)
		if err := o.store.write(state); err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			return err
		}
		return file.Sync()
	})
}

func (o *Owner) Receive(ctx context.Context, path string) error {
	return o.mutate(ctx, func(state *State) error {
		data, err := privateRead(path, maxState)
		if err != nil {
			return err
		}
		var handoff capsule
		if json.Unmarshal(data, &handoff) != nil || handoff.Record == nil || handoff.Routes == nil || !ValidName(handoff.Account) {
			return Error("Malformed SIWC handoff capsule")
		}
		if handoff.Version != 1 || handoff.HostID != state.HostID || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(handoff.HandoffID) || slices.Contains(state.Received, handoff.HandoffID) || state.Accounts[handoff.Account] != nil {
			return Error("SIWC handoff target, replay or account collision")
		}
		record := handoff.Record
		access, err := o.provider.claims(ctx, record.AccessToken, Resource, "")
		if err != nil {
			return err
		}
		identity, err := o.provider.claims(ctx, record.IDToken, record.ClientID, "")
		if err != nil {
			return err
		}
		if identity.Subject != record.Subject || access.Subject != record.Subject || access.ClientID != record.ClientID || record.RefreshToken == "" || record.RefreshPending || record.ExpiresAt > access.Expires {
			return Error("SIWC handoff credentials do not match their registration")
		}
		for alias, route := range handoff.Routes {
			if _, exists := state.Routes[alias]; exists || route.Account != handoff.Account || route.ClientID != record.ClientID || route.Subject != record.Subject {
				return Error("SIWC handoff route binding mismatch or collision")
			}
		}
		state.Accounts[handoff.Account] = record
		for alias, route := range handoff.Routes {
			state.Routes[alias] = route
		}
		state.Received = append(state.Received, handoff.HandoffID)
		return o.store.write(state)
	})
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
