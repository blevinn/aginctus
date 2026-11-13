package gateway

import "testing"

func TestNativeGatewayDevicePlanPreservesIsolation(t *testing.T) {
	p:=nativePinnedFixture()
	got,err:=PlanNativeGatewayDevices(p,"rootpool","datapool")
	if err!=nil {t.Fatal(err)}
	if got.Project!=p.Project {t.Fatal("project scope lost")}
	if len(got.Postgres)!=3 || len(got.LiteLLM)!=3 {t.Fatalf("unexpected devices: %+v",got)}
	if _,ok:=got.Postgres["egress"];ok {t.Fatal("database has egress")}
	if _,ok:=got.LiteLLM["database"];ok {t.Fatal("LiteLLM received database mount")}
	if got.Postgres["database"]["source"]!=p.DatabaseVolume ||
		got.Postgres["database"]["pool"]!="datapool" ||
		got.Postgres["database"]["path"]!="/var/lib/postgresql/data" {
		t.Fatal("persistent volume mismatched")
	}
	if got.Postgres["root"]["pool"]!="rootpool" || got.LiteLLM["root"]["pool"]!="rootpool" {t.Fatal("root pool mismatch")}
	if got.LiteLLM["egress"]["ipv4.address"]!="10.43.0.12" {t.Fatal("incorrect pinned egress address")}
	for _,v:=range []map[string]map[string]string{got.Postgres,got.LiteLLM} {
		if v["management"]["network"]!="mgmt" || v["management"]["ipv4.gateway"]!="none" {
			t.Fatal("management network lost no-gateway constraint")
		}
	}
}
func TestNativeGatewayDevicePlanWithoutEgress(t *testing.T) {
	p:=nativePinnedFixture()
	p.EgressNetwork=""
	p.Services[1].Networks=p.Services[1].Networks[:1]
	got,err:=PlanNativeGatewayDevices(p,"pool","pool")
	if err!=nil {t.Fatal(err)}
	if len(got.Postgres)!=3 || len(got.LiteLLM)!=2 {t.Fatalf("unexpected no-egress devices: %+v",got)}
}
func TestNativeGatewayDevicePlanRejectsUnsafeInputs(t *testing.T) {
	p:=nativePinnedFixture()
	for _,pool:=range []string{"","default/pool","../../root","pool with spaces"} {
		if _,err:=PlanNativeGatewayDevices(p,pool,"data");err==nil {t.Fatalf("accepted root pool %q",pool)}
		if _,err:=PlanNativeGatewayDevices(p,"root",pool);err==nil {t.Fatalf("accepted data pool %q",pool)}
	}
	p.Services[0].Networks=append(p.Services[0].Networks,NativeGatewayNetwork{Name:"egress"})
	if _,err:=PlanNativeGatewayDevices(p,"root","data");err==nil {t.Fatal("database egress accepted")}
}

func TestNativeGatewayDevicePlanRejectsMalformedProject(t *testing.T) {
	p:=nativePinnedFixture()
	p.Project="../other"
	if _,err:=PlanNativeGatewayDevices(p,"root","data");err==nil {
		t.Fatal("unsafe project accepted")
	}
}
