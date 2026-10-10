package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
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
	return spec.Execute(ctx, options)
}

var executeWorkload = func(ctx context.Context, spec workload.Spec, options workload.Options) error {
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
	case "doctor":
		return runDoctor(ctx, commandArgs[1:], stdout, stderr, incusClient)
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

	spec, err := herdrclient.FromConfig(effective)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
		return 1
	}

	operation := "upsert"
	if args[0] == "teardown" {
		operation = "delete"
	} else if args[0] != "ensure" {
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
	case args[0] == "ensure" && options.DryRun:
		fmt.Fprintf(stdout, "Herdr client %q: would reconcile\n", spec.Name)
	case args[0] == "ensure":
		fmt.Fprintf(stdout, "Herdr client %q: ready\n", spec.Name)
	case options.DryRun:
		fmt.Fprintf(stdout, "Herdr client %q: would delete if present\n", spec.Name)
	default:
		fmt.Fprintf(stdout, "Herdr client %q: absent\n", spec.Name)
	}
	return 0
}

func printHerdrClientUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] herdr client ensure [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] herdr client teardown [--dry-run] [--force]")
}

func runWorkload(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	effective *config.Config,
) int {
	if len(args) < 2 {
		printWorkloadUsage(stderr)
		return 2
	}

	id := args[0]
	action := args[1]
	options, err := parseNetworkOptions(args[2:])
	if err != nil {
		fmt.Fprintf(stderr, "workload options: %v\n", err)
		return 2
	}

	spec, err := workload.FromConfig(effective, id)
	if err != nil {
		fmt.Fprintf(stderr, "workload %q configuration: %v\n", id, err)
		return 1
	}

	operation := "upsert"
	if action == "teardown" {
		operation = "delete"
	} else if action != "ensure" {
		printWorkloadUsage(stderr)
		return 2
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
	case action == "ensure" && options.DryRun:
		fmt.Fprintf(stdout, "workload %q: would reconcile\n", id)
	case action == "ensure":
		fmt.Fprintf(stdout, "workload %q: ready\n", id)
	case options.DryRun:
		fmt.Fprintf(stdout, "workload %q: would delete if present\n", id)
	default:
		fmt.Fprintf(stdout, "workload %q: absent\n", id)
	}
	return 0
}

func printWorkloadUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] workload <name> ensure [--dry-run] [--force]")
	fmt.Fprintln(w, "       aginctus [global options] workload <name> teardown [--dry-run] [--force]")
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
  doctor [--fix]    Check Incus connectivity and HTTPS listener; --fix binds loopback
  gateway validate  Validate the AI gateway deployment configuration
  gateway render    Render the AI gateway Compose model
  herdr client ensure    Create, reconcile, and bootstrap the Herdr client
  herdr client teardown  Delete the Herdr client container
  network ensure    Create or reconcile the configured management network
  network teardown  Delete the configured management network
  workload NAME ensure    Create or reconcile an agent workload
  workload NAME teardown  Delete an agent workload
  version           Print the Aginctus CLI version
  help              Show this help`)
}
