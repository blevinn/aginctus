package gateway

import (
	"context"

	"github.com/blevinn/aginctus/internal/host/net/firewall"
)

// FirewalldExecutor remains the gateway-facing read-only query seam.
// Host command execution and its exit-status contract belong to host/net.
type FirewalldExecutor func(context.Context, ...string) (string, error)

func SystemFirewalld(ctx context.Context, args ...string) (string, error) {
	return firewall.RunFirewalld(ctx, args...)
}

const egressFirewallPolicy = firewall.EgressHTTPS
const egressFirewallZone = firewall.EgressZone
