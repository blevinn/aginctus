package firewall

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func owned() *api.Network {
	return &api.Network{
		Name:    "renamed-bridge",
		Managed: true,
		Type:    "bridge",
		NetworkPut: api.NetworkPut{Config: map[string]string{
			"ipv4.address":           "10.1.2.1/24",
			"ipv4.nat":               "false",
			"ipv4.routing":           "false",
			"ipv6.address":           "none",
			"user.aginctus.managed":  "true",
			"user.aginctus.resource": "management-network",
		}},
	}
}

func TestDeriveOwnedBridge(t *testing.T) {
	n := owned()
	called := false
	p, err := Derive(n, "renamed-bridge", "auto", func(name string, ip net.IP) error {
		called = true
		if name != "renamed-bridge" || !ip.Equal(net.ParseIP("10.1.2.1")) {
			t.Fatal(name, ip)
		}
		return nil
	})
	if err != nil || !called || p.Interface != "renamed-bridge" || p.Address != "10.1.2.1" || p.Subnet != "10.1.2.0/24" {
		t.Fatalf("policy=%+v err=%v", p, err)
	}
}

func TestDeriveRejectsForeignAndDrift(t *testing.T) {
	cases := map[string]func(*api.Network){
		"wrong selected name": func(n *api.Network) { n.Name = "aginctus-mgmt" },
		"not managed":         func(n *api.Network) { n.Managed = false },
		"wrong type":          func(n *api.Network) { n.Type = "physical" },
		"missing marker":      func(n *api.Network) { delete(n.Config, "user.aginctus.managed") },
		"spoofed role":        func(n *api.Network) { n.Config["user.aginctus.resource"] = "egress-network" },
		"routing enabled":     func(n *api.Network) { n.Config["ipv4.routing"] = "true" },
		"nat enabled":         func(n *api.Network) { n.Config["ipv4.nat"] = "true" },
		"ipv6 enabled":        func(n *api.Network) { n.Config["ipv6.address"] = "fd42::1/64" },
		"invalid address":     func(n *api.Network) { n.Config["ipv4.address"] = "auto" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			n := owned()
			change(n)
			calls := 0
			p, err := Derive(n, "renamed-bridge", "auto", func(string, net.IP) error {
				calls++
				return nil
			})
			if err == nil || p != (Policy{}) || calls != 0 {
				t.Fatalf("p=%+v err=%v calls=%d", p, err, calls)
			}
		})
	}
	if _, err := Derive(owned(), "renamed-bridge", "10.1.3.1/24", func(string, net.IP) error {
		return nil
	}); err == nil {
		t.Fatal("configured address drift admitted")
	}
	if _, err := Derive(owned(), "renamed-bridge", "auto", func(string, net.IP) error {
		return errors.New("missing")
	}); err == nil || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("live link mismatch: %v", err)
	}
}
