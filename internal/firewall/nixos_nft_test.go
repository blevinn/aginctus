package firewall

import (
	"strings"
	"testing"
)

func TestNixosNftUsesOnlyInputAllowChildChain(t *testing.T) {
	p := &Policy{Interface: "arbitrary-bridge", Address: "10.77.3.1", Subnet: "10.77.3.0/24"}
	got, err := RenderNixosNft(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"flush chain inet nixos-fw aginctus_dns_input",
		"ct state invalid return",
		"iifname \"arbitrary-bridge\" ip saddr 10.77.3.0/24 ip daddr 10.77.3.1 udp dport 53 accept",
		"iifname \"arbitrary-bridge\" ip saddr 10.77.3.0/24 ip daddr 10.77.3.1 tcp dport 53 accept",
		"iifname \"arbitrary-bridge\" udp sport 68 udp dport 67 accept",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from %q", want, got)
		}
	}
	for _, bad := range []string{"hook input", "FORWARD", "POSTROUTING", "masquerade", "udp dport 22"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe rule %q", bad)
		}
	}
}
func TestNixosNftFailClosedAndInjection(t *testing.T) {
	got, err := RenderNixosNft(nil)
	if err != nil || got != "flush chain inet nixos-fw aginctus_dns_input\n" {
		t.Fatalf("flush-only=%q err=%v", got, err)
	}
	_, err = RenderNixosNft(&Policy{Interface: "x\"; flush table inet nixos-fw", Address: "10.77.3.1", Subnet: "10.77.3.0/24"})
	if err == nil {
		t.Fatal("admitted injected bridge name")
	}
}
