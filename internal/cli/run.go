package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
	"github.com/blevinn/aginctus/internal/workload"
)

const Version = "0.0.0-dev"

type IncusClient interface {
	ServerVersion(context.Context) (string, error)
}

type ConfigLoader interface {
	Load(config.Options) (*config.Config, error)
}

var executeManagementNetwork = func(ctx context.Context, spec managementnetwork.Spec, options managementnetwork.Options) error {
	return spec.Execute(ctx, options)
}

var executeHerdrClient = func(ctx context.Context, spec herdrclient.Spec, options herdrclient.Options) error {
	if options.Operation == "delete" && !options.DryRun {
		if err := transitionHerdrClient(ctx, spec, "stop"); err != nil {
			return fmt.Errorf("stop Herdr client before deletion: %w", err)
		}
	}
	return spec.Execute(ctx, options)
}

var executeWorkload = func(ctx context.Context, spec workload.Spec, options workload.Options) error {
	if options.Operation == "delete" && !options.DryRun {
		if err := transitionWorkload(ctx, spec, "stop"); err != nil {
			return fmt.Errorf("stop workload before deletion: %w", err)
		}
	}
	return spec.Execute(ctx, options)
}

func Run(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	incusClient IncusClient,
	configLoader ConfigLoader,
) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(stdout)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintf(stdout, "aginctus %s\n", Version)
		return 0
	}

	options, commandArgs, err := parseGlobalOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "configuration options: %v\n", err)
		return 2
	}
	if len(commandArgs) == 0 {
		printUsage(stdout)
		return 0
	}

	effective, err := configLoader.Load(options)
	if err != nil {
		fmt.Fprintf(stderr, "configuration: %v\n", err)
		return 1
	}

	switch commandArgs[0] {
	case "remove":
		return runAggregateRemove(ctx, commandArgs[1:], stdout, stderr, effective)
	case "status":
		return runAggregateStatus(ctx, commandArgs[1:], stdout, stderr, effective)
	case "apply":
		return runAggregateApply(ctx, commandArgs[1:], stdout, stderr, effective)
	case "doctor":
		return runDoctor(ctx, commandArgs[1:], stdout, stderr, incusClient)
	case "images":
		return runImages(ctx, commandArgs[1:], stdout, stderr)
	case "config":
		return runConfig(commandArgs[1:], stdout, stderr, effective)
	case "network":
		return runNetwork(ctx, commandArgs[1:], stdout, stderr, effective)
	case "gateway":
		return runGateway(ctx, commandArgs[1:], stdout, stderr, incusClient, effective)
	case "herdr":
		return runHerdr(ctx, commandArgs[1:], stdout, stderr, incusClient, effective)
	case "workload":
		return runWorkload(ctx, commandArgs[1:], stdout, stderr, effective)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", commandArgs[0])
		printUsage(stderr)
		return 2
	}
}

func parseGlobalOptions(args []string) (config.Options, []string, error) {
	var options config.Options

	for len(args) > 0 {
		arg := args[0]
		if !strings.HasPrefix(arg, "--") {
			return options, args, nil
		}

		switch {
		case strings.HasPrefix(arg, "--config="):
			value := strings.TrimPrefix(arg, "--config=")
			if value == "" {
				return options, nil, fmt.Errorf("--config requires path=value")
			}
			options.Overrides = append(options.Overrides, value)
			args = args[1:]

		case arg == "--config":
			if len(args) < 2 {
				return options, nil, fmt.Errorf("--config requires path=value")
			}
			options.Overrides = append(options.Overrides, args[1])
			args = args[2:]

		case strings.HasPrefix(arg, "--configure="):
			value := strings.TrimPrefix(arg, "--configure=")
			if value == "" {
				return options, nil, fmt.Errorf("--configure requires path=value")
			}
			options.Overrides = append(options.Overrides, value)
			args = args[1:]

		case arg == "--configure":
			if len(args) < 2 {
				return options, nil, fmt.Errorf("--configure requires path=value")
			}
			options.Overrides = append(options.Overrides, args[1])
			args = args[2:]

		case strings.HasPrefix(arg, "--configuration-file="):
			value := strings.TrimPrefix(arg, "--configuration-file=")
			if value == "" {
				return options, nil, fmt.Errorf("--configuration-file requires a path")
			}
			options.ConfigurationFile = value
			args = args[1:]

		case arg == "--configuration-file":
			if len(args) < 2 {
				return options, nil, fmt.Errorf("--configuration-file requires a path")
			}
			options.ConfigurationFile = args[1]
			args = args[2:]

		default:
			return options, nil, fmt.Errorf("unknown global option %q", arg)
		}
	}

	return options, args, nil
}

