package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/siwc"
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

func TestSIWCProviderInstancesAndCatalogBinding(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "credentials")
	run := func(args ...string) {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), append([]string{"gateway"}, args...), &out, &stderr, nil, nil); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
	}
	data, err := json.Marshal(map[string]any{"state_dir": root, "models": map[string]any{"fixture-model": map[string]string{"source": "fixture-model"}}})
	if err != nil {
		t.Fatal(err)
	}
	run("plugin", "show", "openai-siwc")
	run("provider", "apply", "openai-siwc", "--provider", "personal", "--config", string(data))
	run("catalog", "apply", "upstream", "--config", `{"models":{"coding":{"provider":"personal","name":"fixture-model"}}}`)
	run("catalog", "apply", "coding", "--config", `{"inherits":[{"catalog":"upstream","models":{"coding":"siwc-coding"}}]}`)
	binding := siwc.Binding{Route: "siwc-coding"}
	directory, err := resolveSIWCCatalogBinding("coding", &binding)
	if err != nil || directory != root || binding.Account != "personal" || binding.Model != "fixture-model" {
		t.Fatalf("catalog binding lost provider identity: %+v %s %v", binding, directory, err)
	}
	binding.Route = "siwc-missing"
	if _, err := resolveSIWCCatalogBinding("coding", &binding); err == nil {
		t.Fatal("accepted unexposed catalog alias")
	}
	run("provider", "apply", "openai-siwc", "--provider", "work", "--config", string(data))
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"gateway", "provider", "login"}, &out, &stderr, nil, nil); code != 1 || !strings.Contains(stderr.String(), "explicitly") {
		t.Fatalf("ambiguous provider login: %d %s", code, stderr.String())
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("registry operations or ambiguous login initialized credentials")
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
		{"gateway", "provider", "login", "--help"},
		{"help", "gateway", "provider", "login"},
		{"gateway", "siwc", "logout", "--state-dir", dir},
		{"gateway", "siwc", "status", "--state-dir", dir, "--unknown"},
		{"gateway", "siwc", "status", "--state-dir", dir, "extra"},
		{"gateway", "siwc", "logout", "--state-dir", dir, "--account", "../foreign"},
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
