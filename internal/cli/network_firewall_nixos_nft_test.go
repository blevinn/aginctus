package cli

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/blevinn/aginctus/internal/config"
    "github.com/blevinn/aginctus/internal/firewall"
)

func TestNativeNftReconcileScopedToOwnedChildChain(t *testing.T) {
    cfg:=firewallReconcileFixture(t)
    var txn string
    firewallCommand=func(_ context.Context,input,name string,args ...string)(string,error) {
        call:=name+" "+strings.Join(args," ")
        switch call {
        case "nft -a list ruleset":return "table inet nixos-fw { }",nil
        case "nft -a list chain inet nixos-fw input":
            return "chain input { type filter hook input priority filter; policy drop; ct state vmap { new : jump input-allow } }",nil
        case "nft -a list chain inet nixos-fw input-allow":
            return "chain input-allow { jump aginctus_dns_input comment \"aginctus:guest-host-dns-dhcp\" }",nil
        case "nft list chain inet nixos-fw aginctus_dns_input":
            return "chain aginctus_dns_input { }",nil
        case "nft -f -":txn=input;return "",nil
        default:t.Fatalf("unexpected command %q",call);return "",nil
        }
    }
    discoverFirewallPolicy=func(context.Context,*config.Config)(firewall.Policy,error){
        return firewall.Policy{Interface:"dynamic-net",Address:"10.40.50.1",Subnet:"10.40.50.0/24"},nil
    }
    var out,errs bytes.Buffer
    if got:=runNetworkFirewallReconcile(context.Background(),&out,&errs,cfg);got!=0{
        t.Fatalf("exit=%d stderr=%q",got,errs.String())
    }
    for _,want:=range []string{"flush chain inet nixos-fw aginctus_dns_input","iifname \"dynamic-net\"","udp dport 53","tcp dport 53","udp dport 67"}{
        if !strings.Contains(txn,want){t.Fatalf("missing %q: %s",want,txn)}
    }
}

func TestNativeNftDeletedBridgeClearsOnlyOwnedPermissions(t *testing.T){
    cfg:=firewallReconcileFixture(t)
    var txn string
    firewallCommand=func(_ context.Context,input,name string,args ...string)(string,error) {
        switch name+" "+strings.Join(args," ") {
        case "nft -a list ruleset":return "table inet nixos-fw { }",nil
        case "nft -a list chain inet nixos-fw input":
            return "chain input { type filter hook input priority filter; policy drop; jump input-allow }",nil
        case "nft -a list chain inet nixos-fw input-allow":
            return "chain input-allow { jump aginctus_dns_input }",nil
        case "nft list chain inet nixos-fw aginctus_dns_input":
            return "chain aginctus_dns_input { }",nil
        case "nft -f -":txn=input;return "",nil
        default:t.Fatal("unexpected firewall command");return "",nil
        }
    }
    discoverFirewallPolicy=func(context.Context,*config.Config)(firewall.Policy,error){
        return firewall.Policy{},errors.New("foreign network")
    }
    var out,errs bytes.Buffer
    if got:=runNetworkFirewallReconcile(context.Background(),&out,&errs,cfg);got!=1 {
        t.Fatalf("exit=%d",got)
    }
    if txn!="flush chain inet nixos-fw aginctus_dns_input\n" {
        t.Fatalf("unexpected policy cleanup %q",txn)
    }
}

func TestNativeNftMissingInputHookNeverMutates(t *testing.T){
    cfg:=firewallReconcileFixture(t)
    firewallCommand=func(_ context.Context,_ string,name string,args ...string)(string,error){
        switch name+" "+strings.Join(args," ") {
        case "nft -a list ruleset":return "table inet nixos-fw { }",nil
        case "nft -a list chain inet nixos-fw input":
            return "chain input { type filter hook input priority filter; policy drop; jump input-allow }",nil
        case "nft -a list chain inet nixos-fw input-allow":
            return "chain input-allow { }",nil
        default:t.Fatal("host mutated despite missing owned hook");return "",nil
        }
    }
    var out,errs bytes.Buffer
    if got:=runNetworkFirewallReconcile(context.Background(),&out,&errs,cfg);got!=1 {
        t.Fatalf("exit=%d",got)
    }
}
