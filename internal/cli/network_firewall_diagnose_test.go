package cli

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"
)

func TestFirewallDiagnoseNoMutationOrRulesetLeak(t *testing.T) {
    old := firewallProbe
    t.Cleanup(func() { firewallProbe = old })
    var calls []string
    firewallProbe = func(_ context.Context, name string, args ...string) (string, error) {
        calls = append(calls, name+" "+strings.Join(args, " "))
        if name == "iptables" { return "iptables v1.8 (nf_tables)", nil }
        return "table ip filter { chain filter_INPUT { reject # PRIVATE } }", nil
    }
    var out, errs bytes.Buffer
    if got := runNetworkFirewallDiagnose(context.Background(), &out, &errs); got != 0 {
        t.Fatalf("exit=%d stderr=%s", got, errs.String())
    }
    if !strings.Contains(out.String(), "iptables-nft") || !strings.Contains(out.String(), "reject: observed") {
        t.Fatalf("output=%q", out.String())
    }
    if strings.Contains(out.String()+errs.String(), "PRIVATE") { t.Fatal("leaked rules") }
    if got := strings.Join(calls, "|"); got != "iptables --version|nft -a list ruleset" {
        t.Fatalf("unexpected commands: %s", got)
    }
}

func TestFirewallDiagnoseFailsClosed(t *testing.T) {
    old := firewallProbe
    t.Cleanup(func() { firewallProbe = old })
    firewallProbe = func(_ context.Context, name string, _ ...string) (string, error) {
        if name == "iptables" { return "iptables (legacy)", nil }
        return "chain INPUT { accept }", nil
    }
    var out, errs bytes.Buffer
    if got := runNetworkFirewallDiagnose(context.Background(), &out, &errs); got != 1 {
        t.Fatalf("exit=%d output=%q", got, out.String())
    }
    firewallProbe = func(context.Context, string, ...string) (string, error) {
        return "", errors.New("permission denied")
    }
    out.Reset(); errs.Reset()
    if got := runNetworkFirewallDiagnose(context.Background(), &out, &errs); got != 1 {
        t.Fatalf("exit=%d", got)
    }
}
