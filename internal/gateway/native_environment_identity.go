package gateway

import (
	"fmt"
	"strings"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// NativeGatewayResourceIdentity identifies one environment's gateway resources
// in a shared Incus project. The IDs are derived from the immutable environment
// UUID, never from display names, ambient client configuration, or the project
// name. It is a naming contract, NOT authorization to create the resources.
type NativeGatewayResourceIdentity struct {
	Project string `json:"project"`
	EnvironmentID string `json:"environmentId"`
	PostgresInstance string `json:"postgresInstance"`
	LiteLLMInstance string `json:"litellmInstance"`
	DatabaseVolume string `json:"databaseVolume"`
}

// NativeGatewayIdentity binds a canonical environment to an independently
// verified Incus server/project selection. The caller must still authenticate
// and authorize every SDK operation and pin project-scoped accepted state.
func NativeGatewayIdentity(descriptor environment.Descriptor, observed environment.Binding) (NativeGatewayResourceIdentity,error) {
	if err:=descriptor.VerifyBinding(observed);err!=nil {
		return NativeGatewayResourceIdentity{},fmt.Errorf("gateway environment binding unverified: %w",err)
	}
	base:="ag-"+strings.ReplaceAll(descriptor.ID,"-","")
	return NativeGatewayResourceIdentity{
		Project:descriptor.Binding.Project,
		EnvironmentID:descriptor.ID,
		PostgresInstance:base+"-pg",
		LiteLLMInstance:base+"-llm",
		DatabaseVolume:base+"-pgdata",
	},nil
}

// ValidateEnvironmentIdentity confirms that a resource identity matches the
// same canonical UUID and selected project. A manually assembled identity
// cannot rename or redirect the expected PostgreSQL volume or guest.
func (id NativeGatewayResourceIdentity) ValidateEnvironmentIdentity(descriptor environment.Descriptor, observed environment.Binding) error {
	expected,err:=NativeGatewayIdentity(descriptor,observed)
	if err!=nil{return err}
	if id!=expected {
		return fmt.Errorf("gateway resource identity does not match the bound environment")
	}
	return nil
}
