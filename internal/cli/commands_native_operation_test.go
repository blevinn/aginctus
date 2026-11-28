package cli

import "testing"

func TestNativeOperationSelectionSeparatesComponentAuthority(t *testing.T) {
	for _, tc := range []struct {
		workload string
		herdr, gateway, recover bool
		target, inspection string
	}{
		{"worker", false, false, false, "worker", "inspect"},
		{"herdr-client", false, false, false, "herdr-client", "inspect"},
		{"gateway-credentials", false, false, true, "gateway-credentials", "inspect"},
		{"", true, false, false, "herdr-client", "herdr-inspect"},
		{"", false, true, false, "gateway-credentials", "gateway-inspect"},
		{"", true, false, true, "", ""},
		{"", false, true, true, "", ""},
		{"worker", true, false, false, "", ""},
		{"", true, true, false, "", ""},
		{"", false, false, false, "", ""},
	} {
		target, inspection, err := nativeOperationSelection(tc.workload, tc.herdr, tc.gateway, tc.recover)
		if (err != nil) != (tc.target == "") || target != tc.target || inspection != tc.inspection {
			t.Fatalf("selection mismatch for %+v: %q %q %v", tc, target, inspection, err)
		}
	}
}
