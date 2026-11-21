package gateway

import (
	"fmt"
	"strings"
)

// validateNativeGatewayPairOwner binds exact service identities to one
// immutable environment. This is an identity predicate, not a writer grant.
// Empty environment IDs are retained for historical (unactivated) fixtures.
func validateNativeGatewayPairOwner(pair NativeGatewayInstancePair) error {
	pg,app:=pair.Postgres,pair.LiteLLM
	if pg.Project=="" || pg.Project!=app.Project || pg.Role!="postgres" ||
		app.Role!="litellm" || pg.ImageAlias=="" || pg.ImageAlias==app.ImageAlias ||
		pg.EnvironmentID!=app.EnvironmentID {
		return fmt.Errorf("gateway instance owner or service identity mismatch")
	}
	if pg.EnvironmentID=="" {
		if pg.Name!="postgres-1" || app.Name!="litellm-1" {
			return fmt.Errorf("unbound gateway must retain legacy instance identities")
		}
		return nil
	}
	if !gatewayEnvironmentIDPattern.MatchString(pg.EnvironmentID) {
		return fmt.Errorf("gateway environment UUID invalid")
	}
	base:="ag-"+strings.ReplaceAll(pg.EnvironmentID,"-","")
	if pg.Name!=base+"-pg" || app.Name!=base+"-llm" {
		return fmt.Errorf("gateway instances do not belong to selected environment")
	}
	return nil
}

func gatewayOwnerMatches(config map[string]string,environmentID string) bool {
	if environmentID=="" {return config["user.aginctus.environment-id"]==""}
	return config["user.aginctus.environment-id"]==environmentID
}
