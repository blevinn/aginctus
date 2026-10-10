package cli

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "os/exec"
 "strings"

 "github.com/blevinn/aginctus/internal/config"
 "github.com/blevinn/aginctus/internal/herdrclient"
 "github.com/blevinn/aginctus/internal/workload"
)

var inspectIncusInstances = func(ctx context.Context) ([]byte,error) {
 cmd:=exec.CommandContext(ctx,"incus","list","--all-projects","--format=json")
 data,err:=cmd.CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("incus list: %w: %s",err,strings.TrimSpace(string(data)))}
 return data,nil
}

type inspectedInstance struct {
 Name string `json:"name"`
 Status string `json:"status"`
 Config map[string]string `json:"config"`
 Project string `json:"project"`
}

func findManagedInstance(instances []inspectedInstance,name,resource,role,id string) (string,error) {
 for _,instance:=range instances {
  if instance.Name!=name {continue}
  if instance.Config["user.aginctus.managed"]!="true" ||
   instance.Config["user.aginctus.resource"]!=resource ||
   (role!="" && instance.Config["user.aginctus.role"]!=role) ||
   (id!="" && instance.Config["user.aginctus.workload"]!=id) {
    return "",fmt.Errorf("instance %q exists but ownership markers do not match",name)
  }
  if instance.Project!="" && instance.Project!="default" {
   return "",fmt.Errorf("instance %q exists in unexpected project %q",name,instance.Project)
  }
  return instance.Status,nil
 }
 return "",nil
}

func statusInstances(ctx context.Context) ([]inspectedInstance,error) {
 data,err:=inspectIncusInstances(ctx)
 if err!=nil{return nil,err}
 var instances []inspectedInstance
 if err:=json.Unmarshal(data,&instances);err!=nil{return nil,fmt.Errorf("decode Incus instances: %w",err)}
 return instances,nil
}

func runHerdrClientStatus(ctx context.Context,args []string,out,stderr io.Writer,cfg *config.Config) int {
 if len(args)!=0 {fmt.Fprintln(stderr,"Usage: aginctus herdr client status");return 2}
 spec,err:=herdrclient.FromConfig(cfg)
 if err!=nil {fmt.Fprintf(stderr,"Herdr client configuration: %v\n",err);return 1}
 instances,err:=statusInstances(ctx)
 if err==nil {
  var status string
  status,err=findManagedInstance(instances,spec.Name,"infrastructure","herdr-client","")
  if err==nil {if status=="" {status="absent"};fmt.Fprintf(out,"Herdr client %q: %s\n",spec.Name,status);return 0}
 }
 fmt.Fprintf(stderr,"Herdr client status: %v\n",err);return 1
}

func runWorkloadStatus(ctx context.Context,args []string,out,stderr io.Writer,cfg *config.Config) int {
 if len(args)>1 {fmt.Fprintln(stderr,"Usage: aginctus workload status [name]");return 2}
 ids:=workloadIDs(cfg)
 if len(args)==1 {ids=[]string{args[0]}}
 specs:=make([]workload.Spec,0,len(ids))
 for _,id:=range ids {spec,err:=workload.FromConfig(cfg,id);if err!=nil {fmt.Fprintf(stderr,"workload %q configuration: %v\n",id,err);return 1};specs=append(specs,spec)}
 instances,err:=statusInstances(ctx)
 if err!=nil {fmt.Fprintf(stderr,"workload status: %v\n",err);return 1}
 // Check all requested names before presenting any results.
 results:=make([]string,0,len(specs))
 for _,spec:=range specs {
  state,err:=findManagedInstance(instances,spec.Name,"workload","agent-workload",spec.ID)
  if err!=nil {fmt.Fprintf(stderr,"workload %q status: %v\n",spec.ID,err);return 1}
  if state=="" {state="absent"}
  results=append(results,fmt.Sprintf("workload %q: %s",spec.ID,state))
 }
 for _,result:=range results {fmt.Fprintln(out,result)}
 return 0
}
