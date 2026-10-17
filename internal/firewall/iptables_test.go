package firewall

import (
	"strings"
	"testing"
)

func TestRenderDNSDHCPOnly(t *testing.T) {
	p := &Policy{Interface: "renamed-bridge", Address: "10.1.2.1", Subnet: "10.1.2.0/24"}
	got, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		":AGINCTUS_INPUT - [0:0]",
		"-F AGINCTUS_INPUT",
		"-i renamed-bridge -s 10.1.2.0/24 -d 10.1.2.1 -p udp --dport 53 -j ACCEPT",
		"-i renamed-bridge -s 10.1.2.0/24 -d 10.1.2.1 -p tcp --dport 53 -j ACCEPT",
		"-i renamed-bridge -p udp --sport 68 --dport 67 -j ACCEPT",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, forbidden := range []string{"FORWARD", "POSTROUTING", "MASQUERADE", "--dport 22"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("unexpected %s", forbidden)
		}
	}
}
func TestRenderClearsOnlyOwnedChain(t *testing.T) {
	got, err := Render(nil)
	if err != nil || got != "*filter\n:AGINCTUS_INPUT - [0:0]\n-F AGINCTUS_INPUT\nCOMMIT\n" {
		t.Fatalf("got %q: %v", got, err)
	}
}
func TestRenderRejectsCommandInjection(t *testing.T) {
	_, err := Render(&Policy{Interface: "bridge\n-A INPUT -j ACCEPT", Address: "10.1.2.1", Subnet: "10.1.2.0/24"})
	if err == nil {
		t.Fatal("unsafe interface admitted")
	}
}
