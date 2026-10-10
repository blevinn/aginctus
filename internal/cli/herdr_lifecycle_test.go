package cli

import (
 "bytes"
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/blevinn/aginctus/internal/config"
 "github.com/blevinn/aginctus/internal/herdrclient"
)

func TestHerdrLifecycleTransitions(t *testing.T) {
 oldInspect,oldIncus:=inspectIncusInstances,runWorkloadIncus
 t.Cleanup(func(){inspectIncusInstances=oldInspect;runWorkloadIncus=oldIncus})
 spec:=herdrclient.Spec{Name:"aginctus-herdr"}
 var calls []string
 runWorkloadIncus=func(_ context.Context,args ...string)error{calls=append(calls,strings.Join(args," "));return nil}
 for _,tc:=range []struct{action,state string; expected int}{
  {"stop","Running",1},{"stop","Stopped",1},{"start","Stopped",2},{"start","Running",2},
 }{
  inspectIncusInstances=func(context.Context)([]byte,error){
   return []byte(`[{"name":"aginctus-herdr","status":"`+tc.state+`","config":{"user.aginctus.managed":"true","user.aginctus.resource":"infrastructure","user.aginctus.role":"herdr-client"}}]`),nil
  }
  if err:=transitionHerdrClient(context.Background(),spec,tc.action);err!=nil{t.Fatalf("%s %s: %v",tc.action,tc.state,err)}
  if len(calls)!=tc.expected{t.Fatalf("commands after %s %s: %v",tc.action,tc.state,calls)}
 }
 if calls[0]!="stop aginctus-herdr --project default" || calls[1]!="start aginctus-herdr --project default"{t.Fatalf("calls=%v",calls)}
}

func TestHerdrLifecycleForeignInstanceCannotBeStopped(t *testing.T){
 oldInspect,oldIncus:=inspectIncusInstances,runWorkloadIncus
 t.Cleanup(func(){inspectIncusInstances=oldInspect;runWorkloadIncus=oldIncus})
 inspectIncusInstances=func(context.Context)([]byte,error){return []byte(`[{"name":"aginctus-herdr","status":"Running","config":{}}]`),nil}
 runWorkloadIncus=func(context.Context,...string)error{t.Fatal("foreign instance mutated");return nil}
 if err:=transitionHerdrClient(context.Background(),herdrclient.Spec{Name:"aginctus-herdr"},"stop");err==nil{t.Fatal("expected ownership error")}
}

func TestHerdrLifecycleStopFailureBlocksDelete(t *testing.T){
 old:=transitionHerdrClient
 t.Cleanup(func(){transitionHerdrClient=old})
 transitionHerdrClient=func(context.Context,herdrclient.Spec,string)error{return errors.New("stop failed")}
 err:=executeHerdrClient(context.Background(),herdrclient.Spec{Name:"aginctus-herdr"},herdrclient.Options{Operation:"delete"})
 if err==nil || !strings.Contains(err.Error(),"stop failed"){t.Fatalf("error=%v",err)}
}

func TestHerdrLifecycleCommands(t *testing.T){
 old:=transitionHerdrClient
 t.Cleanup(func(){transitionHerdrClient=old})
 var calls []string
 transitionHerdrClient=func(_ context.Context,_ herdrclient.Spec,action string)error{calls=append(calls,action);return nil}
 loader:=config.NewLoader();loader.SystemPath="";loader.UserPath="";loader.Environment=nil
 for _,action:=range []string{"start","stop"} {
  var out,errOut bytes.Buffer
  code:=Run(context.Background(),[]string{"herdr","client",action},&out,&errOut,&fakeIncusClient{},loader)
  if code!=0 {t.Fatalf("action %s: code=%d err=%q",action,code,errOut.String())}
 }
 if strings.Join(calls,",")!="start,stop"{t.Fatalf("calls=%v",calls)}
}
