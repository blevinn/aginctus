package siwc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

// RouteIntent is derived from provider/catalog configuration, never edited state.
type RouteIntent struct{ Account, Model, KeyHash string }

type Plan struct {
	Routes    map[string]RouteIntent
	Providers map[string]bool
}

func Plugin() gatewaycontrol.Plugin {
	return gatewaycontrol.Plugin{Name: "openai-siwc", DefaultProvider: "openai-siwc",
		Description: "Managed Sign in with ChatGPT through public Responses",
		Validate: func(c gatewaycontrol.Configuration) error {
			if c.StateDir != "" || len(c.MCPs) != 0 {
				return fmt.Errorf("openai-siwc uses managed gateway credentials and offers models only")
			}
			for name, model := range c.Models {
				if !modelPattern.MatchString(model.Source) || name != model.Source {
					return fmt.Errorf("SIWC models use exact upstream slugs; rename them in catalogs")
				}
			}
			for alias, route := range c.Routes {
				if !ValidName(alias) || !strings.HasPrefix(alias, "siwc-") || !gatewaycontrol.ValidName(route.Catalog) || !filepath.IsAbs(route.WorkloadKeyFile) || filepath.Clean(route.WorkloadKeyFile) != route.WorkloadKeyFile {
					return fmt.Errorf("SIWC routes require a siwc- alias, catalog and absolute workload_key_file")
				}
			}
			return nil
		},
	}
}

func Configure(store *gatewaycontrol.Store) (*Plan, error) {
	providers, err := store.Providers()
	if err != nil {
		return nil, err
	}
	assets, err := store.Assets()
	if err != nil {
		return nil, err
	}
	plan := &Plan{Routes: map[string]RouteIntent{}, Providers: map[string]bool{}}
	for name, provider := range providers {
		if provider.Plugin != "openai-siwc" {
			continue
		}
		if !ValidName(name) {
			return nil, fmt.Errorf("SIWC provider requires a simple account label")
		}
		plan.Providers[name] = true
		for alias, route := range provider.Config.Routes {
			catalog, err := gatewaycontrol.Resolve(route.Catalog, providers, assets)
			if err != nil {
				return nil, err
			}
			model, ok := catalog.Models[alias]
			if !ok || model.Provider != name {
				return nil, fmt.Errorf("SIWC route must expose a model from its own provider")
			}
			if _, exists := plan.Routes[alias]; exists {
				return nil, fmt.Errorf("duplicate SIWC route alias")
			}
			key, err := privateRead(route.WorkloadKeyFile, 4096)
			if err != nil {
				return nil, Error("SIWC workload key file must be private, owned and readable")
			}
			key = bytes.TrimSpace(key)
			if !bytes.HasPrefix(key, []byte("sk-")) || len(key) < 12 || slices.ContainsFunc(key, func(c byte) bool { return c < 33 || c > 126 }) {
				return nil, Error("Expected a private LiteLLM workload key file")
			}
			hash := sha256.Sum256(key)
			plan.Routes[alias] = RouteIntent{Account: name, Model: provider.Config.Models[model.Name].Source, KeyHash: hex.EncodeToString(hash[:])}
		}
	}
	return plan, nil
}

func (p *Plan) RuntimeConfig() (string, error) {
	models := []any{}
	aliases := make([]string, 0, len(p.Routes))
	for alias := range p.Routes {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	for _, alias := range aliases {
		route := p.Routes[alias]
		models = append(models, map[string]any{"model_name": alias, "model_info": map[string]string{"mode": "responses"}, "litellm_params": map[string]string{"model": "openai/" + route.Model, "api_base": Resource, "api_key": "aginctus-siwc:" + alias}})
	}
	data, err := json.Marshal(map[string]any{"model_list": models, "litellm_settings": map[string]any{"callbacks": []string{"litellm_siwc.proxy_handler_instance"}, "turn_off_message_logging": true}, "router_settings": map[string]any{"num_retries": 0, "fallbacks": []string{}}})
	return string(data), err
}
