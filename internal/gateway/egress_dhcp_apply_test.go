package gateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type fakeEgressEditor struct {
	network *api.Network
	etag    string
	updates int
	last    api.NetworkPut
	fail    error
}

func (f *fakeEgressEditor) GetNetwork(_ string) (*api.Network, string, error) {
	return f.network, f.etag, nil
}
func (f *fakeEgressEditor) UpdateNetwork(_ string, put api.NetworkPut, etag string) error {
	if etag != f.etag {
		return fmt.Errorf("etag mismatch")
	}
	f.updates++
	f.last = put
	return f.fail
}
func TestApplyEgressDHCPMigration(t *testing.T) {
	spec := egressTestSpec()
	spec.EgressIPv4Address = "10.42.40.25/24"
	editor := &fakeEgressEditor{
		etag: "test-etag",
		network: &api.Network{
			Name: "aginctus-egress", Type: "bridge", Managed: true,
			NetworkPut: api.NetworkPut{Config: api.ConfigMap{
				"user.aginctus.managed":  "true",
				"user.aginctus.resource": "gateway-egress-network",
				"ipv4.address":           "10.42.40.1/24",
				"ipv4.nat":               "true", "ipv4.routing": "true",
				"ipv6.address": "none",
			}},
		},
	}
	query := func(_ context.Context, args ...string) ([]byte, error) {
		switch args[len(args)-1] {
		case "/1.0/networks/aginctus-egress":
			return []byte(`{"name":"aginctus-egress","type":"bridge","managed":true,"config":{"user.aginctus.managed":"true","user.aginctus.resource":"gateway-egress-network","ipv4.address":"10.42.40.1/24","ipv4.nat":"true","ipv4.routing":"true","ipv6.address":"none"}}`), nil
		case "/1.0/networks/aginctus-egress/leases":
			return []byte(`[{"address":"10.42.40.110"}]`), nil
		default:
			return nil, fmt.Errorf("unknown query")
		}
	}
	ranges := "10.42.40.100-10.42.40.199"
	if err := applyEgressDHCPMigration(context.Background(), spec, ranges, query, editor); err != nil {
		t.Fatal(err)
	}
	if editor.updates != 1 || editor.last.Config["ipv4.dhcp.ranges"] != ranges ||
		editor.last.Config["ipv4.address"] != "10.42.40.1/24" {
		t.Fatalf("expected single narrow bridge config update, got %+v", editor)
	}
	editor.updates = 0
	editor.etag = ""
	if err := applyEgressDHCPMigration(context.Background(), spec, ranges, query, editor); err == nil || editor.updates != 0 {
		t.Fatal("expected missing ETag to fail closed")
	}
}
