package herdr

import (
	"context"
	"fmt"
	"io"
	"strings"

	workloadops "github.com/blevinn/aginctus/internal/cli/operations/workload"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
)

// ConsoleActions checks the exact legacy owned/running Herdr client before
// attaching the host terminal to the preexisting guest console command.
type ConsoleActions struct {
	Inspect func(context.Context) ([]workloadops.InspectedInstance, error)
	Find func([]workloadops.InspectedInstance, string, string, string, string) (string, error)
	Launch func(context.Context, string, io.Reader, io.Writer, io.Writer) error
}

func RunConsole(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer, cfg *config.Config, actions ConsoleActions) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: aginctus [global options] herdr")
		return 2
	}
	spec, err := herdrclient.FromConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client configuration: %v\n", err)
		return 1
	}
	instances, err := actions.Inspect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Herdr client inspection: %v\n", err)
		return 1
	}
	state, err := actions.Find(instances, spec.Name, "infrastructure", "herdr-client", "")
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
	if err := actions.Launch(ctx, spec.Name, in, out, stderr); err != nil {
		fmt.Fprintf(stderr, "Herdr console: %v\n", err)
		return 1
	}
	return 0
}
