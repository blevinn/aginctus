package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	doctorops "github.com/blevinn/aginctus/internal/cli/operations/doctor"
	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
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

// executeNetworkOperation is the typed compatibility binding from Cobra.
func executeNetworkOperation(ctx context.Context, operation string, options mutationOptions, stdout, stderr io.Writer, effective *config.Config) int {
	return networkops.Execute(ctx, operation, networkops.Options{DryRun: options.DryRun, Force: options.Force}, stdout, stderr, effective, executeManagementNetwork)
}

type mutationOptions struct {
	DryRun bool
	Force  bool
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

// The CLI root injects its testable domain mutation seams.
func executeConfiguredWorkloads(ctx context.Context, action string, ids []string, options mutationOptions, stdout, stderr io.Writer, effective *config.Config) int {
	return workloadops.Configured(ctx, action, ids, workloadops.Options{DryRun: options.DryRun, Force: options.Force}, stdout, stderr, effective, transitionWorkload, executeWorkload)
}

// Used by legacy aggregate and workload status paths until those orchestration
// consumers move to their own operation packages.
func workloadIDs(effective *config.Config) []string {
	return workloadops.IDs(effective)
}