type doctorHTTPSClient interface {
	HTTPSAddress(context.Context) (string, error)
}

var setIncusHTTPSAddress = func(ctx context.Context, address string) error {
	cmd := exec.CommandContext(ctx, "incus", "config", "set", "core.https_address="+address)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("incus config set: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, incusClient IncusClient) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--fix") {
		fmt.Fprintln(stderr, "Usage: aginctus doctor [--fix]")
		return 2
	}
	fix := len(args) == 1
	version, err := incusClient.ServerVersion(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "incus daemon: unreachable: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "incus daemon: reachable (%s)\n", version)
	client, ok := incusClient.(doctorHTTPSClient)
	if !ok {
		fmt.Fprintln(stderr, "incus HTTPS listener: cannot inspect with this client")
		return 1
	}
	address, err := client.HTTPSAddress(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "incus HTTPS listener: check failed: %v\n", err)
		return 1
	}
	if address != "" {
		fmt.Fprintf(stdout, "incus HTTPS listener: configured (%s)\n", address)
		return 0
	}
	if !fix {
		fmt.Fprintln(stderr, "incus HTTPS listener: absent (required by incus-compose image caching); run 'aginctus doctor --fix' to configure a loopback-only listener")
		return 1
	}
	// Never implicitly expose the Incus management API on all host interfaces.
	const loopback = "127.0.0.1:8443"
	if err := setIncusHTTPSAddress(ctx, loopback); err != nil {
		fmt.Fprintf(stderr, "incus HTTPS listener: remediation failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "incus HTTPS listener: configured (%s); verify image-cache connectivity in your Incus setup\n", loopback)
	return 0
}

func runConfig(args []string, stdout, stderr io.Writer, effective *config.Config) int {
	if len(args) == 1 && args[0] == "show" {
		data, err := json.MarshalIndent(effective.Values(), "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "encode configuration: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}

	if len(args) == 2 && args[0] == "get" {
		value, ok := effective.Get(args[1])
		if !ok {
			fmt.Fprintf(stderr, "configuration key %q is not set\n", args[1])
			return 1
		}
		data, err := json.Marshal(value)
		if err != nil {
			fmt.Fprintf(stderr, "encode configuration value: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}

	fmt.Fprintln(stderr, "Usage: aginctus [global options] config show")
	fmt.Fprintln(stderr, "       aginctus [global options] config get <path>")
	return 2
}

func runNetwork(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	effective *config.Config,
) int {
	if len(args) == 0 {
		printNetworkUsage(stderr)
		return 2
	}

	if args[0] == "status" {
		return runNetworkStatus(ctx, args[1:], stdout, stderr, effective)
	}
	if len(args) == 2 && args[0] == "firewall" && args[1] == "policy" {
		return runNetworkFirewallPolicy(ctx, stdout, stderr, effective)
	}
	if len(args) == 2 && args[0] == "firewall" && args[1] == "reconcile" {
		return runNetworkFirewallReconcile(ctx, stdout, stderr, effective)
	}

	action := args[0]
	options, err := parseNetworkOptions(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "network options: %v\n", err)
		return 2
	}

	spec, err := managementnetwork.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "management network configuration: %v\n", err)
		return 1
	}

	var operation string
	switch action {
	case "apply", "ensure":
		operation = "upsert"
	case "remove", "teardown":
		operation = "delete"
	default:
		printNetworkUsage(stderr)
		return 2
	}

	err = executeManagementNetwork(ctx, spec, managementnetwork.Options{
		DryRun:    options.DryRun,
		Force:     options.Force,
		Operation: operation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "management network: %v\n", err)
		return 1
	}

	switch {
	case operation == "upsert" && options.DryRun:
		fmt.Fprintf(stdout, "management network %q: would reconcile\n", spec.Name)
	case operation == "upsert":
		fmt.Fprintf(stdout, "management network %q: ready\n", spec.Name)
	case options.DryRun:
		fmt.Fprintf(stdout, "management network %q: would delete if present\n", spec.Name)
	default:
		fmt.Fprintf(stdout, "management network %q: absent\n", spec.Name)
	}
	return 0
}

type mutationOptions struct {
	DryRun bool
	Force  bool
}

func parseNetworkOptions(args []string) (mutationOptions, error) {
	var options mutationOptions
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			options.DryRun = true
		case "--force":
			options.Force = true
		default:
			return mutationOptions{}, fmt.Errorf("unknown option %q", arg)
		}
	}
	return options, nil
}

func printNetworkUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] network apply [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] network remove [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] network status")
	fmt.Fprintln(w, "       aginctus [global options] network firewall policy")
	fmt.Fprintln(w, "       aginctus [global options] network firewall reconcile  # privileged mutation")
}

