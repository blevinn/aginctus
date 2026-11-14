// Package plugin defines the experimental public plugin identity, discovery and
// configuration-validation boundary. It does not load plugins or authorize
// operations. Compiled implementations are trusted code, not sandboxed extensions.
//
// This API is experimental: incompatible changes may occur before it is declared
// stable. APIVersion identifies this contract independently of implementation
// versions and future orchestration or reporting wire schemas.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// APIVersion is the plugin contract supported by this package.
const APIVersion = "aginctus.dev/plugin/v1alpha1"

// ErrUnsupportedAPIVersion indicates an incompatible plugin contract.
var ErrUnsupportedAPIVersion = errors.New("unsupported plugin API version")

// CapabilityKind classifies an integration, not its deployment or permissions.
type CapabilityKind string

const (
	Agent         CapabilityKind = "agent"
	Gateway       CapabilityKind = "gateway"
	UI            CapabilityKind = "ui"
	MCP           CapabilityKind = "mcp"
	ModelProvider CapabilityKind = "model-provider"
)

// Capability describes a named contribution within one plugin. Multiple named
// capabilities may have the same kind. Discovery does not enable a capability.
type Capability struct {
	Name string
	Kind CapabilityKind
}

// Descriptor describes an implementation, not a configured instance. Names are
// case-sensitive, 1–128 ASCII characters, beginning with a letter or digit and
// otherwise containing letters, digits, periods, underscores or hyphens.
// Version is an opaque, nonsecret implementation identifier (for example a
// release version or source revision), not a compatibility or provenance proof.
// Descriptor must be available without configuration, I/O or side effects.
type Descriptor struct {
	APIVersion   string
	Name         string
	Version      string
	Capabilities []Capability
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Validate rejects incompatible contracts, invalid identities, unknown kinds
// and duplicate capability names. Capability order is preserved; it has no
// execution-order meaning. Cross-plugin registration is a separate concern.
func (d Descriptor) Validate() error {
	if d.APIVersion != APIVersion {
		return ErrUnsupportedAPIVersion
	}
	if !namePattern.MatchString(d.Name) {
		return fmt.Errorf("invalid plugin name")
	}
	if len(d.Version) == 0 || len(d.Version) > 128 {
		return fmt.Errorf("plugin version must contain 1–128 printable ASCII characters without spaces")
	}
	for _, c := range d.Version {
		if c < '!' || c > '~' {
			return fmt.Errorf("plugin version must contain 1–128 printable ASCII characters without spaces")
		}
	}
	if len(d.Capabilities) == 0 {
		return fmt.Errorf("plugin must declare at least one capability")
	}
	seen := make(map[string]bool, len(d.Capabilities))
	for i, capability := range d.Capabilities {
		if !namePattern.MatchString(capability.Name) {
			return fmt.Errorf("capability %d has an invalid name", i)
		}
		if seen[capability.Name] {
			return fmt.Errorf("capability %d duplicates a name", i)
		}
		seen[capability.Name] = true
		switch capability.Kind {
		case Agent, Gateway, UI, MCP, ModelProvider:
		default:
			return fmt.Errorf("capability %d has an unknown kind", i)
		}
	}
	return nil
}

// Plugin is the common discovery/configuration contract. Capability-specific
// contribution interfaces will be added separately; this interface does not
// imply container deployment or require a local container.
//
// ValidateConfiguration validates one nonsecret JSON configuration object using
// the plugin's schema. Implementations must reject unknown fields and invalid
// combinations, honor cancellation, and avoid I/O, resource mutation, ambient
// configuration reads and secret-bearing diagnostics. Image resolution, guest
// inspection, credentials and active probes belong to later authorized phases.
type Plugin interface {
	Descriptor() Descriptor
	ValidateConfiguration(context.Context, json.RawMessage) error
}

// Validate checks compatibility and the JSON object envelope before invoking
// plugin-specific configuration validation. It does not register or enable the
// plugin. Cancellation is checked around the callback even if an implementation
// returns nil after its context was canceled; implementations must cooperate to
// interrupt a blocking callback. Errors retain their causes for errors.Is/As.
// Plugin errors must already be safe diagnostics; this is not a secret scrubber.
func Validate(ctx context.Context, implementation Plugin, configuration json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if implementation == nil {
		return fmt.Errorf("plugin implementation is required")
	}
	if err := implementation.Descriptor().Validate(); err != nil {
		return fmt.Errorf("validate plugin descriptor: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &object); err != nil || object == nil {
		return fmt.Errorf("plugin configuration must be one JSON object")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	validationErr := implementation.ValidateConfiguration(ctx, configuration)
	if err := ctx.Err(); err != nil {
		return err
	}
	if validationErr != nil {
		return fmt.Errorf("validate plugin configuration: %w", validationErr)
	}
	return nil
}
