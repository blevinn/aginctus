package gatewayops

import (
	"context"
	"fmt"
	"io"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
)

// LifecycleActions isolate additional configuration and post-deploy SIWC
// handling from gateway orchestration, keeping command registration passive.
type LifecycleActions struct {
	ConfiguredSpec func(*config.Config) (gateway.Spec, error)
	Status func(context.Context, []string, io.Writer, io.Writer, *config.Config) int
	SyncSIWC func(context.Context, *config.Config) error
}

func ExecuteLifecycle(ctx context.Context, action string, force bool, stdout, stderr io.Writer, effective *config.Config, actions LifecycleActions) int {
	spec, err := gateway.FromConfig(effective)
	if err == nil && (action == "validate" || action == "render") {
		spec, err = actions.ConfiguredSpec(effective)
	}
	if err != nil {
		fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
		return 1
	}
	switch action {
	case "status":
		return actions.Status(ctx, nil, stdout, stderr, effective)
	case "remove":
		if err := spec.Remove(ctx, force); err != nil {
			fmt.Fprintf(stderr, "gateway cleanup: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gateway %q: gateway instances removed (persistent volumes retained)\n", spec.ID)
		return 0
	case "apply", "up":
		runtimeEnvironment, err := spec.InitializeRuntimeEnvironment()
		if err != nil {
			fmt.Fprintf(stderr, "gateway initialization: %v\n", err)
			return 1
		}
		spec, err = actions.ConfiguredSpec(effective)
		if err != nil {
			fmt.Fprintf(stderr, "gateway configuration: %v\n", err)
			return 1
		}
		if err := spec.Deploy(ctx, runtimeEnvironment); err != nil {
			fmt.Fprintf(stderr, "gateway deployment: %v\n", err)
			return 1
		}
		if spec.SIWCConfig != "" {
			if err := actions.SyncSIWC(ctx, effective); err != nil {
				fmt.Fprintf(stderr, "managed SIWC configuration: %v\n", err)
				return 1
			}
		}
		fmt.Fprintf(stdout, "gateway %q: started (application readiness not verified)\n", spec.ID)
		return 0
	case "validate":
		fmt.Fprintf(stdout, "gateway %q: configuration valid\n", spec.ID)
		return 0
	case "render":
		rendered, err := spec.RenderCompose()
		if err != nil {
			fmt.Fprintf(stderr, "gateway render: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, rendered)
		return 0
	default:
		fmt.Fprintln(stderr, "unsupported gateway lifecycle operation")
		return 2
	}
}
