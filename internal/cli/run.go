package cli

import (
	"context"
	"fmt"
	"io"
)

const Version = "0.0.0-dev"

type IncusClient interface {
	ServerVersion(context.Context) (string, error)
	EnsureManagementNetwork(context.Context) (bool, error)
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, incusClient IncusClient) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version", "--version":
		fmt.Fprintf(stdout, "aginctus %s\n", Version)
		return 0
	case "doctor":
		return runDoctor(ctx, stdout, stderr, incusClient)
	case "network":
		return runNetwork(ctx, args[1:], stdout, stderr, incusClient)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
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

func runNetwork(ctx context.Context, args []string, stdout, stderr io.Writer, incusClient IncusClient) int {
	if len(args) != 1 || args[0] != "ensure" {
		fmt.Fprintln(stderr, "Usage: aginctus network ensure")
		return 2
	}

	created, err := incusClient.EnsureManagementNetwork(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "management network: %v\n", err)
		return 1
	}

	if created {
		fmt.Fprintln(stdout, "management network: created")
	} else {
		fmt.Fprintln(stdout, "management network: ready")
	}

	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: aginctus <command>

Commands:
  doctor          Check local Incus daemon connectivity
  network ensure  Create or verify the Aginctus management network
  version         Print the Aginctus CLI version
  help            Show this help`)
}
