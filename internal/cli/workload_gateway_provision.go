package cli

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "os"
 "os/exec"
 "strings"

 "github.com/blevinn/aginctus/internal/config"
 "github.com/blevinn/aginctus/internal/workload"
)

// The key is deliberately piped to a fixed guest helper, never placed in argv.
var provisionGatewayGuest = func(ctx context.Context, instance string, payload []byte) error {
 cmd := exec.CommandContext(ctx, "incus", "exec", instance, "--project", "default", "--", "aginctus-opencode-gateway-provision")
 cmd.Stdin = strings.NewReader(string(payload))
 if err := cmd.Run(); err != nil { return fmt.Errorf("guest provisioning failed (verify image and permissions)") }
 return nil
}

type gatewayDiscoveryInstance struct {
 Name string `json:"name"`
 Status string `json:"status"`
 Config map[string]string `json:"config"`
 State struct {
  Network map[string]struct { Addresses []struct {
   Family string `json:"family"`
   Address string `json:"address"`
   Scope string `json:"scope"`
  } `json:"addresses"` } `json:"network"`
 } `json:"state"`
}

func discoverGatewayIPv4(data []byte, project string) (string, error) {
 var instances []gatewayDiscoveryInstance
 if err := json.Unmarshal(data, &instances); err != nil { return "", fmt.Errorf("decode gateway instances: %w", err) }
 addresses := map[string]bool{}
 count := 0
 for _, instance := range instances {
  if instance.Name != "litellm" && !strings.HasPrefix(instance.Name,"litellm-") { continue }
  if !gatewayStatusService(instance.Name) || instance.Config["user.aginctus.managed"] != "true" ||
   instance.Config["user.aginctus.resource"] != "compose-instance" ||
   instance.Config["user.aginctus.deployment"] != project ||
   !strings.EqualFold(instance.Status, "running") { return "", fmt.Errorf("LiteLLM not running or not owned") }
  count++
  for _, iface := range instance.State.Network {
   for _, a := range iface.Addresses {
    ip := net.ParseIP(a.Address)
    if a.Family == "inet" && ip != nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() {
     addresses[ip.String()] = true
    }
   }
  }
 }
 if count != 1 || len(addresses) != 1 { return "", fmt.Errorf("expected exactly one owned running LiteLLM instance with one private IPv4 address") }
 for address := range addresses { return address, nil }
 return "", errors.New("LiteLLM IP unavailable")
}

func readWorkloadKey(path string) (string,error) {
 info,err:=os.Lstat(path)
 if err!=nil { return "",fmt.Errorf("open workload key: %w",err) }
 if !info.Mode().IsRegular() || info.Mode().Perm()&0o077!=0 { return "",errors.New("workload key must be a private regular file") }
 f,err:=os.Open(path)
 if err!=nil { return "",err }
 defer f.Close()
 raw,err:=io.ReadAll(io.LimitReader(f, 8193))
 if err!=nil { return "",errors.New("read workload key failed") }
 key:=strings.TrimSpace(string(raw))
 if len(raw)>8192 || key=="" || strings.ContainsAny(key,"\r\n") { return "",errors.New("invalid workload key") }
 return key,nil
}

func runWorkloadGatewayProvision(ctx context.Context,args []string,stdout,stderr io.Writer,cfg *config.Config) int {
 if len(args)!=3 || !strings.HasPrefix(args[1],"--model=") || !strings.HasPrefix(args[2],"--key-file=") {
  fmt.Fprintln(stderr,"Usage: aginctus workload gateway provision <workload-id> --model=ALIAS --key-file=PATH")
  return 2
 }
 id:=args[0]
 model:=strings.TrimPrefix(args[1],"--model=")
 keyPath:=strings.TrimPrefix(args[2],"--key-file=")
 if model=="" || strings.TrimSpace(model)!=model || strings.ContainsAny(model,"/\r\n") || keyPath=="" {
  fmt.Fprintln(stderr,"invalid model or key-file option");return 2
 }
 target,err:=workload.FromConfig(cfg,id)
 if err!=nil { fmt.Fprintln(stderr,"invalid workload configuration"); return 1 }
 if target.Runtime!="opencode" || target.Isolation!="container" { fmt.Fprintln(stderr,"only OpenCode containers are supported");return 1 }
 instances,err:=statusInstances(ctx)
 if err!=nil { fmt.Fprintln(stderr,"cannot inspect workload ownership"); return 1 }
 if err:=requireRunningOwnedInstance(instances,target.Name,"workload","agent-workload",id); err!=nil {
  fmt.Fprintln(stderr,"workload missing, unowned or stopped");return 1
 }
 project,err:=cfg.String("gateway.compose.project")
 if err!=nil { fmt.Fprintln(stderr,"gateway project missing");return 1 }
 raw,err:=inspectGatewayProjects(ctx)
 if err!=nil { fmt.Fprintln(stderr,"cannot inspect gateway ownership");return 1 }
 var projects []statusProject
 if err=json.Unmarshal(raw,&projects);err!=nil { fmt.Fprintln(stderr,"invalid gateway project inventory");return 1 }
 owned:=false
 for _,p:=range projects { if p.Name==project && p.Config["user.aginctus.managed"]=="true" &&
  p.Config["user.aginctus.resource"]=="compose-project" && p.Config["user.aginctus.deployment"]==project {owned=true} }
 if !owned {fmt.Fprintln(stderr,"gateway project not owned by Aginctus");return 1}
 raw,err=inspectGatewayInstances(ctx,project)
 if err!=nil {fmt.Fprintln(stderr,"cannot inspect gateway instances");return 1}
 address,err:=discoverGatewayIPv4(raw,project)
 if err!=nil {fmt.Fprintln(stderr,"private LiteLLM endpoint not uniquely discoverable");return 1}
 key,err:=readWorkloadKey(keyPath)
 if err!=nil {fmt.Fprintln(stderr,"workload key unavailable or invalid");return 1}
 payload,_:=json.Marshal(map[string]string{"endpoint":"http://"+net.JoinHostPort(address,"4000")+"/v1","model":model,"key":key})
 if err=provisionGatewayGuest(ctx,target.Name,payload);err!=nil {fmt.Fprintln(stderr,"workload gateway provisioning failed");return 1}
 fmt.Fprintf(stdout,"workload %q: private LiteLLM runtime configuration provisioned (reachability not verified)\n",id)
 return 0
}
