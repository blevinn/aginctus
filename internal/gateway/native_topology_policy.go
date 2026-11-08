package gateway

import (
	"fmt"
	"regexp"
	"reflect"
)

// Native image acquisition must resolve public tags to platform-specific
// manifest digests before a resource write. This gate does not perform that
// resolution and must not be used as authorization to import or adopt images.
var nativeGatewayDigest = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[a-f0-9]{64}$`)

// ValidateExecutionShape rejects dangerous drift from the deliberately narrow
// gateway topology before a future native executor can use it. The caller must
// separately verify accepted revision, resource ownership, project authority,
// credentials, image identity/aliases, volume state, and mutation-time races.
func (p NativeGatewayTopology) ValidateExecutionShape() error {
	if p.Deployment == "" || p.Project == "" || p.Project == "default" ||
		p.ManagementNetwork == "" || p.DatabaseVolume != "gateway-postgres" {
		return fmt.Errorf("native gateway requires an explicit nondefault project and managed database volume")
	}
	if len(p.Services) != 2 {
		return fmt.Errorf("native gateway requires exactly two services")
	}
	db, app := p.Services[0], p.Services[1]
	if db.Name != "postgres" || app.Name != "litellm" ||
		!nativeGatewayDigest.MatchString(db.ImageReference) ||
		!nativeGatewayDigest.MatchString(app.ImageReference) {
		return fmt.Errorf("native gateway requires ordered postgres/litellm services with digest-pinned images")
	}
	management := []NativeGatewayNetwork{{Name: p.ManagementNetwork}}
	if !reflect.DeepEqual(db.Networks, management) ||
		db.Volume != p.DatabaseVolume || len(db.DependsOn) != 0 ||
		!reflect.DeepEqual(db.SecretEnvironment, []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}) {
		return fmt.Errorf("native gateway database isolation or storage contract mismatch")
	}
	appNetworks := append([]NativeGatewayNetwork(nil), management...)
	if p.EgressNetwork != "" {
		if p.EgressNetwork == p.ManagementNetwork {
			return fmt.Errorf("native gateway egress and management networks must differ")
		}
		appNetworks = append(appNetworks, NativeGatewayNetwork{Name:p.EgressNetwork})
		// Fixed IPv4 addresses are retained here for later actor-authorized
		// device reconciliation, never used to create host networks implicitly.
		if len(app.Networks) == 2 {
			appNetworks[1].IPv4Address = app.Networks[1].IPv4Address
		}
	}
	if !reflect.DeepEqual(app.Networks, appNetworks) || app.Volume != "" ||
		!reflect.DeepEqual(app.DependsOn, []string{"postgres"}) ||
		!reflect.DeepEqual(app.SecretEnvironment, []string{
			"AGINCTUS_GATEWAY_POSTGRES_PASSWORD", "AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY",
		}) {
		return fmt.Errorf("native gateway application attachment or secret-reference contract mismatch")
	}
	return nil
}
