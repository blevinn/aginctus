package herdrremote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	incusadapter "github.com/blevinn/aginctus/internal/incus"
	sshaccess "github.com/blevinn/aginctus/internal/orchestration/drivers/sshaccess"
)

type Incus interface {
	InstanceConfig(context.Context, string) (map[string]string, error)
	InstanceAddress(context.Context, string, string) (string, error)
	ExecInstance(context.Context, string, []string, string) (incusadapter.InstanceExecResult, error)
}

type Runtime struct {
	incus Incus
}

func NewRuntime(incus Incus) *Runtime {
	return &Runtime{incus: incus}
}

func (r *Runtime) Inspect(ctx context.Context, cfg sshaccess.Config) (sshaccess.State, error) {
	if err := r.verifyOwnership(ctx, cfg); err != nil {
		return sshaccess.State{}, err
	}
	identity, present, err := r.inspectIdentity(ctx, cfg.ClientInstance)
	if err != nil || !present {
		return sshaccess.State{IdentityPresent: present}, err
	}
	authorization, present, err := r.inspectAuthorization(ctx, cfg.TargetInstance, cfg.TargetAccount)
	if err != nil {
		return sshaccess.State{}, err
	}
	machinePresent, err := r.machinePresent(ctx, cfg)
	if err != nil {
		return sshaccess.State{}, err
	}
	return sshaccess.State{
		IdentityPresent:      true,
		AuthorizationPresent: present,
		AuthorizationMatches: present && strings.TrimSpace(authorization) == strings.TrimSpace(identity.PublicKey),
		MachinePresent:       machinePresent,
	}, nil
}

func (r *Runtime) Reconcile(ctx context.Context, cfg sshaccess.Config) error {
	if err := r.verifyOwnership(ctx, cfg); err != nil {
		return err
	}

	result, err := r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{"aginctus-ssh-client-identity", "ensure"}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("ensure Herdr client SSH identity", result)
	}
	identity, err := parseIdentity(result.Stdout)
	if err != nil {
		return err
	}

	address, err := r.incus.InstanceAddress(ctx, cfg.TargetInstance, "eth0")
	if err != nil {
		return err
	}
	hostKey, err := r.readHostKey(ctx, cfg.TargetInstance)
	if err != nil {
		return err
	}
	alias := hostAlias(cfg.WorkloadID)
	if err := r.verifyExistingHostTrust(ctx, cfg.ClientInstance, cfg.WorkloadID, alias, hostKey); err != nil {
		return err
	}

	// Refuse a changed host key before mutating workload authorization. This
	// keeps a failed trust check read-only with respect to the target.
	result, err = r.incus.ExecInstance(ctx, cfg.TargetInstance, []string{"aginctus-ssh-authorize", "reconcile", cfg.TargetAccount}, identity.PublicKey+"\n")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("authorize Herdr client on workload", result)
	}
	if err := r.publishSSHConfig(ctx, cfg.ClientInstance, cfg.WorkloadID, alias, address, cfg.TargetAccount, identity.Generation, hostKey); err != nil {
		return err
	}

	result, err = r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{"ssh", alias, "true"}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("probe workload SSH connection", result)
	}

	if err := r.reconcileMachine(ctx, cfg, alias); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) Revoke(ctx context.Context, cfg sshaccess.Config) error {
	if err := r.verifyClientOwnership(ctx, cfg.ClientInstance); err != nil {
		return err
	}
	if err := r.removeMachine(ctx, cfg); err != nil {
		return err
	}
	for _, path := range []string{
		remoteConfigPath(cfg.WorkloadID),
		knownHostsPath(cfg.WorkloadID),
	} {
		result, err := r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{"rm", "-f", path}, "")
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return helperError("remove Herdr remote SSH configuration", result)
		}
	}
	if err := r.verifyWorkloadOwnership(ctx, cfg); err != nil {
		return fmt.Errorf("Herdr remote removed from client, but workload authorization revocation is incomplete: %w", err)
	}

	result, err := r.incus.ExecInstance(ctx, cfg.TargetInstance, []string{"aginctus-ssh-authorize", "remove", cfg.TargetAccount}, "")
	if err != nil {
		return fmt.Errorf("Herdr remote removed from client, but workload authorization revocation is incomplete: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("Herdr remote removed from client, but workload authorization revocation is incomplete: %w", helperError("revoke Herdr client from workload", result))
	}
	return nil
}

