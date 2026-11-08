package gateway

import (
	"reflect"
	"testing"
)

func TestNativeTopologySeparatesServicesAndSecrets(t *testing.T) {
	s := Spec{
		ID: "gateway", Project: "gateway", Network: "aginctus-mgmt",
		EgressNetwork: "aginctus-egress", EgressIPv4Address: "10.43.0.12/24",
		EgressBridgeIPv4CIDR: "10.43.0.1/24",
		LiteLLMImage: "ghcr.io/berriai/litellm:main",
		PostgresImage: "docker.io/library/postgres:16",
	}
	got, err := s.NativeTopology()
	if err != nil { t.Fatal(err) }
	if len(got.Services) != 2 { t.Fatalf("services = %d", len(got.Services)) }
	db, app := got.Services[0], got.Services[1]
	if db.Name != "postgres" || app.Name != "litellm" { t.Fatalf("service order: %+v", got.Services) }
	if !reflect.DeepEqual(db.Networks, []NativeGatewayNetwork{{Name: s.Network}}) {
		t.Fatalf("database has unexpected network: %+v", db.Networks)
	}
	if !reflect.DeepEqual(app.Networks, []NativeGatewayNetwork{{Name:s.Network}, {Name:s.EgressNetwork, IPv4Address:s.EgressIPv4Address}}) {
		t.Fatalf("LiteLLM network attachments: %+v", app.Networks)
	}
	if db.Volume != got.DatabaseVolume || app.Volume != "" { t.Fatal("unexpected persistent storage assignment") }
	if !reflect.DeepEqual(app.DependsOn, []string{"postgres"}) { t.Fatalf("depends on: %v", app.DependsOn) }
	if len(db.SecretEnvironment) != 1 || len(app.SecretEnvironment) != 3 { t.Fatal("secret references missing") }
}
func TestNativeTopologyRejectsInvalidSpec(t *testing.T) {
	_, err := (Spec{}).NativeTopology()
	if err == nil { t.Fatal("invalid gateway specification accepted") }
}
