package herdr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/workload"
)

type GuestExec func(context.Context, string, string, ...string) (string, int, error)

// Actions exposes only the pre-verified guest and instance capabilities.
type Actions struct {
	AuthorizeExec GuestExec
	TrustExec GuestExec
	MachineExec GuestExec
	CheckRunningPair func(context.Context, string, string, string) error
	PinHost func(context.Context, *config.Config, string) error
}

// Authorization gives a workload only the managed public client key, never a
// private key or a remotely discovered SSH trust anchor.
var ManagedClientPublicKeyPattern = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/=]+ aginctus:[1-9][0-9]*$`)

func ParsePublicIdentity(output string) (string, error) {
	var generation string
	var publicKey string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "generation":
			if generation != "" || len(fields) != 2 {
				return "", fmt.Errorf("invalid client identity generation response")
			}
			if n, err := strconv.ParseUint(fields[1], 10, 64); err != nil || n == 0 {
				return "", fmt.Errorf("invalid client identity generation")
			}
			generation = fields[1]
		case "public_key":
			if publicKey != "" || len(fields) != 4 {
				return "", fmt.Errorf("invalid client public key response")
			}
			publicKey = strings.Join(fields[1:], " ")
		case "fingerprint":
			if len(fields) != 2 || !strings.HasPrefix(fields[1], "SHA256:") {
				return "", fmt.Errorf("invalid client identity fingerprint response")
			}
		default:
			return "", fmt.Errorf("unrecognized client identity field")
		}
	}
	if generation == "" || !ManagedClientPublicKeyPattern.MatchString(publicKey) ||
		!strings.HasSuffix(publicKey, " aginctus:"+generation) {
		return "", fmt.Errorf("missing or invalid managed client public key")
	}
	return publicKey, nil
}

func VerifyManagedAuthorization(publicKey, installed string) error {
	if strings.TrimSpace(installed) != publicKey {
		return fmt.Errorf("workload has different managed authorization; refusing automatic credential replacement")
	}
	return nil
}

// AuthorizeWorkload is deliberately limited to client public-key delivery.
// It does not claim a verified SSH connection or publish a Herdr machine.
// This initial CLI transport uses the existing Incus exec boundary; moving the
// helper calls into the Incus Go adapter is tracked by the wider SSH lifecycle.
func AuthorizeWorkload(ctx context.Context, cfg *config.Config, id string, actions Actions) error {
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("herdr client configuration: %w", err)
	}
	target, err := workload.FromConfig(cfg, id)
	if err != nil {
		return fmt.Errorf("workload configuration: %w", err)
	}
	if target.Runtime != "opencode" || target.Isolation != "container" {
		return fmt.Errorf("SSH authorization is currently supported only for OpenCode containers")
	}
	if client.Name == target.Name {
		return fmt.Errorf("client and workload instance names must differ")
	}
	if err := actions.CheckRunningPair(ctx, client.Name, target.Name, target.ID); err != nil {
		return err
	}

	identity, _, err := actions.AuthorizeExec(ctx, client.Name, "", "aginctus-ssh-client-identity", "inspect")
	if err != nil {
		return fmt.Errorf("client identity must already exist: %w", err)
	}
	key, err := ParsePublicIdentity(identity)
	if err != nil {
		return err
	}

	installed, status, inspectErr := actions.AuthorizeExec(ctx, target.Name, "", "aginctus-ssh-authorize", "inspect", "agent")
	if inspectErr != nil && status != 3 {
		return fmt.Errorf("inspect workload authorization: %w", inspectErr)
	}
	if inspectErr == nil {
		return VerifyManagedAuthorization(key, installed)
	}
	// Code 3 is the documented 'not provisioned' result for inspect only.
	if _, _, err := actions.AuthorizeExec(ctx, target.Name, key+"\n", "aginctus-ssh-authorize", "reconcile", "agent"); err != nil {
		return fmt.Errorf("reconcile workload authorization: %w", err)
	}
	installed, _, err = actions.AuthorizeExec(ctx, target.Name, "", "aginctus-ssh-authorize", "inspect", "agent")
	if err != nil {
		return fmt.Errorf("verify workload authorization: %w", err)
	}
	return VerifyManagedAuthorization(key, installed)
}

func RunAuthorize(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, actions Actions) int {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, "Usage: aginctus herdr authorize <workload-id>")
		return 2
	}
	if err := AuthorizeWorkload(ctx, cfg, args[0], actions); err != nil {
		fmt.Fprintf(stderr, "Herdr workload authorization: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Herdr public-key authorization for workload %q: verified (remote SSH not yet probed)\n", args[0])
	return 0
}

// Host trust is pinned through Incus guest inspection before SSH is probed.
var ManagedSSHHostAlias = regexp.MustCompile(`^aginctus-[A-Za-z0-9_-]+$`)

func ParseWorkloadHostKey(raw string) (string, error) {
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

func ParseManagementAddress(output string) (string, error) {
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

// PinHost performs one explicit authenticated trust handoff, then probes
// SSH using the managed identity. A failed SSH probe does not undo a verified
// pin, and is reported as a separate connectivity failure.
func PinHost(ctx context.Context, cfg *config.Config, id string, actions Actions) error {
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("herdr client configuration: %w", err)
	}
	target, err := workload.FromConfig(cfg, id)
	if err != nil {
		return fmt.Errorf("workload configuration: %w", err)
	}
	if target.Runtime != "opencode" || target.Isolation != "container" {
		return fmt.Errorf("host trust currently supports only OpenCode containers")
	}
	alias := target.Name
	if !ManagedSSHHostAlias.MatchString(alias) || client.Name == target.Name {
		return fmt.Errorf("invalid managed host alias")
	}
	if err := actions.CheckRunningPair(ctx, client.Name, target.Name, target.ID); err != nil {
		return err
	}

	raw, _, err := actions.TrustExec(ctx, target.Name, "", "cat", "/etc/ssh/ssh_host_ed25519_key.pub")
	if err != nil {
		return fmt.Errorf("read workload public host key: %w", err)
	}
	key, err := ParseWorkloadHostKey(raw)
	if err != nil {
		return err
	}
	addressText, _, err := actions.TrustExec(ctx, target.Name, "", "ip", "-4", "-o", "addr", "show", "dev", "eth0")
	if err != nil {
		return fmt.Errorf("discover workload management address: %w", err)
	}
	address, err := ParseManagementAddress(addressText)
	if err != nil {
		return err
	}

	pinned, status, inspectErr := actions.TrustExec(ctx, client.Name, "", "aginctus-ssh-host-trust", "inspect", alias)
	expected := alias + " " + key
	if inspectErr == nil {
		if strings.TrimSpace(pinned) != expected {
			return fmt.Errorf("pinned host key differs; explicit recovery required")
		}
	} else if status == 3 {
		if _, _, err := actions.TrustExec(ctx, client.Name, key+"\n", "aginctus-ssh-host-trust", "reconcile", alias); err != nil {
			return fmt.Errorf("pin SSH host trust: %w", err)
		}
	} else {
		return fmt.Errorf("inspect pinned host key: %w", inspectErr)
	}
	pinned, _, err = actions.TrustExec(ctx, client.Name, "", "aginctus-ssh-host-trust", "inspect", alias)
	if err != nil || strings.TrimSpace(pinned) != expected {
		return fmt.Errorf("verify pinned SSH host key: mismatch or unavailable")
	}

	clientIdentity, _, err := actions.TrustExec(ctx, client.Name, "", "aginctus-ssh-client-identity", "inspect")
	if err != nil {
		return fmt.Errorf("inspect client SSH identity: %w", err)
	}
	clientKey, err := ParsePublicIdentity(clientIdentity)
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
	identity, _, err := actions.TrustExec(ctx, client.Name, "", probe...)
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

func RunPinHost(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, actions Actions) int {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, "Usage: aginctus herdr pin-host <workload-id>")
		return 2
	}
	if err := PinHost(ctx, cfg, args[0], actions); err != nil {
		fmt.Fprintf(stderr, "Herdr host trust: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Herdr host trust for workload %q: pinned; strict SSH probe passed\n", args[0])
	return 0
}

// A machine is registered only after the strict alias/host-key preflight.
type SavedHerdrMachine struct {
	Label         string `json:"label"`
	SSHTarget     string `json:"ssh_target"`
	RemoteSession string `json:"remote_session"`
}

func ParseSavedMachines(raw string) ([]SavedHerdrMachine, error) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse Herdr machine inventory: %w", err)
	}
	result := make([]SavedHerdrMachine, 0, len(entries))
	for _, entry := range entries {
		var machine SavedHerdrMachine
		for _, field := range []struct {
			keys []string
			out  *string
		}{
			{[]string{"label"}, &machine.Label},
			{[]string{"ssh_target", "sshTarget", "target"}, &machine.SSHTarget},
			{[]string{"remote_session", "remoteSession", "session"}, &machine.RemoteSession},
		} {
			for _, name := range field.keys {
				if value, ok := entry[name]; ok {
					if err := json.Unmarshal(value, field.out); err != nil {
						return nil, fmt.Errorf("invalid Herdr machine %s: %w", name, err)
					}
					break
				}
			}
		}
		if machine.Label == "" || machine.SSHTarget == "" {
			return nil, fmt.Errorf("herdr machine inventory lacks required label or SSH target")
		}
		result = append(result, machine)
	}
	return result, nil
}

func VerifySavedMachine(machines []SavedHerdrMachine, label, target string) (bool, error) {
	for _, machine := range machines {
		if machine.Label != label {
			continue
		}
		if machine.SSHTarget != target || (machine.RemoteSession != "" && machine.RemoteSession != "default") {
			return false, fmt.Errorf("herdr machine label %q conflicts with an existing profile", label)
		}
		return true, nil
	}
	return false, nil
}

func RegisterMachine(ctx context.Context, cfg *config.Config, id string, actions Actions) error {
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		return err
	}
	target, err := workload.FromConfig(cfg, id)
	if err != nil {
		return err
	}
	if target.Runtime != "opencode" || target.Isolation != "container" ||
		!ManagedSSHHostAlias.MatchString(target.Name) || client.Name == target.Name {
		return fmt.Errorf("herdr registration requires a distinct managed OpenCode container")
	}
	if err := actions.CheckRunningPair(ctx, client.Name, target.Name, target.ID); err != nil {
		return err
	}

	// Never publish a machine until SSH host trust and authentication succeed.
	if err := actions.PinHost(ctx, cfg, id); err != nil {
		return fmt.Errorf("strict SSH preflight: %w", err)
	}

	addressOutput, _, err := actions.MachineExec(ctx, target.Name, "", "ip", "-4", "-o", "addr", "show", "dev", "eth0")
	if err != nil {
		return fmt.Errorf("discover workload address: %w", err)
	}
	address, err := ParseManagementAddress(addressOutput)
	if err != nil {
		return err
	}
	rawIdentity, _, err := actions.MachineExec(ctx, client.Name, "", "aginctus-ssh-client-identity", "inspect")
	if err != nil {
		return fmt.Errorf("inspect persistent SSH identity: %w", err)
	}
	key, err := ParsePublicIdentity(rawIdentity)
	if err != nil {
		return err
	}
	generation := strings.TrimPrefix(strings.Fields(key)[2], "aginctus:")
	if _, err := strconv.ParseUint(generation, 10, 64); err != nil {
		return fmt.Errorf("invalid identity generation: %w", err)
	}

	alias := target.Name
	// A missing config is installed once; an existing different config
	// requires explicit operator recovery, never silent credential rotation.
	_, code, err := actions.MachineExec(ctx, client.Name, "", "aginctus-ssh-machine-config", "inspect", alias, address, generation)
	if err != nil {
		if code != 3 {
			return fmt.Errorf("inspect machine SSH config: %w", err)
		}
		if _, _, err := actions.MachineExec(ctx, client.Name, "", "aginctus-ssh-machine-config", "reconcile", alias, address, generation); err != nil {
			return fmt.Errorf("write persistent SSH machine config: %w", err)
		}
	}
	if _, _, err := actions.MachineExec(ctx, client.Name, "", "aginctus-ssh-machine-config", "inspect", alias, address, generation); err != nil {
		return fmt.Errorf("verify managed SSH machine config: %w", err)
	}
	// Prove the SSH alias resolves via the installed guest configuration,
	// not merely with bespoke flags used by the host-trust preflight.
	// The mismatch case is reported separately from the exec error case:
	// a successful session that returns the wrong account is a real defect,
	// not a transient transport failure.
	result, _, err := actions.MachineExec(ctx, client.Name, "", "ssh", "-o", "BatchMode=yes", alias, "id", "-un")
	if err != nil {
		return fmt.Errorf("managed SSH alias probe failed: %w", err)
	}
	if strings.TrimSpace(result) != "agent" {
		return fmt.Errorf("managed SSH alias probe returned unexpected account %q", strings.TrimSpace(result))
	}

	raw, _, err := actions.MachineExec(ctx, client.Name, "", "herdr", "machine", "list", "--json")
	if err != nil {
		return fmt.Errorf("list saved Herdr machines: %w", err)
	}
	machines, err := ParseSavedMachines(raw)
	if err != nil {
		return err
	}
	exists, err := VerifySavedMachine(machines, alias, alias)
	if err != nil {
		return err
	}
	if !exists {
		if _, _, err := actions.MachineExec(ctx, client.Name, "", "herdr", "machine", "add",
			alias, "--label", alias, "--remote-session", "default"); err != nil {
			return fmt.Errorf("register remote Herdr machine: %w", err)
		}
	}
	raw, _, err = actions.MachineExec(ctx, client.Name, "", "herdr", "machine", "list", "--json")
	if err != nil {
		return fmt.Errorf("verify saved Herdr machine: %w", err)
	}
	machines, err = ParseSavedMachines(raw)
	if err != nil {
		return err
	}
	exists, err = VerifySavedMachine(machines, alias, alias)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("herdr machine was not saved")
	}
	return nil
}

func RunRegister(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config, actions Actions) int {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, "Usage: aginctus herdr register <workload-id>")
		return 2
	}
	if err := RegisterMachine(ctx, cfg, args[0], actions); err != nil {
		fmt.Fprintf(stderr, "Herdr machine registration: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Herdr machine for workload %q: registered and SSH verified\n", args[0])
	return 0
}
