package firewall

import (
    "strings"
    "testing"
)
func TestRenderFirewalldHooksEffectiveInputAndOnlyDNSDHCP(t *testing.T) {
    p:=&Policy{Interface:"custom-bridge",Address:"10.20.30.1",Subnet:"10.20.30.0/24"}
    got,err:=RenderFirewalld(p,false,false)
    if err!=nil {t.Fatal(err)}
    for _,want:=range []string{
        "add chain inet firewalld aginctus_dns_input",
        "insert rule inet firewalld filter_INPUT jump aginctus_dns_input",
        "iifname \"custom-bridge\" ip saddr 10.20.30.0/24 ip daddr 10.20.30.1 udp dport 53 accept",
        "iifname \"custom-bridge\" ip saddr 10.20.30.0/24 ip daddr 10.20.30.1 tcp dport 53 accept",
        "iifname \"custom-bridge\" udp sport 68 udp dport 67 accept",
    }{
        if !strings.Contains(got,want){t.Errorf("missing %q: %s",want,got)}
    }
    for _,bad:=range []string{"hook input","POSTROUTING","forward","masquerade","tcp dport 22"} {
        if strings.Contains(got,bad){t.Errorf("unexpected %q: %s",bad,got)}
    }
}
func TestRenderFirewalldFlushOnlyOnDisown(t *testing.T) {
    got,err:=RenderFirewalld(nil,true,true)
    if err!=nil || got!="flush chain inet firewalld aginctus_dns_input\n"{
        t.Fatalf("got=%q err=%v",got,err)
    }
}
func TestRenderFirewalldFailsOnCorruptHookAndInjectedName(t *testing.T) {
    if _,err:=RenderFirewalld(nil,false,true);err==nil {t.Fatal("orphan hook admitted")}
    if _,err:=RenderFirewalld(&Policy{
        Interface:"bad\"\nflush table inet firewalld",Address:"10.20.30.1",Subnet:"10.20.30.0/24",
    },true,true);err==nil {t.Fatal("injection admitted")}
}
