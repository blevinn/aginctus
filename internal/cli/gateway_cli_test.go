package cli

import (
	"context"
	"io"

	"github.com/blevinn/aginctus/internal/config"
)

// runGateway keeps domain tests concise while exercising the public Cobra
// command tree rather than the removed positional gateway dispatcher.
func runGateway(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	incusClient IncusClient,
	effective *config.Config,
) int {
	return Run(ctx, append([]string{"gateway"}, args...), stdout, stderr, incusClient, &fakeConfigLoader{cfg: effective})
}
