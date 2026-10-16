package cli

import (
    "context"
    "fmt"
    "io"
    "os/exec"
    "strings"
)

// firewallProbe is read-only. Never echo raw firewall rules: unrelated host
// addresses, topology and service details may be private.
var firewallProbe = func(ctx context.Context, name string, args ...string) (string, error) {
    output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
    return string(output), err
}

func runNetworkFirewallDiagnose(ctx context.Context, stdout, stderr io.Writer) int {
    version, err := firewallProbe(ctx, "iptables", "--version")
    if err != nil {
        fmt.Fprintln(stderr, "iptables backend: unavailable; install iptables for read-only inspection")
        return 1
    }
    backend := "unknown"
    if strings.Contains(version, "(nf_tables)") { backend = "iptables-nft" }
    if strings.Contains(version, "(legacy)") { backend = "iptables-legacy" }
    fmt.Fprintf(stdout, "iptables backend: %s\n", backend)
    rules, err := firewallProbe(ctx, "nft", "-a", "list", "ruleset")
    if err != nil {
        fmt.Fprintln(stderr, "nftables ruleset: cannot inspect (read privileges may be required)")
        return 1
    }
    switch {
    case strings.Contains(rules, "chain filter_INPUT") && strings.Contains(rules, "reject"):
        fmt.Fprintln(stdout, "NixOS-style INPUT chain with reject: observed")
    case strings.Contains(rules, "table inet nixos-fw") || strings.Contains(rules, "table ip nixos-fw"):
        fmt.Fprintln(stdout, "NixOS nftables firewall: observed; iptables-nft integration is not applicable")
    default:
        fmt.Fprintln(stderr, "effective firewall INPUT path: unknown; fail closed until verified")
        return 1
    }
    fmt.Fprintln(stdout, "guest DNS verdict: unconfirmed; use a targeted packet trace to identify the final verdict")
    return 0
}
