package gateway

import (
	"testing"
)

func TestParseEgressDHCPRanges(t *testing.T) {
	blocked, err := parseEgressDHCPRanges("10.42.40.1/24", "10.42.40.100-10.42.40.200")
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 101 {
		t.Fatalf("got %d blocked addresses", len(blocked))
	}
	pick, err := selectEgressIPv4("10.42.40.1/24", blocked)
	if err != nil {
		t.Fatal(err)
	}
	if pick != "10.42.40.254/24" {
		t.Fatalf("unexpected candidate %s", pick)
	}
	for _, bad := range []string{
		"", "10.42.40.2", "10.42.40.2-", "10.42.40.2-10.42.41.2",
		"10.42.40.200-10.42.40.100", "2001:db8::1-2001:db8::3",
	} {
		if _, err := parseEgressDHCPRanges("10.42.40.1/24", bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
