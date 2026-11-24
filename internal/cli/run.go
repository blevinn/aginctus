package cli

import (
	"context"
	"fmt"
	"io"

	doctorops "github.com/blevinn/aginctus/internal/cli/operations/doctor"
	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/internal/workload"
	"github.com/blevinn/aginctus/pkg/client"
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
	return RunWithPlugins(ctx, args, stdout, stderr, incusClient, configLoader, nil)
}

// RunWithPlugins receives the application's already validated compiled registry.
// Compilation is not runtime enablement. Embedded callers using Run have an empty
// contract inventory; concrete product commands keep their existing paths.
func RunWithPlugins(ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, configLoader ConfigLoader, registry *plugins.Registry,
) int {
	return runBasicCobra(ctx, args, stdout, stderr, incusClient, configLoader, registry, nil, nil)
}

// RunWithLogicalWorkloads is for hosts that inject an authenticated,
// environment-owned workload reader. Passing nil deliberately fails closed.
// This is not a credentialed remote management interface.
func RunWithLogicalWorkloads(ctx context.Context,args []string,stdout,stderr io.Writer,
	incusClient IncusClient, configLoader ConfigLoader, registry *plugins.Registry,
	reader client.LogicalWorkloadReader) int {
	return runBasicCobra(ctx,args,stdout,stderr,incusClient,configLoader,registry,reader,nil)
}

// RunWithAuthorizedReaders is used only by an embedding host supplying
// independently authenticated enrollment and logical workload readers.
// A nil reader never falls back to instance labels or the local state socket.
func RunWithAuthorizedReaders(ctx context.Context,args []string,stdout,stderr io.Writer,
	incusClient IncusClient, configLoader ConfigLoader, registry *plugins.Registry,
	environments client.EnvironmentReader, workloads client.LogicalWorkloadReader) int {
	return runBasicCobra(ctx,args,stdout,stderr,incusClient,configLoader,registry,workloads,environments)
}

// RunWithStateReaders lets a trusted embedding host supply independently
// authenticated state readers. The installed CLI has no implicit state
// authority and therefore fails closed until backend integration is approved.
func RunWithStateReaders(ctx context.Context, args []string, stdout, stderr io.Writer,
	incusClient IncusClient, configLoader ConfigLoader, registry *plugins.Registry,
	configured client.ConfiguredStateReader, accepted client.AcceptedStateReader,
	diff client.ConfiguredAcceptedDiffReader) int {
	return runBasicCobraWithStateReaders(ctx, args, stdout, stderr,
		incusClient, configLoader, registry, configured, accepted, diff)
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, incusClient IncusClient) int {
	return doctorops.Run(ctx, args, stdout, stderr, incusClient, func(ctx context.Context, address string) error {
		setter, ok := incusClient.(interface {
			SetHTTPSAddress(context.Context, string) error
		})
		if !ok {
			return fmt.Errorf("Incus client does not support HTTPS listener remediation")
		}
		return setter.SetHTTPSAddress(ctx, address)
	})
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