func (r *Runtime) verifyOwnership(ctx context.Context, cfg sshaccess.Config) error {
	if err := r.verifyClientOwnership(ctx, cfg.ClientInstance); err != nil {
		return err
	}
	return r.verifyWorkloadOwnership(ctx, cfg)
}

func (r *Runtime) verifyClientOwnership(ctx context.Context, instance string) error {
	client, err := r.incus.InstanceConfig(ctx, instance)
	if err != nil {
		return err
	}
	for key, want := range map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": "infrastructure",
		"user.aginctus.role":     "herdr-client",
	} {
		if client[key] != want {
			return fmt.Errorf("Herdr client %q ownership mismatch: %s=%q, want %q", instance, key, client[key], want)
		}
	}
	return nil
}

func (r *Runtime) verifyWorkloadOwnership(ctx context.Context, cfg sshaccess.Config) error {
	target, err := r.incus.InstanceConfig(ctx, cfg.TargetInstance)
	if err != nil {
		return err
	}
	for key, want := range map[string]string{
		"user.aginctus.managed":  "true",
		"user.aginctus.resource": "workload",
		"user.aginctus.role":     "agent-workload",
		"user.aginctus.workload": cfg.WorkloadID,
	} {
		if target[key] != want {
			return fmt.Errorf("workload %q ownership mismatch: %s=%q, want %q", cfg.WorkloadID, key, target[key], want)
		}
	}
	return nil
}

type identity struct {
	Generation  string
	Fingerprint string
	PublicKey   string
}

func (r *Runtime) inspectIdentity(ctx context.Context, instance string) (identity, bool, error) {
	result, err := r.incus.ExecInstance(ctx, instance, []string{"aginctus-ssh-client-identity", "inspect"}, "")
	if err != nil {
		return identity{}, false, err
	}
	if result.ExitCode == 3 {
		return identity{}, false, nil
	}
	if result.ExitCode != 0 {
		return identity{}, false, helperError("inspect Herdr client SSH identity", result)
	}
	value, err := parseIdentity(result.Stdout)
	return value, err == nil, err
}

func (r *Runtime) inspectAuthorization(ctx context.Context, instance, account string) (string, bool, error) {
	result, err := r.incus.ExecInstance(ctx, instance, []string{"aginctus-ssh-authorize", "inspect", account}, "")
	if err != nil {
		return "", false, err
	}
	if result.ExitCode == 3 {
		return "", false, nil
	}
	if result.ExitCode != 0 {
		return "", false, helperError("inspect workload SSH authorization", result)
	}
	return strings.TrimSpace(result.Stdout), true, nil
}

func parseIdentity(output string) (identity, error) {
	var value identity
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		key, field, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		switch key {
		case "generation":
			value.Generation = field
		case "fingerprint":
			value.Fingerprint = field
		case "public_key":
			value.PublicKey = field
		}
	}
	if value.Generation == "" || value.Fingerprint == "" || value.PublicKey == "" {
		return identity{}, fmt.Errorf("invalid Herdr client SSH identity response")
	}
	if !strings.HasPrefix(value.PublicKey, "ssh-ed25519 ") {
		return identity{}, fmt.Errorf("Herdr client SSH identity is not Ed25519")
	}
	return value, nil
}

func (r *Runtime) readHostKey(ctx context.Context, instance string) (string, error) {
	result, err := r.incus.ExecInstance(ctx, instance, []string{"cat", "/etc/ssh/ssh_host_ed25519_key.pub"}, "")
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", helperError("read workload SSH host key", result)
	}
	fields := strings.Fields(result.Stdout)
	if len(fields) < 2 || fields[0] != "ssh-ed25519" {
		return "", fmt.Errorf("workload SSH host key is not Ed25519")
	}
	return fields[0] + " " + fields[1], nil
}

