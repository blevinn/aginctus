package firewall

import (
	"strings"
	"testing"
)

func TestFirewalldRules(t *testing.T) {
	p := &Policy{Interface: "custom-bridge", Address: "10.20.30.1", Subnet: "10.20.30.0/24"}
	got, err := FirewalldRules(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected DNS UDP/TCP and DHCP, got %v", got)
	}
	for _, want := range []string{
		`source address="10.20.30.0/24" destination address="10.20.30.1" port port="53" protocol="udp"`,
		`source address="10.20.30.0/24" destination address="10.20.30.1" port port="53" protocol="tcp"`,
		`port port="67" protocol="udp" accept`,
	} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Errorf("missing %q: %v", want, got)
		}
	}
	for _, rule := range got {
		if strings.Contains(rule, "dport 22") || strings.Contains(rule, "masquerade") || strings.Contains(rule, "forward") {
			t.Fatalf("unexpected broad permission: %s", rule)
		}
	}
}

func TestFirewalldRulesDisownedOrInvalid(t *testing.T) {
	empty, err := FirewalldRules(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("disowned network must produce no exceptions: %v, %v", empty, err)
	}
	for _, p := range []Policy{
		{Interface: "bad\"\nadd-rule", Address: "10.20.30.1", Subnet: "10.20.30.0/24"},
		{Interface: "bridge0", Address: "10.20.31.1", Subnet: "10.20.30.0/24"},
		{Interface: "bridge0", Address: "not-an-ip", Subnet: "10.20.30.0/24"},
	} {
		if _, err := FirewalldRules(&p); err == nil {
			t.Fatalf("invalid policy admitted: %+v", p)
		}
	}
}
