package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

// Registry execution does not parse command words or flags: the caller's
// leaf-specific Cobra validation already established the operation.
func executeGatewayModelRead(action, name string, stdout, stderr io.Writer) int {
	if action == "render" {
		definition, err := renderRegisteredModel(name)
		if err != nil {
			fmt.Fprintf(stderr, "render registered model: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(definition))
		fmt.Fprintln(stderr, "model definition rendered for review only; gateway routing and credentials remain inactive")
		return 0
	}
		path, err := modelregistry.DefaultPath()
		if err != nil {
			fmt.Fprintln(stderr, "resolve model registry: unavailable")
			return 1
		}
		reg, err := modelregistry.Load(path)
		if err != nil {
			fmt.Fprintln(stderr, "read private model registry: invalid or inaccessible")
			return 1
		}
		if action == "list" {
			for _, model := range reg.Models {
				fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", model.Name, model.Source, model.Provider, "activation pending")
			}
			return 0
		}
		for _, model := range reg.Models {
			if model.Name == name {
				fmt.Fprintf(stdout, "model %q: source %q via provider %q (authorization and activation pending)\n", model.Name, model.Source, model.Provider)
				return 0
			}
		}
		fmt.Fprintf(stderr, "model alias %q is not registered\n", name)
		return 1
}

func executeGatewayProviderRead(action, name string, stdout, stderr io.Writer) int {
		path, err := providerregistry.DefaultPath()
		if err != nil {
			fmt.Fprintf(stderr, "provider registry path: %v\n", err)
			return 1
		}
		reg, err := providerregistry.Load(path)
		if err != nil {
			fmt.Fprintf(stderr, "read provider registry: %v\n", err)
			return 1
		}
		if action == "list" {
			for _, p := range reg.Providers {
				fmt.Fprintf(stdout, "%s\t%s\t%s\n", p.Alias, p.Type, "authentication pending")
			}
			return 0
		}
		for _, p := range reg.Providers {
			if p.Alias == name {
				fmt.Fprintf(stdout, "provider %q (%s): registered locally (authentication pending)\n", p.Alias, p.Type)
				return 0
			}
		}
		fmt.Fprintf(stderr, "provider alias %q is not registered\n", name)
		return 1
}

func executeGatewayRegistration(kind, name, provider, source, alias string, dryRun bool, stdout, stderr io.Writer) int {
		if kind == "provider" {
			if alias == "" {
				alias = name
			}
			var rendered []byte
			var err error
			switch name {
			case "chatgpt":
				rendered, err = gateway.ChatGPTProviderRegistration(alias)
			case "opencode-go":
				rendered, err = gateway.OpenCodeGoProviderRegistration(alias)
			default:
				fmt.Fprintln(stderr, "unsupported provider type")
				return 2
			}
			if err != nil {
				fmt.Fprintf(stderr, "gateway provider: %v\n", err)
				return 2
			}
			if dryRun {
				fmt.Fprintln(stdout, string(rendered))
				return 0
			}
			path, err := providerregistry.DefaultPath()
			if err != nil {
				fmt.Fprintf(stderr, "provider registry path: %v\n", err)
				return 1
			}
			changed, err := providerregistry.Register(path, providerregistry.Provider{Type: name, Alias: alias})
			if err != nil {
				fmt.Fprintf(stderr, "register provider: %v\n", err)
				return 1
			}
			if changed {
				fmt.Fprintf(stdout, "provider %q (%s): registered locally (authentication pending)\n", alias, name)
			} else {
				fmt.Fprintf(stdout, "provider %q (%s): already registered (authentication pending)\n", alias, name)
			}
			return 0
		}

		if source == "" {
			source = name
		}
		if provider == "" && strings.HasPrefix(source, "chatgpt/") {
			provider = "chatgpt"
		}
		if provider == "" {
			fmt.Fprintln(stderr, "model registration requires --provider=ALIAS (or --source=chatgpt/...)")
			return 2
		}
		path, err := providerregistry.DefaultPath()
		if err != nil {
			fmt.Fprintln(stderr, "cannot verify provider alias")
			return 1
		}
		providers, err := providerregistry.Load(path)
		if err != nil {
			fmt.Fprintln(stderr, "cannot read private provider registry")
			return 1
		}
		providerType := ""
		for _, p := range providers.Providers {
			if p.Alias == provider {
				providerType = p.Type
				break
			}
		}
		if providerType == "" {
			fmt.Fprintf(stderr, "model provider alias %q is not registered locally\n", provider)
			if dryRun {
				return 2
			}
			return 1
		}
		var rendered []byte
		switch providerType {
		case "chatgpt":
			rendered, err = gateway.ChatGPTModelRegistration(name, source)
		case "opencode-go":
			rendered, err = gateway.OpenCodeGoModelRegistration(name, source)
		default:
			fmt.Fprintln(stderr, "unsupported model provider type")
			return 2
		}
		if err != nil {
			fmt.Fprintf(stderr, "invalid %s Responses model registration: %v\n", providerType, err)
			return 2
		}
		if dryRun {
			fmt.Fprintln(stdout, string(rendered))
			return 0
		}
		modelPath, err := modelregistry.DefaultPath()
		if err != nil {
			fmt.Fprintln(stderr, "resolve model registry: unavailable")
			return 1
		}
		changed, err := modelregistry.Register(modelPath, modelregistry.Model{Name: name, Provider: provider, Source: source})
		if err != nil {
			fmt.Fprintf(stderr, "register model: %v\n", err)
			return 1
		}
		if changed {
			fmt.Fprintf(stdout, "model %q: registered locally (authorization and activation pending)\n", name)
		} else {
			fmt.Fprintf(stdout, "model %q: already registered (authorization and activation pending)\n", name)
		}
		return 0
}
