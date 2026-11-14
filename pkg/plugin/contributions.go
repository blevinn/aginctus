package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blevinn/aginctus/pkg/orchestration"
)

// TargetContributor is an optional extension to discovery/configuration. Targets
// describes desired contributions from the supplied configuration, without I/O,
// secret access, ambient configuration reads or mutation. Implementations must
// honor cancellation and return stable local IDs for equivalent configuration.
// An empty contribution is valid; a target can also have no local containers.
// Host-side registry assembly qualifies IDs and validates the full dependency
// graph; this interface neither selects instances nor authorizes execution.
type TargetContributor interface {
	Plugin
	Targets(context.Context, json.RawMessage) ([]orchestration.Target, error)
}

// Contribute validates configuration before invoking Targets, then checks local
// declarations, duplicate target IDs and membership in advertised capabilities.
// Missing/cyclic graph dependencies, image resolution, runtime criteria and
// write-time policy checks remain host responsibilities. Errors must already be
// safe diagnostics. No raw internal driver configuration is part of this API.
func Contribute(ctx context.Context, implementation TargetContributor, configuration json.RawMessage) ([]orchestration.Target, error) {
	if err := Validate(ctx, implementation, configuration); err != nil {
		return nil, err
	}
	targets, err := implementation.Targets(ctx, configuration)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, fmt.Errorf("contribute plugin targets: %w", err)
	}
	capabilities := make(map[string]bool)
	for _, capability := range implementation.Descriptor().Capabilities {
		capabilities[capability.Name] = true
	}
	seen := make(map[string]bool, len(targets))
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := target.Validate(); err != nil {
			return nil, fmt.Errorf("contributed target %d: %w", i, err)
		}
		if seen[target.ID] {
			return nil, fmt.Errorf("duplicate contributed target ID")
		}
		seen[target.ID] = true
		if !capabilities[target.Capability] {
			return nil, fmt.Errorf("target references an unadvertised capability")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}
