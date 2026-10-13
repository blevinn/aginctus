package native

import (
 "context"
 "fmt"

 incusclient "github.com/lxc/incus/v7/client"
 "github.com/lxc/incus/v7/shared/api"
)

// NetworkReader is the smallest read-only Incus surface needed for the first
// native inspection slice. No create/update/delete method is exposed.
type NetworkReader interface {
 GetNetworks() ([]api.Network,error)
}
type NetworkConnector func(context.Context)(NetworkReader,error)
type NetworkObserver struct { connect NetworkConnector }
func NewNetworkObserver() *NetworkObserver {
 return &NetworkObserver{connect:func(ctx context.Context)(NetworkReader,error){
  return incusclient.ConnectIncusUnixWithContext(ctx,"",&incusclient.ConnectionArgs{SkipGetServer:true})
 }}
}
func NewNetworkObserverWithConnector(c NetworkConnector)*NetworkObserver {
 return &NetworkObserver{connect:c}
}
// ObserveNetworks inspects only explicitly requested, unscoped networks.
// Unsupported resources fail explicitly. Snapshot changes are planned by
// Plan separately; execution is deliberately absent in this stack.
func (o *NetworkObserver) ObserveNetworks(ctx context.Context, resources []Resource)(map[ID]Observed,error) {
 if err:=ValidateSelection(resources);err!=nil{return nil,err}
 for _,r:=range resources {
  if r.Kind!=Network || r.Project!="" {return nil,fmt.Errorf("network observer does not support %s %q in project %q",r.Kind,r.Name,r.Project)}
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 client,err:=o.connect(ctx);if err!=nil{return nil,fmt.Errorf("connect to Incus for network inspection: %w",err)}
 networks,err:=client.GetNetworks();if err!=nil{return nil,fmt.Errorf("list Incus networks: %w",err)}
 if err:=ctx.Err();err!=nil{return nil,err}
 known:=make(map[string]api.Network,len(networks))
 for _,network:=range networks {known[network.Name]=network}
 observed:=make(map[ID]Observed,len(resources))
 for _,r:=range resources {
  network,exists:=known[r.Name]
  if !exists {observed[r.ID()]=Observed{Exists:false};continue}
  config:=make(map[string]string,len(network.Config))
  for k,v:=range network.Config {config[k]=v}
  observed[r.ID()]=Observed{Exists:true,Config:config}
 }
 return observed,nil
}
