package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/workload"
)

// Host trust is obtained from the workload through Incus, never from the SSH
// network. The guest helper owns the persistent, fail-closed known_hosts state.
var herdrHostTrustGuestExec = herdrAuthorizationGuestExec

var managedSSHHostAlias = regexp.MustCompile(`^aginctus-[A-Za-z0-9_-]+$`)

func parseWorkloadHostKey(raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != 3 || fields[0] != "ssh-ed25519" {
		return "", fmt.Errorf("invalid workload Ed25519 host public key")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(fields[1])
	if err != nil || len(decoded) < 48 {
		return "", fmt.Errorf("invalid workload host key encoding")
	}
	// Ed25519 wire format: length-prefixed name and 32-byte key.
	if string(decoded[:4]) != "\x00\x00\x00\x0b" || string(decoded[4:15]) != "ssh-ed25519" ||
		string(decoded[15:19]) != "\x00\x00\x00\x20" || len(decoded) != 51 {
		return "", fmt.Errorf("invalid Ed25519 host key wire format")
	}
	return fields[0] + " " + fields[1], nil
}

func parseManagementAddress(output string) (string, error) {
	var address string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "inet" {
				continue
			}
			cidr := fields[i+1]
			ip, _, err := net.ParseCIDR(cidr)
			if err != nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return "", fmt.Errorf("invalid management IPv4 address")
			}
			if address != "" {
				return "", fmt.Errorf("ambiguous management IPv4 address")
			}
			address = ip.String()
		}
	}
	if address == "" {
		return "", fmt.Errorf("missing management IPv4 address")
	}
	return address, nil
}

// pinHerdrHost performs one explicit authenticated trust handoff, then probes
// SSH using the managed identity. A failed SSH probe does not undo a verified
// pin, and is reported as a separate connectivity failure.
func pinHerdrHost(ctx context.Context, cfg *config.Config, id string) error {
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("Herdr client configuration: %w", err)
	}
	target, err := workload.FromConfig(cfg, id)
	if err != nil {
		return fmt.Errorf("workload configuration: %w", err)
	}
	if target.Runtime != "opencode" || target.Isolation != "container" {
		return fmt.Errorf("host trust currently supports only OpenCode containers")
	}
	alias := target.Name
	if !managedSSHHostAlias.MatchString(alias) || client.Name == target.Name {
		return fmt.Errorf("invalid managed host alias")
	}
	instances, err := statusInstances(ctx)
	if err != nil {
		return fmt.Errorf("inspect Incus ownership: %w", err)
	}
	if err := requireRunningOwnedInstance(instances, client.Name, "infrastructure", "herdr-client", ""); err != nil {
		return err
	}
	if err := requireRunningOwnedInstance(instances, target.Name, "workload", "agent-workload", target.ID); err != nil {
		return err
	}

	raw, _, err := herdrHostTrustGuestExec(ctx, target.Name, "", "cat", "/etc/ssh/ssh_host_ed25519_key.pub")
	if err != nil {
		return fmt.Errorf("read workload public host key: %w", err)
	}
	key, err := parseWorkloadHostKey(raw)
	if err != nil {
		return err
	}
	addressText, _, err := herdrHostTrustGuestExec(ctx, target.Name, "", "ip", "-4", "-o", "addr", "show", "dev", "eth0")
	if err != nil {
		return fmt.Errorf("discover workload management address: %w", err)
	}
	address, err := parseManagementAddress(addressText)
	if err != nil {
		return err
	}

	pinned, status, inspectErr := herdrHostTrustGuestExec(ctx, client.Name, "", "aginctus-ssh-host-trust", "inspect", alias)
	expected := alias + " " + key
	if inspectErr == nil {
		if strings.TrimSpace(pinned) != expected {
			return fmt.Errorf("pinned host key differs; explicit recovery required")
		}
	} else if status == 3 {
		if _, _, err := herdrHostTrustGuestExec(ctx, client.Name, key+"\n", "aginctus-ssh-host-trust", "reconcile", alias); err != nil {
			return fmt.Errorf("pin SSH host trust: %w", err)
		}
	} else {
		return fmt.Errorf("inspect pinned host key: %w", inspectErr)
	}
	pinned, _, err = herdrHostTrustGuestExec(ctx, client.Name, "", "aginctus-ssh-host-trust", "inspect", alias)
	if err != nil || strings.TrimSpace(pinned) != expected {
		return fmt.Errorf("verify pinned SSH host key: mismatch or unavailable")
	}

	clientIdentity, _, err := herdrHostTrustGuestExec(ctx, client.Name, "", "aginctus-ssh-client-identity", "inspect")
	if err != nil {
		return fmt.Errorf("inspect client SSH identity: %w", err)
	}
	clientKey, err := parseHerdrPublicIdentity(clientIdentity)
	if err != nil {
		return err
	}
	parts := strings.Split(clientKey, " aginctus:")
	generation := parts[len(parts)-1]

	// Strict verification is independent of ~/.ssh, system host files, and
	// forwarded SSH agents. The private key is never copied to the host.
	probe := []string{"ssh", "-F", "/dev/null",
		"-i", "/var/lib/aginctus/ssh/identities/" + generation + "/id_ed25519",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UpdateHostKeys=no",
		"-o", "UserKnownHostsFile=/var/lib/aginctus/ssh/trust/known_hosts",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "HostKeyAlias=" + alias,
		"-o", "HostKeyAlgorithms=ssh-ed25519",
		"-o", "ConnectTimeout=5",
		"agent@" + address, "id", "-un"}
	identity, _, err := herdrHostTrustGuestExec(ctx, client.Name, "", probe...)
	if err != nil {
		return fmt.Errorf("host key pinned but strict SSH probe failed (check host firewall and network): %w", err)
	}
	// Report the actual account name on mismatch; the previous string
	// rendered the bare "SSH probe returned unexpected remote login
	// account" with no diagnostic value.
	if account := strings.TrimSpace(identity); account != "agent" {
		return fmt.Errorf("SSH probe returned unexpected remote login account %q", account)
	}
	return nil
}

func runHerdrPinHost(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, "Usage: aginctus herdr pin-host <workload-id>")
		return 2
	}
	if err := pinHerdrHost(ctx, cfg, args[0]); err != nil {
		fmt.Fprintf(stderr, "Herdr host trust: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Herdr host trust for workload %q: pinned; strict SSH probe passed\n", args[0])
	return 0
}
