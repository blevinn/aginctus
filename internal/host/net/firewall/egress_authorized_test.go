package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAuthorizedEgressRejectsUnprivilegedWithoutSideEffects(t *testing.T) {
	calls := 0
	err := ReconcileAuthorizedEgress(context.Background(), false, AuthorizedEgressActions{
		EffectiveUID: func() int { return 1000 },
		Lock: func(string) (func(), error) { calls++; return func(){}, nil },
		Bridge: func() (string, error) { calls++; return "bridge", nil },
	}, func(context.Context) (EgressGrant,error) { calls++; return EgressGrant{},nil })
	if err == nil || calls != 0 { t.Fatalf("err=%v calls=%d", err, calls) }
}

func TestAuthorizedEgressRefusesMissingCapabilities(t *testing.T) {
	if err := ReconcileAuthorizedEgress(context.Background(), true, AuthorizedEgressActions{}, nil); err == nil {
		t.Fatal("missing authority accepted")
	}
	if err := ReconcileAuthorizedEgress(context.Background(), true, AuthorizedEgressActions{
		EffectiveUID: func() int { return 0 },
	}, func(context.Context)(EgressGrant,error){return EgressGrant{},nil}); err == nil {
		t.Fatal("missing lock/bridge accepted")
	}
}

func TestAuthorizedEgressRefusesUnavailableLockAndBridge(t *testing.T) {
	for _, tc := range []struct{name string; lockErr bool; bridge string; bridgeErr bool}{
		{"lock failure",true,"bridge",false},
		{"blank bridge",false,"",false},
		{"bridge error",false,"bridge",true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unlocks, discovers := 0, 0
			err := ReconcileAuthorizedEgress(context.Background(), false, AuthorizedEgressActions{
				EffectiveUID: func() int { return 0 },
				Lock: func(name string)(func(),error){
					if name != "/run/aginctus-firewall" { t.Fatalf("wrong lock %q",name) }
					if tc.lockErr { return nil, errors.New("private lock details") }
					return func(){ unlocks++ },nil
				},
				Bridge: func()(string,error){
					if tc.bridgeErr { return "",errors.New("private bridge details") }
					return tc.bridge,nil
				},
			},func(context.Context)(EgressGrant,error){ discovers++;return EgressGrant{},nil })
			if err == nil || discovers != 0 { t.Fatalf("err=%v discovery=%d",err,discovers) }
			if tc.lockErr && unlocks!=0 || !tc.lockErr && unlocks!=1 { t.Fatalf("unlocks=%d",unlocks) }
			if tc.bridgeErr && strings.Contains(err.Error(),"private bridge details") { t.Fatal("private detail leaked") }
		})
	}
}
