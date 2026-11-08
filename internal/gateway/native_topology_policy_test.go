package gateway

import (
	"strings"
	"testing"
)

func nativePinnedFixture() NativeGatewayTopology {
	const digest = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return NativeGatewayTopology{
		Deployment: "gw", Project: "gw", ManagementNetwork: "mgmt",
		EgressNetwork: "egress", DatabaseVolume: "gateway-postgres",
		Services: []NativeGatewayService{
			{Name:"postgres",ImageReference:"docker.io/library/postgres@"+digest,
				Networks:[]NativeGatewayNetwork{{Name:"mgmt"}},Volume:"gateway-postgres",
				SecretEnvironment:[]string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"}},
			{Name:"litellm",ImageReference:"ghcr.io/berriai/litellm@"+digest,
				Networks:[]NativeGatewayNetwork{{Name:"mgmt"},{Name:"egress",IPv4Address:"10.43.0.12/24"}},
				DependsOn:[]string{"postgres"},
				SecretEnvironment:[]string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD","AGINCTUS_GATEWAY_MASTER_KEY","AGINCTUS_GATEWAY_SALT_KEY"}},
		},
	}
}

func TestNativeGatewayExecutionShape(t *testing.T) {
	p := nativePinnedFixture()
	if err := p.ValidateExecutionShape(); err != nil { t.Fatal(err) }
	tests := map[string]func(*NativeGatewayTopology){
		"ambient project": func(p *NativeGatewayTopology){p.Project="default"},
		"unpinned tag": func(p *NativeGatewayTopology){p.Services[1].ImageReference="ghcr.io/berriai/litellm:main"},
		"extra service": func(p *NativeGatewayTopology){p.Services=append(p.Services,NativeGatewayService{Name:"extra"})},
		"database egress": func(p *NativeGatewayTopology){p.Services[0].Networks=append(p.Services[0].Networks,NativeGatewayNetwork{Name:"egress"})},
		"database volume removed": func(p *NativeGatewayTopology){p.Services[0].Volume=""},
		"application volume": func(p *NativeGatewayTopology){p.Services[1].Volume="gateway-postgres"},
		"missing dependency": func(p *NativeGatewayTopology){p.Services[1].DependsOn=nil},
		"surprise secret": func(p *NativeGatewayTopology){p.Services[1].SecretEnvironment=append(p.Services[1].SecretEnvironment,"OTHER")},
		"network alias": func(p *NativeGatewayTopology){p.EgressNetwork="mgmt"},
	}
	for name, change := range tests {
		t.Run(name,func(t *testing.T){
			p:=nativePinnedFixture()
			change(&p)
			if err:=p.ValidateExecutionShape();err==nil {t.Fatal("invalid native gateway shape accepted")}
		})
	}
	// The policy error must never contain potentially sensitive image references.
	p=nativePinnedFixture()
	p.Services[1].ImageReference="user:password@registry.example.invalid/litellm:main"
	if err:=p.ValidateExecutionShape();err==nil || strings.Contains(err.Error(),"password") {
		t.Fatalf("unexpected diagnostic: %v",err)
	}
}
