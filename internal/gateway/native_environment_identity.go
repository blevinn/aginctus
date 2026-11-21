package gateway

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

var gatewayEnvironmentIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// NativeGatewayResourceIdentity identifies one environment's gateway resources
// in a shared Incus project. The IDs are derived from the immutable environment
// UUID, never from display names, ambient client configuration, or the project
// name. It is a naming contract, NOT authorization to create the resources.
type NativeGatewayResourceIdentity struct {
	Project          string `json:"project"`
	EnvironmentID    string `json:"environmentId"`
	PostgresInstance string `json:"postgresInstance"`
	LiteLLMInstance  string `json:"litellmInstance"`
	DatabaseVolume   string `json:"databaseVolume"`
}

// NativeGatewayIdentity binds a canonical environment to an independently
// verified Incus server/project selection. The caller must still authenticate
// and authorize every SDK operation and pin project-scoped accepted state.
func NativeGatewayIdentity(descriptor environment.Descriptor, observed environment.Binding) (NativeGatewayResourceIdentity, error) {
	if err := descriptor.VerifyBinding(observed); err != nil {
		return NativeGatewayResourceIdentity{}, fmt.Errorf("gateway environment binding unverified: %w", err)
	}
	base := "ag-" + strings.ReplaceAll(descriptor.ID, "-", "")
	return NativeGatewayResourceIdentity{
		Project:          descriptor.Binding.Project,
		EnvironmentID:    descriptor.ID,
		PostgresInstance: base + "-pg",
		LiteLLMInstance:  base + "-llm",
		DatabaseVolume:   base + "-pgdata",
	}, nil
}

// BindNativeGatewayTopology derives an environment-owned topology from the
// existing validated service graph. The project and deployment cannot be
// silently rebound by a second caller; the descriptor is independently verified
// before assigning resource names. The result remains a configured candidate.
func BindNativeGatewayTopology(top NativeGatewayTopology, descriptor environment.Descriptor, observed environment.Binding) (NativeGatewayTopology, error) {
	id, err := NativeGatewayIdentity(descriptor, observed)
	if err != nil {
		return NativeGatewayTopology{}, err
	}
	if top.EnvironmentID != "" || top.Project != id.Project || top.DatabaseVolume != "gateway-postgres" {
		return NativeGatewayTopology{}, fmt.Errorf("gateway topology must be an unbound matching project candidate")
	}
	if err := top.ValidateExecutionShape(); err != nil {
		return NativeGatewayTopology{}, err
	}
	bound := top
	bound.EnvironmentID = id.EnvironmentID
	bound.DatabaseVolume = id.DatabaseVolume
	bound.Services = append([]NativeGatewayService(nil), top.Services...)
	bound.Services[0].Volume = id.DatabaseVolume
	if err := bound.ValidateExecutionShape(); err != nil {
		return NativeGatewayTopology{}, err
	}
	return bound, nil
}

// ValidateEnvironmentIdentity confirms that a resource identity matches the
// same canonical UUID and selected project. A manually assembled identity
// cannot rename or redirect the expected PostgreSQL volume or guest.
func (id NativeGatewayResourceIdentity) ValidateEnvironmentIdentity(descriptor environment.Descriptor, observed environment.Binding) error {
	expected, err := NativeGatewayIdentity(descriptor, observed)
	if err != nil {
		return err
	}
	if id != expected {
		return fmt.Errorf("gateway resource identity does not match the bound environment")
	}
	return nil
}
