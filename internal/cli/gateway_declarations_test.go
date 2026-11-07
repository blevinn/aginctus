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
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
)

func TestConfiguredGatewayAssets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", root)
	file := os.Getenv("AGINCTUS_NIXOS_ASSET_FIXTURE")
	if file == "" {
		file = filepath.Join(t.TempDir(), "config.json")
		data := `{"gateway":{"providers":{"go":{"plugin":"opencode-go","config":{"models":{"coding":{"source":"openai/gpt-6-luna"}},"mcps":{"search":{"transport":"streamable-http","url":"https://tools.example.invalid/mcp","allowedTools":["search"]}}}}},"catalogs":{"base":{"models":{"coding":{"provider":"go","name":"coding"}},"mcps":{"docs":{"local":true,"name":"docs"}}},"child":{"inherits":[{"catalog":"base","models":{"coding":"dev.coding"},"mcps":{"docs":"tools.docs"}}]}}},"mcp":{"servers":{"docs":{"transport":"stdio","command":"/bin/docs","allowedTools":["search"]}}}}`
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errs bytes.Buffer
		args = append([]string{"--configuration-file", file, "gateway"}, args...)
		code := Run(context.Background(), args, &out, &errs, nil, &config.Loader{})
		return code, out.String() + errs.String()
	}
	for _, args := range [][]string{
		{"provider", "show", "go"}, {"model", "list", "--provider", "go"},
		{"model", "render", "coding", "--provider", "go"},
		{"mcp", "show", "--local", "docs"}, {"mcp", "show", "--provider", "go", "search"},
	} {
		if code, out := run(args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	code, out := run("catalog", "show", "child")
	if code != 0 || !strings.Contains(out, `"dev.coding"`) || !strings.Contains(out, `"tools.docs"`) || !strings.Contains(out, `"provider": "go"`) {
		t.Fatalf("inherited selection: %d %s", code, out)
	}
	for _, args := range [][]string{
		{"provider", "apply", "opencode-go", "--provider", "go"},
		{"catalog", "apply", "base", "--config", `{}`},
		{"mcp", "apply", "docs", "--config", `{"transport":"stdio","command":"/bin/other"}`},
	} {
		if code, out := run(args...); code == 0 || !strings.Contains(out, "configuration-owned") {
			t.Fatalf("managed mutation: %d %s", code, out)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("read or rejected mutation initialized state: %v", err)
	}
	if code, out := run("catalog", "apply", "local", "--config", `{"inherits":[{"catalog":"child","models":{"dev.coding":"local.coding"}}]}`); code != 0 {
		t.Fatalf("local catalog: %d %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "aginctus", "gateway", "assets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var assets gatewaycontrol.Assets
	if json.Unmarshal(data, &assets) != nil || len(assets.Catalogs) != 1 || len(assets.MCPs) != 0 {
		t.Fatal("configured declarations were copied to mutable state")
	}
	if code, out := run("catalog", "show", "local"); code != 0 || !strings.Contains(out, "local.coding") {
		t.Fatalf("local references configured catalog: %d %s", code, out)
	}
}