func (r *Runtime) verifyExistingHostTrust(ctx context.Context, client, workloadID, alias, hostKey string) error {
	path := knownHostsPath(workloadID)
	result, err := r.incus.ExecInstance(ctx, client, []string{"test", "-f", path}, "")
	if err != nil {
		return err
	}
	if result.ExitCode == 1 {
		return nil
	}
	if result.ExitCode != 0 {
		return helperError("inspect existing Herdr remote host trust", result)
	}

	result, err = r.incus.ExecInstance(ctx, client, []string{"cat", path}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("read existing Herdr remote host trust", result)
	}
	want := alias + " " + hostKey
	if strings.TrimSpace(result.Stdout) != want {
		return fmt.Errorf("workload %q SSH host key changed; remove the remote explicitly before trusting a replacement", workloadID)
	}
	return nil
}

func (r *Runtime) publishSSHConfig(ctx context.Context, client, workloadID, alias, address, account, generation, hostKey string) error {
	result, err := r.incus.ExecInstance(ctx, client, []string{"install", "-d", "-m", "0700", "/root/.ssh", "/var/lib/aginctus/ssh/remotes", "/var/lib/aginctus/ssh/known_hosts"}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("create Herdr client SSH configuration directories", result)
	}

	rootConfig := "Include /var/lib/aginctus/ssh/remotes/*.conf\n"
	if err := r.writeClientFile(ctx, client, "/root/.ssh/config", rootConfig, "0600"); err != nil {
		return err
	}

	knownHosts := fmt.Sprintf("%s %s\n", alias, hostKey)
	if err := r.writeClientFile(ctx, client, knownHostsPath(workloadID), knownHosts, "0600"); err != nil {
		return err
	}

	remoteConfig := fmt.Sprintf(
		"Host %s\n  HostName %s\n  User %s\n  IdentityFile /var/lib/aginctus/ssh/identities/%s/id_ed25519\n  IdentitiesOnly yes\n  IdentityAgent none\n  BatchMode yes\n  PasswordAuthentication no\n  KbdInteractiveAuthentication no\n  StrictHostKeyChecking yes\n  UserKnownHostsFile %s\n  GlobalKnownHostsFile /dev/null\n  HostKeyAlias %s\n  UpdateHostKeys no\n  ForwardAgent no\n  ForwardX11 no\n  ClearAllForwardings yes\n  ControlMaster no\n",
		alias, address, account, generation, knownHostsPath(workloadID), alias,
	)
	return r.writeClientFile(ctx, client, remoteConfigPath(workloadID), remoteConfig, "0600")
}

func (r *Runtime) writeClientFile(ctx context.Context, client, path, content, mode string) error {
	// Stage in the destination directory and rename only after content and mode
	// are complete, so readers never observe a partially written trust/config file.
	result, err := r.incus.ExecInstance(ctx, client, []string{"mktemp", path + ".tmp.XXXXXX"}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("stage Herdr client SSH configuration", result)
	}
	tmp := strings.TrimSpace(result.Stdout)
	if tmp == "" || !strings.HasPrefix(tmp, path+".tmp.") {
		return fmt.Errorf("stage Herdr client SSH configuration returned unexpected path %q", tmp)
	}
	cleanup := func() {
		_, _ = r.incus.ExecInstance(ctx, client, []string{"rm", "-f", tmp}, "")
	}

	result, err = r.incus.ExecInstance(ctx, client, []string{"tee", tmp}, content)
	if err != nil {
		cleanup()
		return err
	}
	if result.ExitCode != 0 {
		cleanup()
		return helperError("write Herdr client SSH configuration", result)
	}
	result, err = r.incus.ExecInstance(ctx, client, []string{"chmod", mode, tmp}, "")
	if err != nil {
		cleanup()
		return err
	}
	if result.ExitCode != 0 {
		cleanup()
		return helperError("set Herdr client SSH configuration permissions", result)
	}
	result, err = r.incus.ExecInstance(ctx, client, []string{"mv", tmp, path}, "")
	if err != nil {
		cleanup()
		return err
	}
	if result.ExitCode != 0 {
		cleanup()
		return helperError("publish Herdr client SSH configuration", result)
	}
	return nil
}

