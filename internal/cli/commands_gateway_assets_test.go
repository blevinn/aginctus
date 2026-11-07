package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestGatewayAssetCommandsAndSelectors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	run := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), append([]string{"gateway"}, args...), &out, &stderr, nil, &config.Loader{}); code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
		return out.String()
	}
	if !strings.Contains(run("plugin", "list"), "opencode-go") {
		t.Fatal("missing built-in plugin")
	}
	run("plugin", "show", "--plugin", "chatgpt")
	run("provider", "apply", "--plugin", "chatgpt", "--provider", "personal", "--config", `{"models":{"coding":{"source":"chatgpt/fixture-model"}}}`)
	run("provider", "show", "--provider", "personal")
	run("model", "list", "--provider", "personal")
	for _, args := range [][]string{{"model", "show", "personal", "coding"}, {"model", "show", "--provider", "personal", "--model", "coding"}, {"model", "show", "personal", "--model", "coding"}} {
		if !strings.Contains(run(args...), "fixture-model") {
			t.Fatal("model not found")
		}
	}
	run("mcp", "apply", "--local", "--mcp", "search", "--config", `{"transport":"streamable-http","url":"https://tools.example.invalid/mcp"}`)
	run("mcp", "show", "--local", "search")
	run("provider", "apply", "chatgpt", "--provider", "local")
	var listed struct {
		Local     map[string]json.RawMessage `json:"local"`
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal([]byte(run("mcp", "list")), &listed); err != nil || listed.Local["search"] == nil || listed.Providers["local"] == nil {
		t.Fatal("provider named local shadowed the local MCP namespace")
	}
	run("catalog", "apply", "work", "--config", `{"models":{"dev.coding":{"provider":"personal","name":"coding"}},"mcps":{"tools.search":{"local":true,"name":"search"}}}`)
	if !strings.Contains(run("catalog", "show", "--catalog", "work", "--model=dev.coding"), "coding") {
		t.Fatal("model filter lost selection")
	}
	run("catalog", "show", "work", "--mcp")
}

func TestGatewayAssetSyntaxAndHelpStayOffline(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	t.Setenv("XDG_STATE_HOME", root)
	for _, args := range [][]string{
		{"plugin", "show"}, {"provider", "show", "one", "two"}, {"provider", "apply", "chatgpt", "--plugin", "chatgpt"},
		{"model", "show", "--provider", "p"}, {"mcp", "apply", "m", "--config", "{}", "--config-file", "file"},
		{"mcp", "show", "x", "--provider", "p", "--local"}, {"catalog", "apply", "c"}, {"catalog", "show", "c", "--model", "--mcp"},
	} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), append([]string{"gateway"}, args...), &out, &stderr, nil, nil); code != 2 {
			t.Fatalf("syntax %v: %d %s", args, code, stderr.String())
		}
	}
	for _, family := range []string{"plugin", "provider", "model", "mcp", "catalog"} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"gateway", family, "--help"}, &out, &stderr, nil, nil); code != 0 || !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("help %s: %d %s", family, code, stderr.String())
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("syntax/help touched state")
	}
}
