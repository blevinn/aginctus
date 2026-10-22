package gateway

import (
	"fmt"
	"net/netip"
	"strings"
)

// parseEgressDHCPRanges expands deliberately bounded Incus IPv4 DHCP ranges.
// Failure is safer than trusting incomplete DHCP inventory.
func parseEgressDHCPRanges(bridgeCIDR, ranges string) ([]netip.Addr, error) {
	bridge, err := netip.ParsePrefix(bridgeCIDR)
	if err != nil || !bridge.Addr().Is4() {
		return nil, fmt.Errorf("invalid IPv4 egress bridge prefix")
	}
	subnet := bridge.Masked()
	var blocked []netip.Addr
	for _, raw := range strings.Split(ranges, ",") {
		fields := strings.Split(strings.TrimSpace(raw), "-")
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid egress DHCP range")
		}
		start, e1 := netip.ParseAddr(strings.TrimSpace(fields[0]))
		end, e2 := netip.ParseAddr(strings.TrimSpace(fields[1]))
		if e1 != nil || e2 != nil || !start.Is4() || !end.Is4() ||
			!subnet.Contains(start) || !subnet.Contains(end) || start.Compare(end) > 0 {
			return nil, fmt.Errorf("egress DHCP range lies outside the bridge or is malformed")
		}
		// A /16 is the largest subnet supported by the selector; use that
		// same cap to keep malformed input from exhausting memory.
		if len(blocked) > 65536 {
			return nil, fmt.Errorf("egress DHCP ranges are too large")
		}
		for addr := start; ; addr = addr.Next() {
			blocked = append(blocked, addr)
			if addr == end {
				break
			}
			if len(blocked) > 65536 {
				return nil, fmt.Errorf("egress DHCP ranges are too large")
			}
		}
	}
	return blocked, nil
}
