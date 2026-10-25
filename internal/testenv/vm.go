package testenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Scenario is one of the pinned, explicitly opt-in NixOS VM test derivations.
// This manifest is shared with the testenv CLI and the MCP discovery surface.
type Scenario struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Description string `json:"description"`
}

var vmScenarios = []Scenario{
	{Name: "smoke", Target: "vm-harness-smoke", Description: "Real guest Incus and Herdr image smoke"},
	{Name: "storage-network", Target: "vm-harness-storage-network", Description: "Guest-local Btrfs storage and management bridge"},
	{Name: "runtime-lifecycle", Target: "vm-harness-runtime-lifecycle", Description: "Real Herdr/OpenCode provisioning and reconciliation"},
	{Name: "gateway-topology", Target: "vm-harness-gateway-topology", Description: "Real guest-local LiteLLM/Postgres and Herdr/OpenCode topology (OCI pulls required; no inference)"},
	{Name: "synthetic-gateway", Target: "vm-harness-synthetic-gateway", Description: "Real guest LiteLLM/Postgres with scoped-key synthetic Responses upstream (no agent tool proof)"},
	{Name: "ownership-recovery", Target: "vm-harness-ownership-recovery", Description: "Foreign resource denial and guest daemon recovery"},
}

func VMScenarios() []Scenario {
	return append([]Scenario(nil), vmScenarios...)
}

func VMScenario(name string) (Scenario, error) {
	for _, scenario := range vmScenarios {
		if name == scenario.Name {
			return scenario, nil
		}
	}
	return Scenario{}, fmt.Errorf("unknown VM scenario %q", name)
}

// VMRunnerRequest is a fixed-argv invocation of the existing batch harness.
// It is operator-only: the Python script comes from the source checkout.
// Untrusted-agent execution must wait for the installed immutable supervisor.
type VMRunnerRequest struct {
	Program string
	Args    []string
	Dir     string
}

// VMTestRequest resolves a local checkout and a fixed scenario without ever
// accepting arbitrary shell strings, Nix attributes or script paths.
func VMTestRequest(root, scenarioName string) (VMRunnerRequest, error) {
	if _, err := VMScenario(scenarioName); err != nil {
		return VMRunnerRequest{}, err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return VMRunnerRequest{}, fmt.Errorf("resolve repository: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() {
		return VMRunnerRequest{}, errors.New("testenv requires a local Aginctus checkout")
	}
	script := filepath.Join(absolute, "scripts", "vm_harness", "runner.py")
	fi, err := os.Lstat(script)
	if err != nil || !fi.Mode().IsRegular() {
		return VMRunnerRequest{}, errors.New("testenv requires the repository VM runner (regular file)")
	}
	resolvedRoot, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return VMRunnerRequest{}, errors.New("cannot resolve repository root")
	}
	resolvedScript, err := filepath.EvalSymlinks(script)
	if err != nil || !filepath.IsLocal(strings.TrimPrefix(resolvedScript, resolvedRoot+string(os.PathSeparator))) || !strings.HasPrefix(resolvedScript, resolvedRoot+string(os.PathSeparator)) {
		return VMRunnerRequest{}, errors.New("testenv VM runner escapes repository root")
	}
	// The underlying runner resolves its own repository root and produces the
	// same run-scoped evidence as the existing just test-vm-* commands.
	return VMRunnerRequest{
		Program: "python3",
		Args:    []string{script, "test", scenarioName},
		Dir:     absolute,
	}, nil
}

// RunVMTest reuses the already-validated batch runner. This is not a
// persistent VM or a safe transport for arbitrary agent-supplied commands.
// Invoke only from a dedicated unprivileged test worker without pilot access.
func RunVMTest(ctx context.Context, root, scenarioName string, stdout, stderr io.Writer) error {
	request, err := VMTestRequest(root, scenarioName)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, request.Program, request.Args...)
	cmd.Dir = request.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// The Python runner further sanitizes inherited environment before Nix.
	// Avoid passing local Incus and live acceptance selectors even to Python.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"LANG=" + os.Getenv("LANG"),
		"LC_ALL=" + os.Getenv("LC_ALL"),
		"TERM=" + os.Getenv("TERM"),
		"AGINCTUS_INCUS_INTEGRATION=0",
		"AGINCTUS_INCUS_DISPOSABLE=NO",
		"AGINCTUS_ACCEPTANCE_LIVE=",
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("VM scenario %s: %w", scenarioName, err)
	}
	return nil
}
