package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSIWCBundleDeliversCanonicalSourcesWithoutConfigOrSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"gateway", "siwc", "bundle", dir}, &out, &stderr, nil, nil); code != 0 {
		t.Fatalf("bundle: %d %s", code, stderr.String())
	}
	for _, name := range []string{"siwc.py", "litellm_siwc.py"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("bundle file %s: %v", name, err)
		}
	}
	if code := Run(context.Background(), []string{"gateway", "siwc", "bundle", dir}, &out, &stderr, nil, nil); code == 0 {
		t.Fatal("bundle overwrote existing directory")
	}
}

func TestSIWCRejectsHostOverridesAndDocumentsCommands(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--config", "gateway.id=foreign", "gateway", "siwc", "status"}, &out, &stderr, nil, nil); code != 2 {
		t.Fatalf("host override: %d %s", code, stderr.String())
	}
	printGatewayUsage(&out)
	if !strings.Contains(out.String(), "gateway siwc --state-dir DIRECTORY login") {
		t.Fatal("SIWC enrollment not discoverable")
	}
}

func TestSIWCStateCommandsIgnoreWorkspacePythonModules(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python runtime is supplied by the development shell and packaged CLI")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "argparse.py"), []byte("raise RuntimeError('workspace module imported')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	args := []string{"--state-dir", filepath.Join(dir, "state"), "status"}
	if code := runSIWC(context.Background(), args, &out, &stderr); code != 0 {
		t.Fatalf("isolated state inspection: %d %s", code, stderr.String())
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatal("unexpected initial state")
	}
}
