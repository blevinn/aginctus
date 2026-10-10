package cli

import (
 "bytes"
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/blevinn/aginctus/internal/config"
 "github.com/blevinn/aginctus/internal/workload"
)

func TestWorkloadLifecycleTransitions(t *testing.T) {
 oldInspect,oldRun:=inspectIncusInstances,runWorkloadIncus
 t.Cleanup(func(){inspectIncusInstances=oldInspect;runWorkloadIncus=oldRun})
 spec:=workload.Spec{ID:"dev",Name:"aginctus-dev"}
 var actions []string
 runWorkloadIncus=func(_ context.Context,args ...string)error {actions=append(actions,strings.Join(args," "));return nil}
 for _,tc:=range []struct{action,state string; wantCalls int; fail bool}{
  {"stop","Running",1,false},{"stop","Stopped",1,false},{"start","Stopped",2,false},{"start","Running",2,false},
 }{
  inspectIncusInstances=func(context.Context)([]byte,error){
   return []byte(`[{"name":"aginctus-dev","status":"`+tc.state+`","config":{"user.aginctus.managed":"true","user.aginctus.resource":"workload","user.aginctus.role":"agent-workload","user.aginctus.workload":"dev"}}]`),nil
  }
  if err:=transitionWorkload(context.Background(),spec,tc.action);(err!=nil)!=tc.fail{t.Fatalf("%s %s: %v",tc.action,tc.state,err)}
  if len(actions)!=tc.wantCalls{t.Fatalf("commands after %s %s: %v",tc.action,tc.state,actions)}
 }
 if actions[0]!="stop aginctus-dev --project default" || actions[1]!="start aginctus-dev --project default"{t.Fatalf("commands: %v",actions)}
}
func TestWorkloadLifecycleRejectsForeignInstance(t *testing.T) {
 oldInspect,oldRun:=inspectIncusInstances,runWorkloadIncus
 t.Cleanup(func(){inspectIncusInstances=oldInspect;runWorkloadIncus=oldRun})
 inspectIncusInstances=func(context.Context)([]byte,error){return []byte(`[{"name":"aginctus-dev","status":"Running","config":{}}]`),nil}
 runWorkloadIncus=func(context.Context,...string)error{t.Fatal("unexpected Incus mutation");return nil}
 if err:=transitionWorkload(context.Background(),workload.Spec{ID:"dev",Name:"aginctus-dev"},"stop");err==nil{t.Fatal("expected ownership failure")}
}
func TestWorkloadCLIStartStop(t *testing.T) {
 old:=transitionWorkload;t.Cleanup(func(){transitionWorkload=old})
 var actions []string
 transitionWorkload=func(_ context.Context,s workload.Spec,action string)error{actions=append(actions,s.ID+":"+action);return nil}
 loader:=config.NewLoader();loader.SystemPath="";loader.UserPath="";loader.Environment=nil
 for _,action:=range []string{"start","stop"}{
  var out,errOut bytes.Buffer
  code:=Run(context.Background(),[]string{"workload",action,"dev"},&out,&errOut,&fakeIncusClient{},loader)
  if code!=0 || !strings.Contains(out.String(),map[string]string{"start":"started","stop":"stopped"}[action]){t.Fatalf("%s: code=%d output=%q error=%q",action,code,out.String(),errOut.String())}
 }
 if got:=strings.Join(actions,",");got!="dev:start,dev:stop"{t.Fatalf("actions=%s",got)}
}
func TestWorkloadStopFailurePreventsDeletion(t *testing.T) {
 old:=transitionWorkload;t.Cleanup(func(){transitionWorkload=old})
 transitionWorkload=func(context.Context,workload.Spec,string)error{return errors.New("stop failed")}
 err:=executeWorkload(context.Background(),workload.Spec{ID:"dev",Name:"aginctus-dev"},workload.Options{Operation:"delete"})
 if err==nil || !strings.Contains(err.Error(),"stop failed"){t.Fatalf("error=%v",err)}
}
