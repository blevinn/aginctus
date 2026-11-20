package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestVerifiedEgressRouteRefusesMissingCapabilities(t *testing.T) {
	if _,_,err:=VerifiedEgressRoute(context.Background(),EgressRouteActions{},"uplink","egress","mgmt",false);err==nil{t.Fatal("accepted nil observations")}
}

func TestVerifiedEgressRouteBlocksGuestUplinkAndZoneDrift(t *testing.T) {
	const rules=`[{"priority":0,"src":"all","table":"local"},{"priority":32766,"src":"all","table":"main"},{"priority":32767,"src":"all","table":"default"}]`
	for _,tc:=range []struct{name,iface,zone string; fail bool}{
		{"egress bridge","egress","uplink",false},
		{"management bridge","mgmt","uplink",false},
		{"foreign zone","eth0","trusted",false},
		{"zone unavailable","eth0","",true},
		{"allowed uplink","eth0","uplink",false},
	} {
		t.Run(tc.name,func(t *testing.T){
			var got []string
			actions:=EgressRouteActions{
				Command:func(_ context.Context,_ string,cmd string,args ...string)(string,error){
					if cmd!="ip"{t.Fatalf("unexpected %s",cmd)}
					got=append(got,strings.Join(args," "))
					if len(args)>=3 && args[2]=="rule"{return rules,nil}
					return `[{"dst":"default","dev":"`+tc.iface+`","table":"main"}]`,nil
				},
				EffectiveZone:func(_ context.Context,iface string)(string,error){
					if iface!=tc.iface{t.Fatalf("unexpected interface %q",iface)}
					if tc.fail{return "",errors.New("zone unavailable")}
					return tc.zone,nil
				},
			}
			iface,_,err:=VerifiedEgressRoute(context.Background(),actions,"uplink","egress","mgmt",false)
			if tc.name=="allowed uplink"{
				if err!=nil||iface!="eth0"{t.Fatalf("iface=%q err=%v",iface,err)}
			}else if err==nil {t.Fatalf("accepted %s",tc.name)}
			if len(got)!=2{t.Fatalf("unexpected routing observations: %#v",got)}
		})
	}
}
