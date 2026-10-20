package gateway

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "testing"
)

func stagedEgressSpec() Spec {
 s:=egressTestSpec()
 s.EgressBridgeIPv4CIDR="10.42.40.1/24"
 s.EgressIPv4Address="10.42.40.25/24"
 return s
}

func fakeStagedFirewalld(s Spec, mutated *[]string, corrupt string) FirewalldExecutor {
 prefix:="10.42.40.25/32"
 zone:=fmt.Sprintf("aginctus-egress\n  summary: \n  description: %s\n  target: default\n  interfaces: aginctus-egress\n  sources: \n  services: dns dhcp\n  ports: \n  protocols: \n  forward: no\n  masquerade: no\n  forward-ports: \n",egressFirewallOwnership)
 policy:=fmt.Sprintf("aginctus-egress-https\n  summary: \n  description: %s\n  priority: -1000\n  target: CONTINUE\n  ingress-zones: aginctus-egress\n  egress-zones: nixos-fw-default\n  services: \n  ports: \n  protocols: \n  forward-ports: \n",egressFirewallOwnership)
 rules:=append(egressDenyRules(prefix),fmt.Sprintf(`rule family="ipv4" priority="-200" source address="%s" port port="443" protocol="tcp" accept`,prefix))
 return func(_ context.Context,args ...string)(string,error){
  command:=strings.Join(args," ")
  if strings.Contains(command,"--delete-") {*mutated=append(*mutated,command);return "success",nil}
  switch command {
  case "--permanent --info-zone=aginctus-egress":return zone,nil
  case "--permanent --info-policy=aginctus-egress-https":
   if corrupt=="priority"{return strings.Replace(policy,"priority: -1000","priority: 0",1),nil}
   return policy,nil
  case "--permanent --policy=aginctus-egress-https --list-rich-rules":
   if corrupt=="rule"{rules=rules[:len(rules)-1]}
   return strings.Join(rules,"\n"),nil
  default:return "",errors.New("unexpected firewall-cmd: "+command)
  }
 }
}

func TestCheckStagedEgressFirewalld(t *testing.T) {
 s:=stagedEgressSpec()
 for _,corrupt:=range []string{"","priority","rule"} {
  var mutations []string
  run:=fakeStagedFirewalld(s,&mutations,corrupt)
  err:=s.CheckStagedEgressFirewalld(context.Background(),"nixos-fw-default",run)
  if corrupt=="" && err!=nil{t.Errorf("expected valid staged configuration: %v",err)}
  if corrupt!="" && err==nil{t.Errorf("accepted drift %s",corrupt)}
  if len(mutations)!=0{t.Errorf("read-only verification mutated firewall: %v",mutations)}
 }
}
func TestUnstageEgressFirewalldOwnership(t *testing.T){
 s:=stagedEgressSpec()
 for _,corrupt:=range []string{"","rule"} {
  var mutations []string
  run:=fakeStagedFirewalld(s,&mutations,corrupt)
  err:=s.UnstageEgressFirewalld(context.Background(),"nixos-fw-default",run)
  if corrupt!="" {
   if err==nil || len(mutations)!=0{t.Fatalf("deleted drifted objects: %v %v",err,mutations)}
  }else{
   if err!=nil{t.Fatal(err)}
   if len(mutations)!=2 || !strings.Contains(mutations[0],"--delete-policy=") || !strings.Contains(mutations[1],"--delete-zone="){t.Fatalf("unexpected cleanup: %v",mutations)}
  }
 }
}
