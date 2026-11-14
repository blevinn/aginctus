package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	networkcmd "github.com/blevinn/aginctus/internal/cli/commands/network"
	networkops "github.com/blevinn/aginctus/internal/cli/operations/network"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/firewall"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	"github.com/spf13/cobra"
)

// Bind the independent Cobra parser to the existing network implementation.
func addNetworkCommands(root *cobra.Command, stdout, stderr io.Writer,
	load func() (*config.Config, error), status *int,
) {
	networkcmd.Register(root, stdout, stderr, load, status, networkcmd.Handlers{
		Operation: func(ctx context.Context, operation string, dryRun, force bool, out, errOut io.Writer, cfg *config.Config) int {
			return executeNetworkOperation(ctx, operation, mutationOptions{DryRun: dryRun, Force: force}, out, errOut, cfg)
		},
		Status: func(ctx context.Context, out, errOut io.Writer, cfg *config.Config) int {
			return runNetworkStatus(ctx, nil, out, errOut, cfg)
		},
		FirewallPolicy:    runNetworkFirewallPolicy,
		FirewallReconcile: runNetworkFirewallReconcile,
		FirewallEgress:    runNetworkFirewallEgress,
	})
}

// Incus inspection is kept at the CLI composition edge for existing
// fail-closed tests and for explicit network authorization.
var getFirewallNetwork = func(ctx context.Context, name string) (*api.Network, error) {
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return nil, fmt.Errorf("connect to Incus: %w", err)
	}
	network, _, err := server.GetNetwork(name)
	if err != nil {
		return nil, fmt.Errorf("get configured Incus network: %w", err)
	}
	return network, nil
}

func deriveFirewallPolicy(ctx context.Context, cfg *config.Config) (firewall.Policy, error) {
	return networkops.DeriveFirewallPolicy(ctx, cfg, getFirewallNetwork)
}

func runNetworkFirewallPolicy(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	return networkops.ReportFirewallPolicy(ctx, stdout, stderr, cfg, getFirewallNetwork)
}

// Root bindings supply the existing testable host command and authority seams.
var firewallEffectiveUID = os.Geteuid
var discoverFirewallPolicy = deriveFirewallPolicy
var firewallCommand = func(ctx context.Context, input string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func firewallActions() networkops.FirewallActions {
	return networkops.FirewallActions{
		EffectiveUID: firewallEffectiveUID,
		Command:      firewallCommand,
		Discover:     discoverFirewallPolicy,
	}
}

func runNetworkFirewallReconcile(ctx context.Context, stdout, stderr io.Writer, cfg *config.Config) int {
	return networkops.Reconcile(ctx, stdout, stderr, cfg, firewallActions())
}

func selectFirewallBackend(ruleset, requested string) (string, error) {
	return networkops.SelectFirewallBackend(ruleset, requested)
}

func hasFirewalldDropTarget(details string) bool {
	return networkops.HasFirewalldDropTarget(details)
}
