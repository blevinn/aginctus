package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/managementnetwork"
)

const Version = "0.0.0-dev"

type IncusClient interface {
	ServerVersion(context.Context) (string, error)
	EnsureHerdrClient(context.Context, incus.HerdrClientSpec, incus.MutationOptions) (incus.EnsureResult, error)
	TeardownHerdrClient(context.Context, string, incus.MutationOptions) (incus.TeardownResult, error)
}

type ConfigLoader interface {
	Load(config.Options) (*config.Config, error)
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
	case "doctor":
		return runDoctor(ctx, stdout, stderr, incusClient)
	case "config":
		return runConfig(commandArgs[1:], stdout, stderr, effective)
	case "network":
		return runNetwork(ctx, commandArgs[1:], stdout, stderr, effective)
	case "gateway":
		return runGateway(ctx, commandArgs[1:], stdout, stderr, incusClient, effective)
	case "herdr":
		return runHerdr(ctx, commandArgs[1:], stdout, stderr, incusClient, effective)
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

func runDoctor(ctx context.Context, stdout, stderr io.Writer, incusClient IncusClient) int {
	version, err := incusClient.ServerVersion(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "incus daemon: unreachable: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "incus daemon: reachable (%s)\n", version)
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

	operation := "upsert"
	if action == "teardown" {
		operation = "delete"
	} else if action != "ensure" {
		printNetworkUsage(stderr)
		return 2
	}

	err = spec.Execute(ctx, managementnetwork.Options{
		DryRun:    options.DryRun,
		Force:     options.Force,
		Operation: operation,
	})
	if err != nil {
		fmt.Fprintf(stderr, "management network: %v\n", err)
		return 1
	}

	switch {
	case action == "ensure" && options.DryRun:
		fmt.Fprintf(stdout, "management network %q: would reconcile\n", spec.Name)
	case action == "ensure":
		fmt.Fprintf(stdout, "management network %q: ready\n", spec.Name)
	case options.DryRun:
		fmt.Fprintf(stdout, "management network %q: would delete if present\n", spec.Name)
	default:
		fmt.Fprintf(stdout, "management network %q: absent\n", spec.Name)
	}
	return 0
}

func parseNetworkOptions(args []string) (incus.MutationOptions, error) {
	var options incus.MutationOptions
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			options.DryRun = true
		case "--force":
			options.Force = true
		default:
			return incus.MutationOptions{}, fmt.Errorf("unknown option %q", arg)
		}
	}
	return options, nil
}

func printNetworkUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] network ensure [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] network teardown [--dry-run] [--force]")
}

func runGateway(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	incusClient IncusClient,
	effective *config.Config,
) int {
	if len(args) != 1 {
		printGatewayUsage(stderr)
		return 2
	}

	spec, err := gateway.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
		return 1
	}

	switch args[0] {
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
		fmt.Fprintf(stdout, "gateway %q: ready\n", spec.ID)
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
}

func runHerdr(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	incusClient IncusClient,
	effective *config.Config,
) int {
	if len(args) == 0 || args[0] != "client" {
		printHerdrClientUsage(stderr)
		return 2
	}
	args = args[1:]
	if len(args) == 0 {
		printHerdrClientUsage(stderr)
		return 2
	}

	options, err := parseNetworkOptions(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "herdr-client options: %v\n", err)
		return 2
	}

	switch args[0] {
	case "ensure":
		networkSpec, err := managementnetwork.FromConfig(effective)
		if err != nil {
			fmt.Fprintf(stderr, "management network configuration: %v\n", err)
			return 1
		}
		if err := networkSpec.Execute(ctx, managementnetwork.Options{
			DryRun: options.DryRun,
			Force:  options.Force,
		}); err != nil {
			fmt.Fprintf(stderr, "management network: %v\n", err)
			return 1
		}

		spec, err := herdrClientSpec(effective)
		if err != nil {
			fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
			return 1
		}
		result, err := incusClient.EnsureHerdrClient(ctx, spec, options)
		if err != nil {
			fmt.Fprintf(stderr, "Herdr client: %v\n", err)
			return 1
		}

		switch {
		case result.DryRun && result.Created:
			fmt.Fprintf(stdout, "Herdr client %q: would create\n", spec.Name)
		case result.DryRun && result.Updated:
			fmt.Fprintf(stdout, "Herdr client %q: would update\n", spec.Name)
		case result.Created:
			fmt.Fprintf(stdout, "Herdr client %q: created\n", spec.Name)
		case result.Updated:
			fmt.Fprintf(stdout, "Herdr client %q: updated\n", spec.Name)
		default:
			fmt.Fprintf(stdout, "Herdr client %q: ready\n", spec.Name)
		}
		return 0

	case "teardown":
		name, err := effective.String("infrastructure.herdr.name")
		if err != nil {
			fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
			return 1
		}
		result, err := incusClient.TeardownHerdrClient(ctx, name, options)
		if err != nil {
			fmt.Fprintf(stderr, "Herdr client: %v\n", err)
			return 1
		}
		switch {
		case result.DryRun && result.Deleted:
			fmt.Fprintf(stdout, "Herdr client %q: would delete\n", name)
		case result.Deleted:
			fmt.Fprintf(stdout, "Herdr client %q: deleted\n", name)
		default:
			fmt.Fprintf(stdout, "Herdr client %q: absent\n", name)
		}
		return 0

	default:
		printHerdrClientUsage(stderr)
		return 2
	}
}

func herdrClientSpec(effective *config.Config) (incus.HerdrClientSpec, error) {
	name, err := effective.String("infrastructure.herdr.name")
	if err != nil {
		return incus.HerdrClientSpec{}, err
	}
	alias, err := effective.String("infrastructure.herdr.image.alias")
	if err != nil {
		return incus.HerdrClientSpec{}, err
	}
	pool, err := effective.String("infrastructure.herdr.storage.pool")
	if err != nil {
		return incus.HerdrClientSpec{}, err
	}
	network, err := effective.String("incus.management.network.name")
	if err != nil {
		return incus.HerdrClientSpec{}, err
	}
	if name == "" || alias == "" || pool == "" || network == "" {
		return incus.HerdrClientSpec{}, fmt.Errorf("Herdr client name, image alias, storage pool, and management network must not be empty")
	}

	return incus.HerdrClientSpec{
		Name:              name,
		ImageAlias:        alias,
		StoragePool:       pool,
		ManagementNetwork: network,
	}, nil
}

func printHerdrClientUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] herdr client ensure [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] herdr client teardown [--dry-run] [--force]")
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: aginctus [global options] <command>

Global options:
  --config=path=value          Override one configuration value; may be repeated
  --configure=path=value       Alias for --config
  --configuration-file=path    Load an explicit JSON configuration file

Commands:
  config show       Print the effective merged configuration
  config get PATH   Print one effective configuration value
  doctor            Check local Incus daemon connectivity
  gateway validate  Validate the AI gateway deployment configuration
  gateway render    Render the AI gateway Compose model
  herdr client ensure    Create, reconcile, and bootstrap the Herdr client
  herdr client teardown  Delete the Herdr client container
  network ensure    Create or reconcile the configured management network
  network teardown  Delete the configured management network
  version           Print the Aginctus CLI version
  help              Show this help`)
}
