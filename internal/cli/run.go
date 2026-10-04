package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/incus"
)

const Version = "0.0.0-dev"

type IncusClient interface {
	ServerVersion(context.Context) (string, error)
	EnsureManagementNetwork(context.Context, incus.ManagementNetworkSpec) (bool, error)
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
		return runNetwork(ctx, commandArgs[1:], stdout, stderr, incusClient, effective)
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
	incusClient IncusClient,
	effective *config.Config,
) int {
	if len(args) != 1 || args[0] != "ensure" {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] network ensure")
		return 2
	}

	spec, err := managementNetworkSpec(effective)
	if err != nil {
		fmt.Fprintf(stderr, "management network configuration: %v\n", err)
		return 1
	}

	created, err := incusClient.EnsureManagementNetwork(ctx, spec)
	if err != nil {
		fmt.Fprintf(stderr, "management network: %v\n", err)
		return 1
	}

	if created {
		fmt.Fprintf(stdout, "management network %q: created\n", spec.Name)
	} else {
		fmt.Fprintf(stdout, "management network %q: ready\n", spec.Name)
	}
	return 0
}

func managementNetworkSpec(effective *config.Config) (incus.ManagementNetworkSpec, error) {
	name, err := effective.String("incus.management.network.name")
	if err != nil {
		return incus.ManagementNetworkSpec{}, err
	}
	ipv4Address, err := effective.String("incus.management.network.ipv4.address")
	if err != nil {
		return incus.ManagementNetworkSpec{}, err
	}
	ipv4NAT, err := effective.Bool("incus.management.network.ipv4.nat")
	if err != nil {
		return incus.ManagementNetworkSpec{}, err
	}
	ipv4Routing, err := effective.Bool("incus.management.network.ipv4.routing")
	if err != nil {
		return incus.ManagementNetworkSpec{}, err
	}
	ipv6Address, err := effective.String("incus.management.network.ipv6.address")
	if err != nil {
		return incus.ManagementNetworkSpec{}, err
	}

	if name == "" {
		return incus.ManagementNetworkSpec{}, fmt.Errorf("configuration key %q must not be empty", "incus.management.network.name")
	}

	return incus.ManagementNetworkSpec{
		Name: name,
		IPv4Address: ipv4Address,
		IPv4NAT: ipv4NAT,
		IPv4Routing: ipv4Routing,
		IPv6Address: ipv6Address,
	}, nil
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
  network ensure    Create or verify the configured management network
  version           Print the Aginctus CLI version
  help              Show this help`)
}
