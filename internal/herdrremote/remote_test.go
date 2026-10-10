package herdrremote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	incusadapter "github.com/blevinn/aginctus/internal/incus"
	sshaccess "github.com/blevinn/aginctus/internal/orchestration/drivers/sshaccess"
)

type fakeIncus struct {
	configs map[string]map[string]string
	address string
	execs   []fakeExec
	run     func(string, []string, string) (incusadapter.InstanceExecResult, error)
}

type fakeExec struct {
	instance string
	command  []string
	stdin    string
}

func (f *fakeIncus) InstanceConfig(_ context.Context, name string) (map[string]string, error) {
	cfg, ok := f.configs[name]
	if !ok {
		return nil, errors.New("missing instance")
	}
	return cfg, nil
}

func (f *fakeIncus) InstanceAddress(_ context.Context, name, interfaceName string) (string, error) {
	if name != "aginctus-dev" || interfaceName != "eth0" {
		return "", errors.New("unexpected address lookup")
	}
	if f.address == "" {
		return "", errors.New("missing address")
	}
	return f.address, nil
}

func (f *fakeIncus) ExecInstance(_ context.Context, instance string, command []string, stdin string) (incusadapter.InstanceExecResult, error) {
	f.execs = append(f.execs, fakeExec{instance: instance, command: append([]string(nil), command...), stdin: stdin})
	if f.run != nil {
		return f.run(instance, command, stdin)
	}
	return incusadapter.InstanceExecResult{}, nil
}

func ownedIncus() *fakeIncus {
	return &fakeIncus{
		address: "10.42.0.25",
		configs: map[string]map[string]string{
			"aginctus-herdr": {
				"user.aginctus.managed":  "true",
				"user.aginctus.resource": "infrastructure",
				"user.aginctus.role":     "herdr-client",
			},
			"aginctus-dev": {
				"user.aginctus.managed":  "true",
				"user.aginctus.resource": "workload",
				"user.aginctus.role":     "agent-workload",
				"user.aginctus.workload": "dev",
			},
		},
	}
}

func remoteConfig() sshaccess.Config {
	return sshaccess.Config{
		Operation:      sshaccess.Reconcile,
		ClientInstance: "aginctus-herdr",
		WorkloadID:     "dev",
		TargetInstance: "aginctus-dev",
		TargetAccount:  "agent",
	}
}

func TestFromConfigResolvesManagedWorkload(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	cfg, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}

	spec, err := FromConfig(cfg, "dev")
	if err != nil {
		t.Fatalf("FromConfig() error = %v", err)
	}
	if spec.WorkloadID != "dev" || spec.ClientInstance != "aginctus-herdr" || spec.TargetInstance != "aginctus-dev" || spec.TargetAccount != "agent" {
		t.Fatalf("spec = %#v", spec)
	}
}

