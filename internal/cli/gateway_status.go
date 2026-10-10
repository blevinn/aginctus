package cli

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "os/exec"
 "strings"

 "github.com/blevinn/aginctus/internal/config"
 "github.com/blevinn/aginctus/internal/gateway"
)

var inspectGatewayProjects = func(ctx context.Context) ([]byte,error) {
 cmd:=exec.CommandContext(ctx,"incus","project","list","--format=json")
 data,err:=cmd.CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("incus project list: %w: %s",err,strings.TrimSpace(string(data)))}
 return data,nil
}
type statusProject struct {
 Name string `json:"name"`
 Config map[string]string `json:"config"`
}
func runGatewayStatus(ctx context.Context,args []string,out,stderr io.Writer,cfg *config.Config) int {
 if len(args)!=0 {fmt.Fprintln(stderr,"Usage: aginctus gateway status");return 2}
 spec,err:=gateway.FromConfig(cfg)
 if err!=nil {fmt.Fprintf(stderr,"gateway configuration: %v\n",err);return 1}
 raw,err:=inspectGatewayProjects(ctx)
 if err!=nil {fmt.Fprintf(stderr,"gateway status: %v\n",err);return 1}
 var projects []statusProject
 if err:=json.Unmarshal(raw,&projects);err!=nil{fmt.Fprintf(stderr,"gateway status: decode projects: %v\n",err);return 1}
 for _,p:=range projects {
  if p.Name!=spec.Project {continue}
  if p.Config["user.aginctus.managed"]!="true" || p.Config["user.aginctus.resource"]!="compose-project" || p.Config["user.aginctus.deployment"]!=spec.Project {
   fmt.Fprintf(stderr,"gateway project %q exists but ownership markers do not match\n",spec.Project);return 1
  }
  // Inspect actual service instances, not merely the presence of a project.
  raw,err=inspectGatewayInstances(ctx,spec.Project)
  if err!=nil {fmt.Fprintf(stderr,"gateway status: %v\n",err);return 1}
  var instances []gatewayStatusInstance
  if err=json.Unmarshal(raw,&instances);err!=nil {fmt.Fprintf(stderr,"gateway status: decode instances: %v\n",err);return 1}
  running:=0
  for _,instance:=range instances {
   if !gatewayStatusService(instance.Name) || instance.Config["user.aginctus.managed"]!="true" || instance.Config["user.aginctus.resource"]!="compose-instance" || instance.Config["user.aginctus.deployment"]!=spec.Project {
    fmt.Fprintf(stderr,"gateway project %q contains unexpected or unowned instance %q\n",spec.Project,instance.Name);return 1
   }
   if strings.EqualFold(instance.Status,"running") {running++}
  }
  fmt.Fprintf(out,"gateway %q: present (%d/%d service instances running; application readiness not verified)\n",spec.ID,running,len(instances))
  return 0
 }
 fmt.Fprintf(out,"gateway %q: absent\n",spec.ID)
 return 0
}

type gatewayStatusInstance struct {
 Name string `json:"name"`
 Status string `json:"status"`
 Config map[string]string `json:"config"`
}
var inspectGatewayInstances = func(ctx context.Context,project string)([]byte,error){
 cmd:=exec.CommandContext(ctx,"incus","list","--project",project,"--format=json")
 data,err:=cmd.CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("incus list gateway instances: %w: %s",err,strings.TrimSpace(string(data)))}
 return data,nil
}
func gatewayStatusService(name string) bool {
 for _,service:=range []string{"postgres","litellm"} {
  if name==service {return true}
  if strings.HasPrefix(name,service+"-") {
   suffix:=strings.TrimPrefix(name,service+"-")
   if suffix!="" && suffix[0]!='0' {
    valid:=true
    for _,ch:=range suffix {if ch<'0'||ch>'9'{valid=false;break}}
    if valid{return true}
   }
  }
 }
 return false
}
