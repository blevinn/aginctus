package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
)

// runAggregateStatus keeps status read-only and buffers all results so a
// failure in a later resource does not print a misleading partial summary.
func runAggregateStatus(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] status")
		return 2
	}
	var results bytes.Buffer
	checks := []struct {
		name string
		run  func(context.Context, []string, io.Writer, io.Writer, *config.Config) int
	}{
		{"network", runNetworkStatus},
		{"gateway", runGatewayStatus},
		{"Herdr client", runHerdrClientStatus},
		{"workloads", runWorkloadStatus},
	}
	for _, check := range checks {
		var errors bytes.Buffer
		if code := check.run(ctx, nil, &results, &errors, cfg); code != 0 {
			fmt.Fprintf(stderr, "%s status failed: %s", check.name, errors.String())
			return 1
		}
	}
	_, err := io.Copy(out, &results)
	if err != nil {
		fmt.Fprintf(stderr, "write status: %v\n", err)
		return 1
	}
	return 0
}
