package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/workload"
)

// All remote operations use authenticated Incus exec and the guest's
// persistent SSH state. Never export private keys to the Incus host.
var herdrMachineGuestExec = herdrAuthorizationGuestExec

type savedHerdrMachine struct {
	Label         string `json:"label"`
	SSHTarget     string `json:"ssh_target"`
	RemoteSession string `json:"remote_session"`
}

func parseSavedHerdrMachines(raw string) ([]savedHerdrMachine, error) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse Herdr machine inventory: %w", err)
	}
	result := make([]savedHerdrMachine, 0, len(entries))
	for _, entry := range entries {
		var machine savedHerdrMachine
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
			return nil, fmt.Errorf("Herdr machine inventory lacks required label or SSH target")
		}
		result = append(result, machine)
	}
	return result, nil
}

func verifySavedHerdrMachine(machines []savedHerdrMachine, label, target string) (bool, error) {
	for _, machine := range machines {
		if machine.Label != label {
			continue
		}
		if machine.SSHTarget != target || (machine.RemoteSession != "" && machine.RemoteSession != "default") {
			return false, fmt.Errorf("Herdr machine label %q conflicts with an existing profile", label)
		}
		return true, nil
	}
	return false, nil
}

func registerHerdrMachine(ctx context.Context, cfg *config.Config, id string) error {
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		return err
	}
	target, err := workload.FromConfig(cfg, id)
	if err != nil {
		return err
	}
	if target.Runtime != "opencode" || target.Isolation != "container" ||
		!managedSSHHostAlias.MatchString(target.Name) || client.Name == target.Name {
		return fmt.Errorf("Herdr registration requires a distinct managed OpenCode container")
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

	// Never publish a machine until SSH host trust and authentication succeed.
	if err := pinHerdrHost(ctx, cfg, id); err != nil {
		return fmt.Errorf("strict SSH preflight: %w", err)
	}

	addressOutput, _, err := herdrMachineGuestExec(ctx, target.Name, "", "ip", "-4", "-o", "addr", "show", "dev", "eth0")
	if err != nil {
		return fmt.Errorf("discover workload address: %w", err)
	}
	address, err := parseManagementAddress(addressOutput)
	if err != nil {
		return err
	}
	rawIdentity, _, err := herdrMachineGuestExec(ctx, client.Name, "", "aginctus-ssh-client-identity", "inspect")
	if err != nil {
		return fmt.Errorf("inspect persistent SSH identity: %w", err)
	}
	key, err := parseHerdrPublicIdentity(rawIdentity)
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
	_, code, err := herdrMachineGuestExec(ctx, client.Name, "", "aginctus-ssh-machine-config", "inspect", alias, address, generation)
	if err != nil {
		if code != 3 {
			return fmt.Errorf("inspect machine SSH config: %w", err)
		}
		if _, _, err := herdrMachineGuestExec(ctx, client.Name, "", "aginctus-ssh-machine-config", "reconcile", alias, address, generation); err != nil {
			return fmt.Errorf("write persistent SSH machine config: %w", err)
		}
	}
	if _, _, err := herdrMachineGuestExec(ctx, client.Name, "", "aginctus-ssh-machine-config", "inspect", alias, address, generation); err != nil {
		return fmt.Errorf("verify managed SSH machine config: %w", err)
	}
	// Prove the SSH alias resolves via the installed guest configuration,
	// not merely with bespoke flags used by the host-trust preflight.
	result, _, err := herdrMachineGuestExec(ctx, client.Name, "", "ssh", "-o", "BatchMode=yes", alias, "id", "-un")
	if err != nil || strings.TrimSpace(result) != "agent" {
		return fmt.Errorf("managed SSH alias probe failed: %v", err)
	}

	raw, _, err := herdrMachineGuestExec(ctx, client.Name, "", "herdr", "machine", "list", "--json")
	if err != nil {
		return fmt.Errorf("list saved Herdr machines: %w", err)
	}
	machines, err := parseSavedHerdrMachines(raw)
	if err != nil {
		return err
	}
	exists, err := verifySavedHerdrMachine(machines, alias, alias)
	if err != nil {
		return err
	}
	if !exists {
		if _, _, err := herdrMachineGuestExec(ctx, client.Name, "", "herdr", "machine", "add",
			alias, "--label", alias, "--remote-session", "default"); err != nil {
			return fmt.Errorf("register remote Herdr machine: %w", err)
		}
	}
	raw, _, err = herdrMachineGuestExec(ctx, client.Name, "", "herdr", "machine", "list", "--json")
	if err != nil {
		return fmt.Errorf("verify saved Herdr machine: %w", err)
	}
	machines, err = parseSavedHerdrMachines(raw)
	if err != nil {
		return err
	}
	exists, err = verifySavedHerdrMachine(machines, alias, alias)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Herdr machine was not saved")
	}
	return nil
}

func runHerdrRegister(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, "Usage: aginctus herdr register <workload-id>")
		return 2
	}
	if err := registerHerdrMachine(ctx, cfg, args[0]); err != nil {
		fmt.Fprintf(stderr, "Herdr machine registration: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Herdr machine for workload %q: registered and SSH verified\n", args[0])
	return 0
}
