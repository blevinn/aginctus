package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/lxc/incus/v7/shared/api"
)

func TestFirewallPolicyRejectsUnownedBeforeDisplay(t *testing.T) {
	old := getFirewallNetwork
	t.Cleanup(func() { getFirewallNetwork = old })
	getFirewallNetwork = func(_ context.Context, name string) (*api.Network, error) {
		return &api.Network{
			Name:       name,
			Managed:    true,
			Type:       "bridge",
			NetworkPut: api.NetworkPut{Config: map[string]string{}},
		}, nil
	}
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	cfg, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if rc := runNetworkFirewallPolicy(context.Background(), &out, &errs, cfg); rc != 1 {
		t.Fatalf("rc=%d output=%q", rc, out.String())
	}
	if out.Len() != 0 || !strings.Contains(errs.String(), "ownership mismatch") {
		t.Fatalf("out=%q err=%q", out.String(), errs.String())
	}
}
