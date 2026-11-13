package gateway

import (
	"testing"
)

func TestNativeGatewayInstancePlanIsolatedAndDetached(t *testing.T) {
	p:=nativePinnedFixture()
	d,err:=PlanNativeGatewayDevices(p,"rootpool","datapool")
	if err!=nil {t.Fatal(err)}
	pair,err:=PlanNativeGatewayInstances(p,d,"pg-image","llm-image")
	if err!=nil {t.Fatal(err)}
	if pair.Postgres.Name!="postgres-1" || pair.LiteLLM.Name!="litellm-1" ||
		pair.Postgres.Project!=p.Project || pair.LiteLLM.Project!=p.Project {
		t.Fatalf("unexpected identities: %+v",pair)
	}
	if len(pair.Postgres.DependsOn)!=0 || len(pair.LiteLLM.DependsOn)!=1 ||
		pair.LiteLLM.DependsOn[0]!="postgres" {t.Fatal("incorrect startup dependency")}
	if _,ok:=pair.Postgres.Devices["egress"];ok {t.Fatal("database received egress")}
	if _,ok:=pair.LiteLLM.Devices["database"];ok {t.Fatal("application received database")}
	if pair.Postgres.Devices["database"]["path"]!="/var/lib/postgresql/data" {t.Fatal("data mount missing")}
	// Changes to the result must not modify the input intent maps/slices.
	pair.Postgres.Devices["root"]["pool"]="other"
	pair.LiteLLM.DependsOn[0]="other"
	pair.LiteLLM.SecretEnvironment[0]="other"
	if d.Postgres["root"]["pool"]!="rootpool" || p.Services[1].DependsOn[0]!="postgres" ||
		p.Services[1].SecretEnvironment[0]!="AGINCTUS_GATEWAY_POSTGRES_PASSWORD" {t.Fatal("result aliases input")}
}
func TestNativeGatewayInstancePlanRejectsUntrustedDevices(t *testing.T) {
	p:=nativePinnedFixture()
	fresh:=func() NativeGatewayDevices {
		d,err:=PlanNativeGatewayDevices(p,"rootpool","datapool")
		if err!=nil {t.Fatal(err)}
		return d
	}
	cases:=map[string]func(*NativeGatewayDevices) {
		"foreign project":func(d *NativeGatewayDevices){d.Project="other"},
		"unexpected host disk":func(d *NativeGatewayDevices){d.LiteLLM["host"] = map[string]string{"type":"disk","source":"/etc","path":"/mnt"}},
		"stolen database":func(d *NativeGatewayDevices){d.Postgres["database"]["source"]="foreign"},
		"application egress drift":func(d *NativeGatewayDevices){d.LiteLLM["egress"]["network"]="unapproved"},
		"management gateway":func(d *NativeGatewayDevices){d.Postgres["management"]["ipv4.gateway"]="auto"},
	}
	for name,edit:=range cases {t.Run(name,func(t *testing.T){
		d:=fresh();edit(&d)
		if _,err:=PlanNativeGatewayInstances(p,d,"pg-image","llm-image");err==nil {t.Fatal("accepted unexpected device")}
	})}
	d:=fresh()
	if _,err:=PlanNativeGatewayInstances(p,d,"same","same");err==nil {t.Fatal("accepted colliding aliases")}
}
