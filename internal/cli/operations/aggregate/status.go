package aggregateops

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
)

// StatusCheck is an explicitly injected, read-only per-resource status hook.
type StatusCheck struct {
	Name string
	Run func(context.Context, []string, io.Writer, io.Writer, *config.Config) int
}

// Status buffers all output and fails atomically instead of printing a partial
// success report when a later status probe fails.
func Status(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config, checks []StatusCheck) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] status")
		return 2
	}
	var results bytes.Buffer
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
