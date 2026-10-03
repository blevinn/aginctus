package cli

import (
	"context"
	"fmt"
	"io"
)

const Version = "0.0.0-dev"

type IncusClient interface {
	Version(context.Context) (string, error)
	CheckDaemon(context.Context) error
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
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runDoctor(ctx context.Context, stdout, stderr io.Writer, incusClient IncusClient) int {
	version, err := incusClient.Version(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "incus client: unavailable: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "incus client: %s\n", version)

	if err := incusClient.CheckDaemon(ctx); err != nil {
		fmt.Fprintf(stderr, "incus daemon: unreachable: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "incus daemon: reachable")

	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: aginctus <command>

Commands:
  doctor   Check local Incus client and daemon connectivity
  version  Print the Aginctus CLI version
  help     Show this help`)
}