func TestRuntimeReconcileCreatesReachableSavedMachine(t *testing.T) {
	incus := ownedIncus()
	var authorized string
	var wroteKnownHosts bool
	var probed bool
	var added bool
	staged := map[string]string{}

	incus.run = func(instance string, command []string, stdin string) (incusadapter.InstanceExecResult, error) {
		switch {
		case instance == "aginctus-herdr" && equalCommand(command, "aginctus-ssh-client-identity", "ensure"):
			return incusadapter.InstanceExecResult{
				Stdout: "generation\t1\nfingerprint\tSHA256:test\npublic_key\tssh-ed25519 AAAATEST aginctus:1\n",
			}, nil

		case instance == "aginctus-dev" && equalCommand(command, "aginctus-ssh-authorize", "reconcile", "agent"):
			authorized = stdin
			return incusadapter.InstanceExecResult{}, nil

		case instance == "aginctus-dev" && equalCommand(command, "cat", "/etc/ssh/ssh_host_ed25519_key.pub"):
			return incusadapter.InstanceExecResult{Stdout: "ssh-ed25519 HOSTKEY workload\n"}, nil

		case instance == "aginctus-herdr" && len(command) == 3 && equalCommand(command[:2], "test", "-f") && strings.Contains(command[2], "/known_hosts/"):
			return incusadapter.InstanceExecResult{ExitCode: 1}, nil

		case instance == "aginctus-herdr" && len(command) == 2 && command[0] == "mktemp":
			tmp := strings.Replace(command[1], "XXXXXX", "ABCDEF", 1)
			return incusadapter.InstanceExecResult{Stdout: tmp + "\n"}, nil
		case instance == "aginctus-herdr" && len(command) == 2 && command[0] == "tee":
			staged[command[1]] = stdin
			return incusadapter.InstanceExecResult{}, nil
		case instance == "aginctus-herdr" && len(command) == 3 && command[0] == "mv":
			if strings.Contains(command[2], "/known_hosts/") {
				if !strings.Contains(staged[command[1]], "aginctus-dev ssh-ed25519 HOSTKEY") {
					t.Fatalf("known_hosts content = %q", staged[command[1]])
				}
				wroteKnownHosts = true
			}
			return incusadapter.InstanceExecResult{}, nil

		case instance == "aginctus-herdr" && equalCommand(command, "ssh", "aginctus-dev", "true"):
			if !wroteKnownHosts {
				t.Fatal("SSH probe ran before host trust was published")
			}
			probed = true
			return incusadapter.InstanceExecResult{}, nil

		case instance == "aginctus-herdr" && equalCommand(command, "herdr", "machine", "list", "--json"):
			return incusadapter.InstanceExecResult{Stdout: "[]\n"}, nil

		case instance == "aginctus-herdr" && len(command) >= 4 && equalCommand(command[:3], "herdr", "machine", "add"):
			if !probed {
				t.Fatal("Herdr machine was added before SSH probe")
			}
			added = true
			return incusadapter.InstanceExecResult{}, nil

		case instance == "aginctus-herdr" && len(command) >= 4 && equalCommand(command[:3], "herdr", "machine", "status"):
			if !added {
				t.Fatal("Herdr status checked before machine add")
			}
			return incusadapter.InstanceExecResult{Stdout: "[]\n"}, nil

		default:
			return incusadapter.InstanceExecResult{}, nil
		}
	}

	if err := NewRuntime(incus).Reconcile(context.Background(), remoteConfig()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if authorized != "ssh-ed25519 AAAATEST aginctus:1\n" {
		t.Fatalf("authorization stdin = %q", authorized)
	}
	if strings.Contains(authorized, "PRIVATE") {
		t.Fatal("private material crossed to workload")
	}
	if !probed || !added {
		t.Fatalf("probed=%v added=%v", probed, added)
	}
}

func TestRuntimeInspectIsReadOnly(t *testing.T) {
	incus := ownedIncus()
	incus.run = func(instance string, command []string, _ string) (incusadapter.InstanceExecResult, error) {
		switch {
		case instance == "aginctus-herdr" && equalCommand(command, "aginctus-ssh-client-identity", "inspect"):
			return incusadapter.InstanceExecResult{
				Stdout: "generation\t1\nfingerprint\tSHA256:test\npublic_key\tssh-ed25519 AAAATEST aginctus:1\n",
			}, nil
		case instance == "aginctus-dev" && equalCommand(command, "aginctus-ssh-authorize", "inspect", "agent"):
			return incusadapter.InstanceExecResult{Stdout: "ssh-ed25519 AAAATEST aginctus:1\n"}, nil
		case instance == "aginctus-herdr" && equalCommand(command, "herdr", "machine", "list", "--json"):
			return incusadapter.InstanceExecResult{
				Stdout: `[{"id":"0123456789abcdef0123456789abcdef","label":"dev","target":"aginctus-dev","session":"default","enabled":true}]`,
			}, nil
		default:
			t.Fatalf("mutating or unexpected command invoked during inspect: %q %#v", instance, command)
			return incusadapter.InstanceExecResult{}, nil
		}
	}

	state, err := NewRuntime(incus).Inspect(context.Background(), remoteConfig())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !state.IdentityPresent || !state.AuthorizationPresent || !state.AuthorizationMatches || !state.MachinePresent {
		t.Fatalf("state = %#v", state)
	}
}

func TestRuntimeRejectsForeignWorkloadBeforeExec(t *testing.T) {
	incus := ownedIncus()
	incus.configs["aginctus-dev"]["user.aginctus.workload"] = "other"

	err := NewRuntime(incus).Reconcile(context.Background(), remoteConfig())
	if err == nil || !strings.Contains(err.Error(), "ownership mismatch") {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(incus.execs) != 0 {
		t.Fatalf("execs = %#v, want none", incus.execs)
	}
}

func TestRuntimeRevokeRemovesMachineBeforeAuthorization(t *testing.T) {
	incus := ownedIncus()
	removedMachine := false
	incus.run = func(instance string, command []string, _ string) (incusadapter.InstanceExecResult, error) {
		switch {
		case instance == "aginctus-herdr" && equalCommand(command, "herdr", "machine", "list", "--json"):
			return incusadapter.InstanceExecResult{
				Stdout: `[{"id":"0123456789abcdef0123456789abcdef","label":"dev","target":"aginctus-dev","session":"default","enabled":true}]`,
			}, nil
		case instance == "aginctus-herdr" && equalCommand(command, "herdr", "machine", "remove", "0123456789abcdef0123456789abcdef"):
			removedMachine = true
			return incusadapter.InstanceExecResult{}, nil
		case instance == "aginctus-dev" && equalCommand(command, "aginctus-ssh-authorize", "remove", "agent"):
			if !removedMachine {
				t.Fatal("authorization revoked before Herdr machine removal")
			}
			return incusadapter.InstanceExecResult{}, nil
		default:
			return incusadapter.InstanceExecResult{}, nil
		}
	}

	if err := NewRuntime(incus).Revoke(context.Background(), remoteConfig()); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if !removedMachine {
		t.Fatal("Herdr machine was not removed")
	}
}

func TestRuntimeRevokeReportsIncompleteAuthorizationOnGuestFailure(t *testing.T) {
	incus := ownedIncus()
	incus.run = func(instance string, command []string, _ string) (incusadapter.InstanceExecResult, error) {
		switch {
		case instance == "aginctus-herdr" && equalCommand(command, "herdr", "machine", "list", "--json"):
			return incusadapter.InstanceExecResult{Stdout: "[]\n"}, nil
		case instance == "aginctus-dev" && equalCommand(command, "aginctus-ssh-authorize", "remove", "agent"):
			return incusadapter.InstanceExecResult{}, errors.New("guest unavailable")
		default:
			return incusadapter.InstanceExecResult{}, nil
		}
	}

	err := NewRuntime(incus).Revoke(context.Background(), remoteConfig())
	if err == nil || !strings.Contains(err.Error(), "authorization revocation is incomplete") || !strings.Contains(err.Error(), "guest unavailable") {
		t.Fatalf("Revoke() error = %v", err)
	}
}

func TestPlanUsesInternalSSHAccessDriver(t *testing.T) {
	spec := Spec{WorkloadID: "dev", ClientInstance: "aginctus-herdr", TargetInstance: "aginctus-dev", TargetAccount: "agent"}
	plan, err := spec.Plan(sshaccess.Reconcile)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Driver != "ssh-access" {
		t.Fatalf("steps = %#v", plan.Steps)
	}
}

func equalCommand(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestRuntimeRejectsUnexpectedHostKeyChange(t *testing.T) {
	incus := ownedIncus()
	incus.run = func(instance string, command []string, stdin string) (incusadapter.InstanceExecResult, error) {
		switch {
		case instance == "aginctus-herdr" && equalCommand(command, "aginctus-ssh-client-identity", "ensure"):
			return incusadapter.InstanceExecResult{
				Stdout: "generation\t1\nfingerprint\tSHA256:test\npublic_key\tssh-ed25519 AAAATEST aginctus:1\n",
			}, nil
		case instance == "aginctus-dev" && equalCommand(command, "aginctus-ssh-authorize", "reconcile", "agent"):
			t.Fatal("authorization changed before host-key mismatch was rejected")
			return incusadapter.InstanceExecResult{}, nil
		case instance == "aginctus-dev" && equalCommand(command, "cat", "/etc/ssh/ssh_host_ed25519_key.pub"):
			return incusadapter.InstanceExecResult{Stdout: "ssh-ed25519 NEWKEY workload\n"}, nil
		case instance == "aginctus-herdr" && len(command) == 3 && equalCommand(command[:2], "test", "-f") && strings.Contains(command[2], "/known_hosts/"):
			return incusadapter.InstanceExecResult{}, nil
		case instance == "aginctus-herdr" && len(command) == 2 && command[0] == "cat" && strings.Contains(command[1], "/known_hosts/"):
			return incusadapter.InstanceExecResult{Stdout: "aginctus-dev ssh-ed25519 OLDKEY\n"}, nil
		default:
			if instance == "aginctus-herdr" && len(command) > 0 && command[0] == "ssh" {
				t.Fatal("SSH probe ran after host-key mismatch")
			}
			return incusadapter.InstanceExecResult{}, nil
		}
	}

	err := NewRuntime(incus).Reconcile(context.Background(), remoteConfig())
	if err == nil || !strings.Contains(err.Error(), "SSH host key changed") {
		t.Fatalf("Reconcile() error = %v", err)
	}
}
