package gatewaycontrol

import "github.com/blevinn/aginctus/internal/gateway"

// OpenCodeGoPlugin shares the audited source/endpoint contract with legacy
// registrations. Rendering does not acquire credentials or enable forwarding.
func OpenCodeGoPlugin() Plugin {
	return registrationPlugin("opencode-go", gateway.OpenCodeGoModelRegistration)
}