var ensureCredentialStorage = gateway.EnsureCredentialVolume
var attachGatewayCredentials = gateway.AttachCredentialVolume
var auditGatewayCredentials = gateway.AuditCredentialAttachment

func runGateway(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	incusClient IncusClient,
	effective *config.Config,
) int {
	if len(args) >= 2 && args[0] == "responses" && args[1] == "verify" {
		return verifyGatewayResponsesStream(args[2:], stdout, stderr)
	}
	if len(args) == 2 && args[0] == "credential-storage" && args[1] == "audit" {
		id, err := effective.String("gateway.id")
		if err != nil {
			fmt.Fprintf(stderr, "gateway id: %v\n", err)
			return 1
		}
		project, err := effective.String("gateway.compose.project")
		if err != nil {
			fmt.Fprintf(stderr, "gateway Compose project: %v\n", err)
			return 1
		}
		pool, err := effective.String("gateway.credentials.storage.pool")
		if err != nil {
			fmt.Fprintf(stderr, "credential storage pool: %v\n", err)
			return 1
		}
		spec := gateway.CredentialAttachment{GatewayID: id, ComposeProject: project, Pool: pool}
		if err := auditGatewayCredentials(ctx, spec); err != nil {
			fmt.Fprintf(stderr, "credential attachment audit: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "credential volume attachment: owned and exclusive to LiteLLM (metadata only; in-guest permissions, OAuth and runtime readiness not verified)\n")
		return 0
	}
	if len(args) == 2 && args[0] == "credential-storage" && args[1] == "attach" {
		id, err := effective.String("gateway.id")
		if err != nil {
			fmt.Fprintf(stderr, "gateway id: %v\n", err)
			return 1
		}
		project, err := effective.String("gateway.compose.project")
		if err != nil {
			fmt.Fprintf(stderr, "gateway Compose project: %v\n", err)
			return 1
		}
		pool, err := effective.String("gateway.credentials.storage.pool")
		if err != nil {
			fmt.Fprintf(stderr, "credential storage pool: %v\n", err)
			return 1
		}
		spec := gateway.CredentialAttachment{GatewayID: id, ComposeProject: project, Pool: pool}
		if err := attachGatewayCredentials(ctx, spec); err != nil {
			fmt.Fprintf(stderr, "credential attachment: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "private credential volume attached exclusively to LiteLLM in Compose project %q (OAuth not activated)\n", project)
		return 0
	}

	if len(args) == 3 && args[0] == "model" && args[2] == "render" {
		definition, err := renderRegisteredChatGPTModel(args[1])
		if err != nil {
			fmt.Fprintf(stderr, "render registered ChatGPT model: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(definition))
		fmt.Fprintln(stderr, "model definition rendered for review only; gateway routing and OAuth remain inactive")
		return 0
	}
	if len(args) > 0 && args[0] == "model" && (len(args) == 2 && args[1] == "list" || len(args) == 3 && args[2] == "show") {
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
		if args[1] == "list" {
			for _, model := range reg.Models {
				fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", model.Name, model.Source, model.Provider, "activation pending")
			}
			return 0
		}
		for _, model := range reg.Models {
			if model.Name == args[1] {
				fmt.Fprintf(stdout, "model %q: source %q via provider %q (authorization and activation pending)\n", model.Name, model.Source, model.Provider)
				return 0
			}
		}
		fmt.Fprintf(stderr, "model alias %q is not registered\n", args[1])
		return 1
	}
	if len(args) > 0 && args[0] == "provider" && (len(args) == 2 && args[1] == "list" || len(args) == 3 && args[2] == "show") {
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
		if args[1] == "list" {
			for _, p := range reg.Providers {
				fmt.Fprintf(stdout, "%s\t%s\t%s\n", p.Alias, p.Type, "authentication pending")
			}
			return 0
		}
		for _, p := range reg.Providers {
			if p.Alias == args[1] {
				fmt.Fprintf(stdout, "provider %q (%s): registered locally (authentication pending)\n", p.Alias, p.Type)
				return 0
			}
		}
		fmt.Fprintf(stderr, "provider alias %q is not registered\n", args[1])
		return 1
	}
	if len(args) > 0 && (args[0] == "provider" || args[0] == "model") {
		if len(args) < 3 || args[2] != "register" {
			printGatewayUsage(stderr)
			return 2
		}
		kind, name := args[0], args[1]
		provider, source, alias, dryRun := "", "", "", false
		for _, arg := range args[3:] {
			switch {
			case arg == "--dry-run":
				dryRun = true
			case strings.HasPrefix(arg, "--alias=") && kind == "provider":
				alias = strings.TrimPrefix(arg, "--alias=")
			case strings.HasPrefix(arg, "--provider=") && kind == "model":
				provider = strings.TrimPrefix(arg, "--provider=")
			case strings.HasPrefix(arg, "--source=") && kind == "model":
				source = strings.TrimPrefix(arg, "--source=")
			default:
				fmt.Fprintf(stderr, "unknown or invalid registration option %q\n", arg)
				return 2
			}
		}
		if !dryRun {
			if kind == "model" {
				if source == "" {
					source = name
				}
				if provider == "" && strings.HasPrefix(source, "chatgpt/") {
					provider = "chatgpt"
				}
				if _, err := gateway.ChatGPTModelRegistration(name, source); err != nil || provider == "" {
					fmt.Fprintln(stderr, "invalid ChatGPT Responses model registration")
					return 2
				}
				path, err := modelregistry.DefaultPath()
				if err != nil {
					fmt.Fprintln(stderr, "resolve model registry: unavailable")
					return 1
				}
				changed, err := modelregistry.Register(path, modelregistry.Model{Name: name, Provider: provider, Source: source})
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
			if name != "chatgpt" {
				fmt.Fprintln(stderr, "only chatgpt provider is supported")
				return 2
			}
			if alias == "" {
				alias = name
			}
			if _, err := gateway.ChatGPTProviderRegistration(alias); err != nil {
				fmt.Fprintf(stderr, "gateway provider: %v\n", err)
				return 2
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
		var rendered []byte
		var err error
		if kind == "provider" {
			if name != "chatgpt" {
				fmt.Fprintln(stderr, "only chatgpt provider is supported")
				return 2
			}
			if alias == "" {
				alias = name
			}
			rendered, err = gateway.ChatGPTProviderRegistration(alias)
		} else {
			if source == "" {
				source = name
			}
			if provider == "" {
				if strings.HasPrefix(source, "chatgpt/") {
					provider = "chatgpt"
				}
			}
			if provider != "chatgpt" {
				path, pathErr := providerregistry.DefaultPath()
				if pathErr != nil {
					fmt.Fprintln(stderr, "cannot verify provider alias")
					return 1
				}
				providers, loadErr := providerregistry.Load(path)
				if loadErr != nil {
					fmt.Fprintln(stderr, "cannot read private provider registry")
					return 1
				}
				registered := false
				for _, p := range providers.Providers {
					if p.Type == "chatgpt" && p.Alias == provider {
						registered = true
						break
					}
				}
				if !registered {
					fmt.Fprintln(stderr, "model provider alias is not registered")
					return 2
				}
			}
			rendered, err = gateway.ChatGPTModelRegistration(name, source)
		}
		if err != nil {
			fmt.Fprintf(stderr, "gateway registration preview: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, string(rendered))
		return 0
	}
	if len(args) == 2 && args[0] == "credential-storage" && args[1] == "ensure" {
		id, err := effective.String("gateway.id")
		if err != nil {
			fmt.Fprintf(stderr, "gateway credential storage gateway ID: %v\n", err)
			return 1
		}
		project, err := effective.String("gateway.compose.project")
		if err != nil {
			fmt.Fprintf(stderr, "gateway credential storage Compose project: %v\n", err)
			return 1
		}
		pool, err := effective.String("gateway.credentials.storage.pool")
		if err != nil {
			fmt.Fprintf(stderr, "gateway credential storage pool: %v\n", err)
			return 1
		}
		a := gateway.CredentialAttachment{GatewayID: id, ComposeProject: project, Pool: pool}
		if err := a.Validate(); err != nil {
			fmt.Fprintf(stderr, "gateway credential storage configuration: %v\n", err)
			return 1
		}
		if err := ensureCredentialStorage(ctx, a); err != nil {
			fmt.Fprintf(stderr, "gateway credential storage: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway credential volume %q: ready in Compose project %q (attachment unchanged; no credentials imported)\n", "aginctus-gateway-"+id+"-credentials", project)
		return 0
	}
	if len(args) == 0 || len(args) > 2 {
		printGatewayUsage(stderr)
		return 2
	}
	if len(args) == 2 && (args[0] != "remove" || args[1] != "--force") {
		printGatewayUsage(stderr)
		return 2
	}

	spec, err := gateway.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
		return 1
	}

	switch args[0] {
	case "status":
		return runGatewayStatus(ctx, args[1:], stdout, stderr, effective)
	case "remove":
		if err := spec.Remove(ctx, len(args) == 2); err != nil {
			fmt.Fprintf(stderr, "gateway cleanup: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway %q: gateway instances removed (persistent volumes retained)\n", spec.ID)
		return 0
	case "apply":
		runtimeEnvironment, err := spec.InitializeRuntimeEnvironment()
		if err != nil {
			fmt.Fprintf(stderr, "gateway initialization: %v\n", err)
			return 1
		}
		if err := spec.Deploy(ctx, runtimeEnvironment); err != nil {
			fmt.Fprintf(stderr, "gateway deployment: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway %q: started (application readiness not verified)\n", spec.ID)
		return 0
	case "validate":
		fmt.Fprintf(stdout, "gateway %q: configuration valid\n", spec.ID)
		return 0
	case "render":
		rendered, err := spec.RenderCompose()
		if err != nil {
			fmt.Fprintf(stderr, "gateway render: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, rendered)
		return 0
	case "up":
		runtimeEnvironment, err := spec.InitializeRuntimeEnvironment()
		if err != nil {
			fmt.Fprintf(stderr, "gateway initialization: %v\n", err)
			return 1
		}
		if err := spec.Deploy(ctx, runtimeEnvironment); err != nil {
			fmt.Fprintf(stderr, "gateway deployment: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway %q: started (application readiness not verified)\n", spec.ID)
		return 0
	default:
		printGatewayUsage(stderr)
		return 2
	}
}

func printGatewayUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] gateway validate")
	fmt.Fprintln(w, "       aginctus [global options] gateway render")
	fmt.Fprintln(w, "       aginctus [global options] gateway up")
	fmt.Fprintln(w, "       aginctus [global options] gateway apply")
	fmt.Fprintln(w, "       aginctus [global options] gateway remove [--force]")
	fmt.Fprintln(w, "       aginctus [global options] gateway status")
	fmt.Fprintln(w, "       aginctus [global options] gateway credential-storage ensure")
	fmt.Fprintln(w, "       aginctus [global options] gateway credential-storage attach")
	fmt.Fprintln(w, "       aginctus [global options] gateway credential-storage audit  (read-only; no authentication)")
	fmt.Fprintln(w, "       aginctus [global options] gateway provider chatgpt register [--alias=NAME] [--dry-run]")
	fmt.Fprintln(w, "       aginctus [global options] gateway provider list")
	fmt.Fprintln(w, "       aginctus [global options] gateway provider ALIAS show")
	fmt.Fprintln(w, "       aginctus [global options] gateway model NAME register [--provider=ALIAS] [--source=chatgpt/MODEL] [--dry-run]")
	fmt.Fprintln(w, "       aginctus [global options] gateway model list")
	fmt.Fprintln(w, "       aginctus [global options] gateway model NAME show")
	fmt.Fprintln(w, "       aginctus [global options] gateway model NAME render  (read-only, not activated)")
	fmt.Fprintln(w, "       aginctus [global options] gateway responses verify [--require-function-call] < stream.sse")
}

func runHerdr(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	incusClient IncusClient,
	effective *config.Config,
) int {
	if len(args) == 0 {
		return runHerdrConsole(ctx, nil, stdout, stderr, effective)
	}
	if args[0] == "authorize" {
		return runHerdrAuthorize(ctx, args[1:], stdout, stderr, effective)
	}
	if args[0] == "pin-host" {
		return runHerdrPinHost(ctx, args[1:], stdout, stderr, effective)
	}
	if args[0] == "register" {
		return runHerdrRegister(ctx, args[1:], stdout, stderr, effective)
	}
	if args[0] != "client" {
		printHerdrClientUsage(stderr)
		return 2
	}
	args = args[1:]
	if len(args) > 0 && args[0] == "status" {
		return runHerdrClientStatus(ctx, args[1:], stdout, stderr, effective)
	}
	if len(args) > 0 && (args[0] == "start" || args[0] == "stop") {
		if len(args) != 1 {
			printHerdrClientUsage(stderr)
			return 2
		}
		spec, err := herdrclient.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
			return 1
		}
		if err := transitionHerdrClient(ctx, spec, args[0]); err != nil {
			fmt.Fprintf(stderr, "Herdr client %s: %v\n", args[0], err)
			return 1
		}
		verb := "started"
		if args[0] == "stop" {
			verb = "stopped"
		}
		fmt.Fprintf(stdout, "Herdr client %q: %s\n", spec.Name, verb)
		return 0
	}
	if len(args) == 0 {
		printHerdrClientUsage(stderr)
		return 2
	}

	options, err := parseNetworkOptions(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "herdr-client options: %v\n", err)
		return 2
	}

	spec, err := herdrclient.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
		return 1
	}

	var operation string
	switch args[0] {
	case "apply", "ensure":
		operation = "upsert"
	case "remove", "teardown":
		operation = "delete"
	default:
		printHerdrClientUsage(stderr)
		return 2
	}

	err = executeHerdrClient(ctx, spec, herdrclient.Options{
		DryRun:    options.DryRun,
		Force:     options.Force,
		Operation: operation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client: %v\n", err)
		return 1
	}

	switch {
	case operation == "upsert" && options.DryRun:
		fmt.Fprintf(stdout, "Herdr client %q: would reconcile\n", spec.Name)
	case operation == "upsert":
		fmt.Fprintf(stdout, "Herdr client %q: ready\n", spec.Name)
	case options.DryRun:
		fmt.Fprintf(stdout, "Herdr client %q: would delete if present\n", spec.Name)
	default:
		fmt.Fprintf(stdout, "Herdr client %q: absent\n", spec.Name)
	}
	return 0
}

func printHerdrClientUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] herdr authorize <workload-id>")
	fmt.Fprintln(w, "       aginctus [global options] herdr pin-host <workload-id>")
	fmt.Fprintln(w, "       aginctus [global options] herdr register <workload-id>")
	fmt.Fprintln(w, "       aginctus [global options] herdr client apply [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] herdr client remove [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] herdr client status")
	fmt.Fprintln(w, "       aginctus [global options] herdr client start")
	fmt.Fprintln(w, "       aginctus [global options] herdr client stop")
}

func runWorkload(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	effective *config.Config,
) int {
	if len(args) > 1 && args[0] == "gateway" && args[1] == "provision" {
		return runWorkloadGatewayProvision(ctx, args[2:], stdout, stderr, effective)
	}
	if len(args) > 0 && args[0] == "status" {
		return runWorkloadStatus(ctx, args[1:], stdout, stderr, effective)
	}
	if len(args) > 0 && (args[0] == "ls" || args[0] == "list") {
		return runActiveWorkloadList(ctx, args[1:], stdout, stderr, effective)
	}

	if len(args) == 0 {
		printWorkloadUsage(stderr)
		return 2
	}

	// New grammar is "workload apply|remove [name]". Keep the historical
	// "workload <name> ensure|teardown" form as a compatibility alias.
	action := args[0]
	var ids []string
	var optionArgs []string
	switch action {
	case "apply", "remove", "start", "stop":
		if len(args) > 1 && !strings.HasPrefix(args[1], "--") {
			ids = []string{args[1]}
			optionArgs = args[2:]
		} else {
			ids = workloadIDs(effective)
			optionArgs = args[1:]
		}
	default:
		if len(args) < 2 || (args[1] != "ensure" && args[1] != "teardown") {
			printWorkloadUsage(stderr)
			return 2
		}
		ids = []string{args[0]}
		if args[1] == "ensure" {
			action = "apply"
		} else {
			action = "remove"
		}
		optionArgs = args[2:]
	}

	if (action == "start" || action == "stop") && len(optionArgs) != 0 {
		printWorkloadUsage(stderr)
		return 2
	}
	options, err := parseNetworkOptions(optionArgs)
	if err != nil {
		fmt.Fprintf(stderr, "workload options: %v\n", err)
		return 2
	}
	if len(ids) == 0 {
		fmt.Fprintln(stderr, "no workloads are configured")
		return 1
	}

	operation := "upsert"
	if action == "remove" {
		operation = "delete"
	}

	for _, id := range ids {
		spec, err := workload.FromConfig(effective, id)
		if err != nil {
			fmt.Fprintf(stderr, "workload %q configuration: %v\n", id, err)
			return 1
		}

		if action == "start" || action == "stop" {
			if err := transitionWorkload(ctx, spec, action); err != nil {
				fmt.Fprintf(stderr, "workload %q %s: %v\n", id, action, err)
				return 1
			}
			verb := "started"
			if action == "stop" {
				verb = "stopped"
			}
			fmt.Fprintf(stdout, "workload %q: %s\n", id, verb)
			continue
		}
		err = executeWorkload(ctx, spec, workload.Options{
			DryRun:    options.DryRun,
			Force:     options.Force,
			Operation: operation,
		})
		if err != nil {
			fmt.Fprintf(stderr, "workload %q: %v\n", id, err)
			return 1
		}

		switch {
		case operation == "upsert" && options.DryRun:
			fmt.Fprintf(stdout, "workload %q: would reconcile\n", id)
		case operation == "upsert":
			fmt.Fprintf(stdout, "workload %q: ready\n", id)
		case options.DryRun:
			fmt.Fprintf(stdout, "workload %q: would delete if present\n", id)
		default:
			fmt.Fprintf(stdout, "workload %q: absent\n", id)
		}
	}
	return 0
}

func workloadIDs(effective *config.Config) []string {
	value, ok := effective.Get("workloads")
	if !ok {
		return nil
	}
	workloads, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(workloads))
	for id := range workloads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func printWorkloadUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] workload ls")
	fmt.Fprintln(w, "       aginctus [global options] workload apply [name] [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] workload remove [name] [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] workload start [name]")
	fmt.Fprintln(w, "       aginctus [global options] workload stop [name]")
	fmt.Fprintln(w, "       aginctus [global options] workload status [name]")
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: aginctus [global options] <command>

Global options:
  --config=path=value          Override one configuration value; may be repeated
  --configure=path=value       Alias for --config
  --configuration-file=path    Load an explicit JSON configuration file

Commands:
  remove            Remove configured resources in reverse dependency order
  status            Inspect all configured resource groups
  apply             Deploy the complete configured orchestration
  apply --render    Print one deterministic JSON orchestration plan (no mutation)
  apply --from FILE Deploy a validated JSON orchestration plan
  images list|ls    List local Incus images and aliases
  images update NAME PATH   Safely import an image from a Nix result directory
  config show       Print the effective merged configuration
  config get PATH   Print one effective configuration value
  doctor [--fix]    Check Incus connectivity and HTTPS listener; --fix binds loopback
  gateway validate  Validate the AI gateway deployment configuration
  gateway apply     Reconcile the AI gateway
  gateway status    Inspect gateway project and service state
  gateway responses verify  Validate an observed Responses SSE stream offline (stdin)
  gateway remove [--force] Remove gateway instances, preserving data volumes
  gateway render    Render the AI gateway Compose model
  gateway chatgpt model render MODEL  Render native LiteLLM Responses model registration
  gateway credential-storage ensure  Create/verify dedicated unmounted gateway credential volume
  herdr                  Launch interactive Herdr in its client container
  herdr client apply     Create, reconcile, and bootstrap the Herdr client
  herdr client remove    Delete the Herdr client container
  herdr client status    Inspect the Herdr client instance
  herdr client start     Start the Herdr client
  herdr client stop      Stop the Herdr client
  network apply          Create or reconcile the configured management network
  network remove         Delete the configured management network
  network status         Inspect the configured management network
  workload ls            List active managed workloads
  workload apply [NAME]  Reconcile one workload, or all configured workloads
  workload remove [NAME] Stop and remove one workload, or all configured workloads
  workload start [NAME]  Start one or all configured workloads
  workload stop [NAME]   Stop one or all configured workloads
  workload status [NAME] Inspect one or all configured workloads
  version           Print the Aginctus CLI version
  help              Show this help`)
}
