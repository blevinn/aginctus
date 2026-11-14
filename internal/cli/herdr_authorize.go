package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	herdrops "github.com/blevinn/aginctus/internal/cli/operations/herdr"
	"github.com/blevinn/aginctus/internal/config"
)

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
