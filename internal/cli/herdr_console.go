package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
)

// herdrConsoleCommand aligns the CLI with the headless server systemd unit.
// Incus exec does not inherit the unit environment; without these paths Herdr
// can attach to a different root-owned session instead of the managed server.
func herdrConsoleCommand(ctx context.Context, instance string) *exec.Cmd {
	return exec.CommandContext(ctx, "incus", "exec", instance, "--project", "default", "--",
		"env", "XDG_CONFIG_HOME=/var/lib/herdr/config", "XDG_RUNTIME_DIR=/run/herdr",
		"XDG_STATE_HOME=/var/lib/herdr", "herdr")
}

// launchHerdrConsole runs an interactive command with the host terminal
// attached. No shell is involved; the client identity stays in the guest.
var launchHerdrConsole = func(ctx context.Context, instance string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := herdrConsoleCommand(ctx, instance)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("incus exec Herdr: %w", err)
	}
	return nil
}

func runHerdrConsole(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] herdr")
		return 2
	}
	spec, err := herdrclient.FromConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
		return 1
	}
	instances, err := statusInstances(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client inspection: %v\n", err)
		return 1
	}
	state, err := findManagedInstance(instances, spec.Name, "infrastructure", "herdr-client", "")
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client ownership: %v\n", err)
		return 1
	}
	if !strings.EqualFold(state, "running") {
		if state == "" {
			state = "absent"
		}
		fmt.Fprintf(stderr, "Herdr client %q is %s; run 'aginctus herdr client apply' and 'aginctus herdr client start' before launching\n", spec.Name, state)
		return 1
	}
	if err := launchHerdrConsole(ctx, spec.Name, os.Stdin, out, stderr); err != nil {
		fmt.Fprintf(stderr, "Herdr console: %v\n", err)
		return 1
	}
	return 0
}
