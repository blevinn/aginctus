// aginctus-testenv-sweep is only for an installed dedicated worker timer.
// It is intentionally not an agent-facing CLI or MCP operation.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/blevinn/aginctus/internal/testenv"
)

var sweepExpired = testenv.RunTrustedIncusExpirySweep

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "testenv recovery: unexpected arguments")
		return 2
	}
	options := testenv.TrustedIncusSweepOptions{
		SocketPath: getenv("AGINCTUS_TESTENV_WORKER_SOCKET"),
		StoragePool: getenv("AGINCTUS_TESTENV_WORKER_POOL"),
		PolicyKeyPath: getenv("AGINCTUS_TESTENV_POLICY_KEY_FILE"),
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	result, err := sweepExpired(ctx, options, time.Now().UTC())
	fmt.Fprintf(stdout, "testenv-recovery scanned=%d skipped=%d destroyed=%d failed=%d\n",
		result.Scanned, result.Skipped, result.Destroyed, result.Failed)
	if err != nil {
		// Never print provider diagnostics, daemon paths, run IDs or key bytes.
		fmt.Fprintln(stderr, "testenv recovery: incomplete or unverified")
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}
