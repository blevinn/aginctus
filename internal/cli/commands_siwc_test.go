package cli

import (
	"bytes"
	"context"
	"os"
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
	if !strings.Contains(out.String(), "gateway siwc --help") {
		t.Fatal("SIWC enrollment not discoverable")
	}
}

func TestSIWCStateCommandsNeedNoPythonOrHostConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	var out, stderr bytes.Buffer
	args := []string{"gateway", "siwc", "status", "--state-dir", filepath.Join(dir, "state")}
	if code := Run(context.Background(), args, &out, &stderr, nil, nil); code != 0 {
		t.Fatalf("isolated state inspection: %d %s", code, stderr.String())
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatal("unexpected initial state")
	}
}

func TestSIWCCobraHelpAndSyntaxDoNotTouchState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{
		{"gateway", "siwc", "--help"},
		{"gateway", "siwc", "login", "--help"},
		{"help", "gateway", "siwc", "login"},
		{"gateway", "siwc", "login", "--state-dir", dir},
		{"gateway", "siwc", "status", "--state-dir", dir, "--unknown"},
		{"gateway", "siwc", "status", "--state-dir", dir, "extra"},
		{"gateway", "siwc", "login", "--state-dir", dir, "--account", "../foreign"},
	} {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), args, &out, &stderr, nil, nil)
		if args[len(args)-1] == "--help" || args[0] == "help" {
			if code != 0 || !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("help: %d %s", code, stderr.String())
			}
		} else if code != 2 {
			t.Fatalf("invalid syntax: %d %s", code, stderr.String())
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("help or invalid syntax touched state")
		}
	}
}
