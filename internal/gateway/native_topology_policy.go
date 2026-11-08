package gateway

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"reflect"
	"regexp"
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
		p.ManagementNetwork == "" || p.DatabaseVolume != "gateway-postgres" ||
		p.EgressNetwork == p.ManagementNetwork {
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
		if len(app.Networks) != 2 {
			return fmt.Errorf("native gateway egress requires one explicit LiteLLM attachment")
		}
		address, err := netip.ParsePrefix(app.Networks[1].IPv4Address)
		if err != nil || !address.Addr().Is4() || address.Bits() < 16 || address.Bits() > 30 ||
			!address.Addr().IsGlobalUnicast() || address.Addr() == address.Masked().Addr() ||
			address.Addr() == lastIPv4Address(address.Masked()) {
			return fmt.Errorf("native gateway egress requires a usable host IPv4 attachment")
		}
		// Fixed IPv4 addresses are retained for later actor-authorized device
		// reconciliation, never used to create host networks implicitly.
		appNetworks = append(appNetworks, NativeGatewayNetwork{
			Name: p.EgressNetwork, IPv4Address: fmt.Sprintf("%s/%d", address.Addr(), address.Bits()),
		})
	} else if len(app.Networks) != 1 {
		return fmt.Errorf("native gateway without egress permits only management attachment")
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

func lastIPv4Address(prefix netip.Prefix) netip.Addr {
	address := prefix.Masked().Addr().As4()
	hostBits := uint(32 - prefix.Bits())
	hostMask := uint32(0)
	if hostBits != 0 {
		hostMask = (uint32(1) << hostBits) - 1
	}
	binary.BigEndian.PutUint32(address[:], binary.BigEndian.Uint32(address[:])|hostMask)
	return netip.AddrFrom4(address)
}
