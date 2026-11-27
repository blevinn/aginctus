package gateway

import (
	"testing"
)

func TestProtectedGatewayAllowlistExact(t *testing.T) {
	for _, tc := range []struct {
		raw string
		want []string
		ok bool
	}{
		{"pool", []string{"pool", "pool"}, true},
		{"egress, mgmt", []string{"mgmt", "egress"}, true},
		{"mgmt,other", []string{"mgmt"}, false},
		{"mgmt,mgmt", []string{"mgmt"}, false},
		{"", []string{"mgmt"}, false},
	} {
		if exactGatewayAllowlist(tc.raw, tc.want) != tc.ok {
			t.Fatalf("unexpected allowlist result for %q", tc.raw)
		}
	}
}
