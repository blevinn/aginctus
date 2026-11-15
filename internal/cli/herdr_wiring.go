package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
)

// Herdr command composition: guest transport remains in the CLI boundary.
// The operation package enforces lifecycle, ownership and pinned-host checks.
// Named adapters remain injectable for established negative-security tests.

// herdr_authorize.go — preexisting CLI adapter functions.
// HerdrAuthorizationGuestExec is the guest-helper transport. Commands are fixed
// and only public SSH key material passes through stdin. The private identity
// remains in the Herdr client. Host key trust is a separate operation.
var herdrAuthorizationGuestExec = func(ctx context.Context, instance string, stdin string, arguments ...string) (string, int, error) {
	args := append([]string{"exec", instance, "--project", "default", "--"}, arguments...)
	cmd := exec.CommandContext(ctx, "incus", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	// Capture stdout and stderr separately. The guest helpers emit a strict
	// tab- or whitespace-delimited protocol on stdout (key fields, generation,
	// pinned host key, SSH probe account) that the host-side code parses with
	// exact equality or regex matches. Any stderr chatter from `incus exec`
	// (warnings, progress lines, daemon advisories) would corrupt those parsers
	// if combined with stdout, so the helper deliberately discards stderr on
	// success and redacts it from errors.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return stdout.String(), 0, nil
	} else {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// Avoid leaking guest helper output into errors; it may contain keys.
			return "", exit.ExitCode(), fmt.Errorf("guest helper %q exited with status %d", arguments[0], exit.ExitCode())
		}
		return "", -1, fmt.Errorf("execute guest helper %q: %w", arguments[0], err)
	}
}


var managedClientPublicKeyPattern = herdrops.ManagedClientPublicKeyPattern
func parseHerdrPublicIdentity(output string) (string, error) { return herdrops.ParsePublicIdentity(output) }
func verifyManagedAuthorization(publicKey, installed string) error { return herdrops.VerifyManagedAuthorization(publicKey, installed) }

func requireRunningOwnedInstance(instances []inspectedInstance, name, resource, role, id string) error {
	status, err := findManagedInstance(instances, name, resource, role, id)
	if err != nil {
		return err
	}
	if !strings.EqualFold(status, "running") {
		return fmt.Errorf("managed instance %q is not running (state %q)", name, status)
	}
	return nil
}


// Preserve the CLI-local ownership test seam; the application operation must
// validate both running, managed instances before using any guest helper.
func verifyHerdrRunningPair(ctx context.Context, clientName, targetName, targetID string) error {
	instances, err := statusInstances(ctx)
	if err != nil {
		return fmt.Errorf("inspect Incus ownership: %w", err)
	}
	if err := requireRunningOwnedInstance(instances, clientName, "infrastructure", "herdr-client", ""); err != nil { return err }
	return requireRunningOwnedInstance(instances, targetName, "workload", "agent-workload", targetID)
}

func herdrActions() herdrops.Actions {
	return herdrops.Actions{
		AuthorizeExec: herdrAuthorizationGuestExec,
		TrustExec: herdrHostTrustGuestExec,
		MachineExec: herdrMachineGuestExec,
		CheckRunningPair: verifyHerdrRunningPair,
		PinHost: pinHerdrHost,
	}
}

func authorizeHerdrWorkload(ctx context.Context, cfg *config.Config, id string) error {
	return herdrops.AuthorizeWorkload(ctx, cfg, id, herdrActions())
}
func runHerdrAuthorize(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return herdrops.RunAuthorize(ctx, args, stdout, stderr, cfg, herdrActions())
}

// herdr_console.go — preexisting CLI adapter functions.
// herdrConsoleCommand aligns the CLI with the headless server systemd unit.
// Incus exec does not inherit the unit environment; without these paths Herdr
// can attach to a different root-owned session instead of the managed server.
func herdrConsoleCommand(ctx context.Context, instance string) *exec.Cmd {
	return exec.CommandContext(ctx, "incus", "exec", instance, "--project", "default", "--",
		"env", "XDG_CONFIG_HOME=/var/lib/herdr/config", "XDG_RUNTIME_DIR=/run/herdr",
		"XDG_STATE_HOME=/var/lib/herdr", "herdr")
}

// launchHerdrConsole runs an interactive command with the host terminal
// attached. No shell is involved; the client identity stays in the guest.
var launchHerdrConsole = func(ctx context.Context, instance string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := herdrConsoleCommand(ctx, instance)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("incus exec Herdr: %w", err)
	}
	return nil
}

func runHerdrConsole(ctx context.Context, args []string, out, stderr io.Writer, cfg *config.Config) int {
	return herdrops.RunConsole(ctx, args, os.Stdin, out, stderr, cfg, herdrops.ConsoleActions{
		Inspect: statusInstances,
		Find: findManagedInstance,
		Launch: launchHerdrConsole,
	})
}

// herdr_lifecycle.go — preexisting CLI adapter functions.
// Transition only an Aginctus-owned Herdr client in the default project.
// Reuse the injectable Incus command runner from workload lifecycle.
var transitionHerdrClient = func(ctx context.Context, spec herdrclient.Spec, action string) error {
	if action != "start" && action != "stop" {
		return fmt.Errorf("unsupported Herdr lifecycle action %q", action)
	}
	instances, err := statusInstances(ctx)
	if err != nil {
		return err
	}
	state, err := findManagedInstance(instances, spec.Name, "infrastructure", "herdr-client", "")
	if err != nil {
		return err
	}
	if state == "" {
		if action == "stop" {
			return nil
		}
		return fmt.Errorf("herdr client %q is absent; run herdr client apply first", spec.Name)
	}
	running := strings.EqualFold(state, "running")
	if (action == "start" && running) || (action == "stop" && !running) {
		return nil
	}
	return runWorkloadIncus(ctx, action, spec.Name, "--project", "default")
}

// herdr_pin_host.go — preexisting CLI adapter functions.
// Preserve the existing Incus guest-exec injection points for CLI tests.
var herdrHostTrustGuestExec = herdrAuthorizationGuestExec
var managedSSHHostAlias = herdrops.ManagedSSHHostAlias

func parseWorkloadHostKey(raw string) (string, error) { return herdrops.ParseWorkloadHostKey(raw) }
func parseManagementAddress(raw string) (string, error) { return herdrops.ParseManagementAddress(raw) }
func pinHerdrHost(ctx context.Context, cfg *config.Config, id string) error {
	return herdrops.PinHost(ctx, cfg, id, herdrActions())
}
func runHerdrPinHost(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return herdrops.RunPinHost(ctx, args, stdout, stderr, cfg, herdrActions())
}

// herdr_register.go — preexisting CLI adapter functions.
// The guest transport is intentionally independently replaceable for tests.
var herdrMachineGuestExec = herdrAuthorizationGuestExec

type savedHerdrMachine = herdrops.SavedHerdrMachine
func parseSavedHerdrMachines(raw string) ([]savedHerdrMachine, error) {
	return herdrops.ParseSavedMachines(raw)
}
func verifySavedHerdrMachine(machines []savedHerdrMachine, label, target string) (bool, error) {
	return herdrops.VerifySavedMachine(machines, label, target)
}
func registerHerdrMachine(ctx context.Context, cfg *config.Config, id string) error {
	return herdrops.RegisterMachine(ctx, cfg, id, herdrActions())
}
func runHerdrRegister(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	return herdrops.RunRegister(ctx, args, stdout, stderr, cfg, herdrActions())
}