type machineRow struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Session string `json:"session"`
	Enabled bool   `json:"enabled"`
}

func (r *Runtime) machineRows(ctx context.Context, client string) ([]machineRow, error) {
	result, err := r.incus.ExecInstance(ctx, client, []string{"herdr", "machine", "list", "--json"}, "")
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, helperError("list Herdr machines", result)
	}
	var rows []machineRow
	if err := json.Unmarshal([]byte(result.Stdout), &rows); err != nil {
		return nil, fmt.Errorf("decode Herdr machine list: %w", err)
	}
	return rows, nil
}

func (r *Runtime) matchingMachine(ctx context.Context, cfg sshaccess.Config) (*machineRow, error) {
	rows, err := r.machineRows(ctx, cfg.ClientInstance)
	if err != nil {
		return nil, err
	}
	alias := hostAlias(cfg.WorkloadID)
	var match *machineRow
	for i := range rows {
		row := &rows[i]
		if row.Label != cfg.WorkloadID && row.Target != alias {
			continue
		}
		if row.Label != cfg.WorkloadID || row.Target != alias || row.Session != "default" {
			return nil, fmt.Errorf("Herdr machine conflict for workload %q: label=%q target=%q session=%q", cfg.WorkloadID, row.Label, row.Target, row.Session)
		}
		if match != nil {
			return nil, fmt.Errorf("multiple Herdr machine entries match workload %q", cfg.WorkloadID)
		}
		match = row
	}
	return match, nil
}

func (r *Runtime) machinePresent(ctx context.Context, cfg sshaccess.Config) (bool, error) {
	match, err := r.matchingMachine(ctx, cfg)
	return match != nil, err
}

func (r *Runtime) reconcileMachine(ctx context.Context, cfg sshaccess.Config, alias string) error {
	match, err := r.matchingMachine(ctx, cfg)
	if err != nil {
		return err
	}
	if match == nil {
		result, err := r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{
			"herdr", "machine", "add", alias,
			"--label", cfg.WorkloadID,
			"--remote-session", "default",
		}, "")
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return helperError("add Herdr machine", result)
		}
	} else if !match.Enabled {
		result, err := r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{"herdr", "machine", "enable", match.ID}, "")
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return helperError("enable Herdr machine", result)
		}
	}

	result, err := r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{"herdr", "machine", "status", cfg.WorkloadID, "--json"}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("verify Herdr machine", result)
	}
	return nil
}

func (r *Runtime) removeMachine(ctx context.Context, cfg sshaccess.Config) error {
	match, err := r.matchingMachine(ctx, cfg)
	if err != nil {
		return err
	}
	if match == nil {
		return nil
	}
	result, err := r.incus.ExecInstance(ctx, cfg.ClientInstance, []string{"herdr", "machine", "remove", match.ID}, "")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return helperError("remove Herdr machine", result)
	}
	return nil
}

func hostAlias(workloadID string) string {
	return "aginctus-" + workloadID
}

func remoteConfigPath(workloadID string) string {
	return "/var/lib/aginctus/ssh/remotes/" + workloadID + ".conf"
}

func knownHostsPath(workloadID string) string {
	return "/var/lib/aginctus/ssh/known_hosts/" + workloadID
}

func helperError(action string, result incusadapter.InstanceExecResult) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		return fmt.Errorf("%s failed with exit code %d", action, result.ExitCode)
	}
	return fmt.Errorf("%s failed with exit code %d: %s", action, result.ExitCode, detail)
}
