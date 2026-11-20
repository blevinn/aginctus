package firewall

import (
	"fmt"
	"strings"
)

// These are host firewall observations and classification policies, not CLI
// authorization decisions. An automatic choice must never authorize NixOS
// native nftables writes without an explicitly selected owner/backend.
func SelectFirewallBackend(ruleset, requested string) (string, error) {
	firewalldPresent := strings.Contains(ruleset, "table inet firewalld")
	nixosPresent := strings.Contains(ruleset, "table inet nixos-fw")
	if firewalldPresent && nixosPresent {
		return "", fmt.Errorf("multiple independently rejecting nftables host firewalls detected; fail closed")
	}
	switch requested {
	case "nixos-nft":
		if !nixosPresent {
			return "", fmt.Errorf("NixOS native nftables input table not ready")
		}
		return "nixos-nft", nil
	case "firewalld":
		if !firewalldPresent {
			return "", fmt.Errorf("firewalld nftables table not ready; retry after firewalld startup")
		}
		return "firewalld", nil
	case "iptables-nft":
		if firewalldPresent || nixosPresent {
			return "", fmt.Errorf("configured iptables-nft backend conflicts with active nftables host firewall")
		}
		return "iptables-nft", nil
	case "":
		// Interactive CLI usage retains read-only automatic selection.
		if firewalldPresent {
			return "firewalld", nil
		}
		if nixosPresent {
			// Systemd explicitly pins the native backend. Automatic selection
			// must not authorize host mutation on an incidental table match.
			return "", fmt.Errorf("NixOS native nftables backend requires an explicit backend selection")
		}
		return "iptables-nft", nil
	default:
		return "", fmt.Errorf("unsupported firewall backend selection")
	}
}

func ContainsWord(words, target string) bool {
	for _, word := range strings.Fields(words) {
		if word == target {
			return true
		}
	}
	return false
}

func SameRichRules(current string, expected []string) bool {
	if strings.TrimSpace(current) == "" {
		return len(expected) == 0
	}
	existing := strings.Split(strings.TrimSpace(current), "\n")
	if len(existing) != len(expected) {
		return false
	}
	for _, rule := range expected {
		found := false
		for _, installed := range existing {
			if strings.TrimSpace(installed) == rule {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func HasFirewalldDropTarget(details string) bool {
	for _, line := range strings.Split(details, "\n") {
		if strings.TrimSpace(line) == "target: DROP" {
			return true
		}
	}
	return false
}
