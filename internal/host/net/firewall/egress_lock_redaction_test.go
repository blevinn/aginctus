package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEgressLockErrorsDoNotRevealHostDetails(t *testing.T) {
	discoveries:=0
	err:=ReconcileAuthorizedEgress(context.Background(),false,AuthorizedEgressActions{
		EffectiveUID:func()int{return 0},
		Lock:func(string)(func(),error){return nil,errors.New("private filesystem path and owner")},
		Bridge:func()(string,error){t.Fatal("bridge read before lock");return "",nil},
	},func(context.Context)(EgressGrant,error){discoveries++;return EgressGrant{},nil})
	if err==nil||strings.Contains(err.Error(),"private filesystem")||discoveries!=0{
		t.Fatalf("unsafe lock refusal: %v, discoveries=%d",err,discoveries)
	}
}
