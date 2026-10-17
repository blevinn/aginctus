package cli

import (
    "context"
    "fmt"
    "io"
    "os"
    "os/exec"
    "strings"

    "github.com/blevinn/aginctus/internal/config"
    "github.com/blevinn/aginctus/internal/firewall"
)

// Replaceable for unit tests. Never expose raw firewall utility output.
var firewallCommand = func(ctx context.Context, input string, name string, args ...string) (string,error) {
    cmd:=exec.CommandContext(ctx,name,args...)
    if input!="" {cmd.Stdin=strings.NewReader(input)}
    output,err:=cmd.CombinedOutput()
    return string(output),err
}

func runNetworkFirewallReconcile(ctx context.Context,stdout,stderr io.Writer,cfg *config.Config) int {
    if os.Geteuid()!=0 {
        fmt.Fprintln(stderr,"firewall reconciliation requires root (host INPUT mutation)")
        return 1
    }
    version,err:=firewallCommand(ctx,"","iptables","--version")
    if err!=nil || !strings.Contains(version,"(nf_tables)") {
        fmt.Fprintln(stderr,"unsupported firewall backend: require NixOS iptables-nft")
        return 1
    }
    ruleset,err:=firewallCommand(ctx,"","nft","-a","list","ruleset")
    if err!=nil {
        fmt.Fprintln(stderr,"cannot verify effective nftables input backend")
        return 1
    }
    if strings.Contains(ruleset,"table inet firewalld") {
        fmt.Fprintln(stderr,"active firewalld nftables chain detected; iptables-nft accept cannot override its reject")
        return 1
    }
    input,err:=firewallCommand(ctx,"","iptables","-S","nixos-fw")
    if err!=nil || !strings.Contains(input,"-j "+firewall.Chain) {
        fmt.Fprintln(stderr,"Aginctus nixos-fw hook missing; activate the host NixOS module")
        return 1
    }
    if _,err:=firewallCommand(ctx,"","iptables","-S",firewall.Chain);err!=nil {
        fmt.Fprintln(stderr,"Aginctus firewall chain missing")
        return 1
    }
    policy,discoveryErr:=deriveFirewallPolicy(ctx,cfg)
    var approved *firewall.Policy
    if discoveryErr==nil {approved=&policy}
    transaction,err:=firewall.Render(approved)
    if err!=nil {
        fmt.Fprintln(stderr,"invalid firewall policy")
        return 1
    }
    // --noflush preserves every foreign chain. The explicit -F clears only
    // our chain; iptables-nft commits its replacement transaction atomically.
    if _,err:=firewallCommand(ctx,transaction,"iptables-restore","-w","--noflush");err!=nil {
        fmt.Fprintln(stderr,"Aginctus INPUT transaction failed; no unrelated rules were modified")
        return 1
    }
    if discoveryErr!=nil {
        fmt.Fprintf(stderr,"Aginctus INPUT exceptions cleared (fail closed): %v\n",discoveryErr)
        return 1
    }
    fmt.Fprintf(stdout,"Aginctus INPUT policy reconciled for verified bridge %q (DNS/DHCP only)\n",policy.Interface)
    return 0
}
