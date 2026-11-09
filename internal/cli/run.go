package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"

	doctorops "github.com/blevinn/aginctus/internal/cli/operations/doctor"
	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/workload"
)

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
	return runBasicCobra(ctx, args, stdout, stderr, incusClient, configLoader)
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
	return doctorops.Run(ctx, args, stdout, stderr, incusClient, setIncusHTTPSAddress)
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

// executeNetworkOperation is the typed compatibility binding from Cobra.
func executeNetworkOperation(ctx context.Context, operation string, options mutationOptions, stdout, stderr io.Writer, effective *config.Config) int {
	return networkops.Execute(ctx, operation, networkops.Options{DryRun: options.DryRun, Force: options.Force}, stdout, stderr, effective, executeManagementNetwork)
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

var ensureCredentialStorage = gateway.EnsureCredentialVolume
var attachGatewayCredentials = gateway.AttachCredentialVolume
var auditGatewayCredentials = gateway.AuditCredentialAttachment


// These bindings keep existing CLI dependency-injection seams intact.
func transitionHerdrClientCommand(ctx context.Context, action string, stdout, stderr io.Writer, effective *config.Config) int {
	return herdrops.Transition(ctx, action, stdout, stderr, effective, transitionHerdrClient)
}

func executeHerdrClientOperation(ctx context.Context, operation string, options mutationOptions, stdout, stderr io.Writer, effective *config.Config) int {
	return herdrops.Execute(ctx, operation, herdrops.Options{DryRun: options.DryRun, Force: options.Force}, stdout, stderr, effective, executeHerdrClient)
}

// executeConfiguredWorkloads takes parsed actions and selected IDs, with an
// empty selection preserving v0.1's 'all configured workloads' semantics.
func executeConfiguredWorkloads(ctx context.Context, action string, ids []string, options mutationOptions, stdout, stderr io.Writer, effective *config.Config) int {
	if len(ids) == 0 {
		ids = workloadIDs(effective)
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
