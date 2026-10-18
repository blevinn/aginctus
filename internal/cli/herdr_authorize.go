package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/workload"
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

var managedClientPublicKeyPattern = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/=]+ aginctus:[1-9][0-9]*$`)

func parseHerdrPublicIdentity(output string) (string, error) {
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
	if generation == "" || !managedClientPublicKeyPattern.MatchString(publicKey) ||
		!strings.HasSuffix(publicKey, " aginctus:"+generation) {
		return "", fmt.Errorf("missing or invalid managed client public key")
	}
	return publicKey, nil
}

func verifyManagedAuthorization(publicKey, installed string) error {
	if strings.TrimSpace(installed) != publicKey {
		return fmt.Errorf("workload has different managed authorization; refusing automatic credential replacement")
	}
	return nil
}

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

// authorizeHerdrWorkload is deliberately limited to client public-key delivery.
// It does not claim a verified SSH connection or publish a Herdr machine.
// This initial CLI transport uses the existing Incus exec boundary; moving the
// helper calls into the Incus Go adapter is tracked by the wider SSH lifecycle.
func authorizeHerdrWorkload(ctx context.Context, cfg *config.Config, id string) error {
	client, err := herdrclient.FromConfig(cfg)
	if err != nil {
		return fmt.Errorf("Herdr client configuration: %w", err)
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

	identity, _, err := herdrAuthorizationGuestExec(ctx, client.Name, "", "aginctus-ssh-client-identity", "inspect")
	if err != nil {
		return fmt.Errorf("client identity must already exist: %w", err)
	}
	key, err := parseHerdrPublicIdentity(identity)
	if err != nil {
		return err
	}

	installed, status, inspectErr := herdrAuthorizationGuestExec(ctx, target.Name, "", "aginctus-ssh-authorize", "inspect", "agent")
	if inspectErr != nil && status != 3 {
		return fmt.Errorf("inspect workload authorization: %w", inspectErr)
	}
	if inspectErr == nil {
		return verifyManagedAuthorization(key, installed)
	}
	// Code 3 is the documented 'not provisioned' result for inspect only.
	if _, _, err := herdrAuthorizationGuestExec(ctx, target.Name, key+"\n", "aginctus-ssh-authorize", "reconcile", "agent"); err != nil {
		return fmt.Errorf("reconcile workload authorization: %w", err)
	}
	installed, _, err = herdrAuthorizationGuestExec(ctx, target.Name, "", "aginctus-ssh-authorize", "inspect", "agent")
	if err != nil {
		return fmt.Errorf("verify workload authorization: %w", err)
	}
	return verifyManagedAuthorization(key, installed)
}

func runHerdrAuthorize(ctx context.Context, args []string, stdout, stderr io.Writer, cfg *config.Config) int {
	if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, "Usage: aginctus herdr authorize <workload-id>")
		return 2
	}
	if err := authorizeHerdrWorkload(ctx, cfg, args[0]); err != nil {
		fmt.Fprintf(stderr, "Herdr workload authorization: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Herdr public-key authorization for workload %q: verified (remote SSH not yet probed)\n", args[0])
	return 0
}
