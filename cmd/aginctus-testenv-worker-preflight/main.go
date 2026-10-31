// Installed operator-only read-only gate. No public CLI/MCP registration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/blevinn/aginctus/internal/testenv"
)

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer,
	preflight func(context.Context, testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error)) int {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Fprintln(stdout, "usage: aginctus-testenv-worker-preflight [--pool-fingerprint]")
		return 0
	}
	inspect := len(args) == 1 && args[0] == "--pool-fingerprint"
	if len(args) != 0 && !inspect {
		fmt.Fprintln(stderr, "usage: aginctus-testenv-worker-preflight [--pool-fingerprint]")
		return 2
	}
	r, err := preflight(ctx, testenv.TrustedIncusPreflightOptions{
		SocketPath:            getenv("AGINCTUS_TESTENV_WORKER_SOCKET"),
		PolicyKeyPath:         getenv("AGINCTUS_TESTENV_POLICY_KEY_FILE"),
		ImageFingerprint:      getenv("AGINCTUS_TESTENV_IMAGE_FINGERPRINT"),
		PoolConfigFingerprint: getenv("AGINCTUS_TESTENV_POOL_CONFIG_FINGERPRINT"),
		InspectPool:           inspect,
	})
	if err != nil {
		r.Status = "fail"
	}
	r.SchemaVersion, r.Profile = 1, "worker_preflight"
	writeErr := json.NewEncoder(stdout).Encode(r)
	if err != nil || writeErr != nil {
		fmt.Fprintln(stderr, "worker preflight incomplete or unverified")
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, testenv.RunTrustedIncusWorkerPreflight))
}
