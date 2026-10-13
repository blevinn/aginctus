package credentialdaemon

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/blevinn/aginctus/internal/orchestration"
 "github.com/blevinn/aginctus/internal/statevolume"
)

func sample()Spec{return Spec{GatewayID:"local",Name:"aginctus-gateway-local-credentials",ImageAlias:"aginctus-oauth-gateway",Pool:"default",ManagementNetwork:"aginctus-mgmt",EgressNetwork:"aginctus-egress"}}
func TestPlanExclusiveCredentialMount(t *testing.T){
 p,err:=sample().Plan();if err!=nil{t.Fatal(err)}
 if len(p.Steps)!=1{t.Fatalf("steps=%d",len(p.Steps))}
 doc:=string(p.Steps[0].Configuration)
 for _,s:=range []string{"aginctus-gateway-local-credentials","/var/lib/aginctus/oauth","aginctus-mgmt","aginctus-egress","credential-daemon"}{if !strings.Contains(doc,s){t.Fatalf("missing %s: %s",s,doc)}}
 for _,s:=range []string{"litellm","postgres","refresh_token","access_token"}{if strings.Contains(doc,s){t.Fatalf("unexpected %s",s)}}
}
func TestInvalidNetworkingFailsClosed(t *testing.T){
 s:=sample();s.EgressNetwork=s.ManagementNetwork
 if _,err:=s.Plan();err==nil{t.Fatal("accepted shared NIC")}
}
func TestStorageFailureStopsDeployment(t *testing.T){
 originalEnsure,originalDeploy:=ensureStorage,deployPlan
 t.Cleanup(func(){ensureStorage,deployPlan=originalEnsure,originalDeploy})
 ensureStorage=func(context.Context,statevolume.GatewayCredentials)error{return errors.New("foreign volume")}
 called:=false
 deployPlan=func(context.Context,orchestration.Plan)error{called=true;return nil}
 if err:=sample().Apply(context.Background());err==nil||called{t.Fatalf("fail closed: %v deployed=%v",err,called)}
}
