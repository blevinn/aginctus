// Command aginctus-project-state-read serves an explicitly provisioned,
// project-owned accepted-state volume through an independently authenticated,
// read-only mTLS endpoint. It never provisions Incus infrastructure or grants
// a root/user proxy. It MUST run nonroot inside the dedicated service instance.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

func main() {
	path := os.Getenv("AGINCTUS_PROJECT_STATE_READ_SERVICE_CONFIG")
	config, err := environment.ReadProjectStateReadServiceConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "project-state read service configuration unavailable")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := environment.ServeMountedProjectStateReads(ctx, config); err != nil {
		fmt.Fprintln(os.Stderr, "restricted project-state service unavailable")
		os.Exit(1)
	}
}
