package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

func TestPluginInventoryExplicitStructuredOutput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		format string
	}{
		{[]string{"--output=json", "plugins", "inventory"}, "json"},
		{[]string{"plugins", "list", "-o", "json"}, "json"},
		{[]string{"-o", "yaml", "plugins", "inventory"}, "yaml"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tc.args, &stdout, &stderr, nil, nil)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if tc.format == "json" {
				var result api.PluginList
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Kind != "PluginList" || result.APIVersion != api.APIVersionV1Alpha1 || result.Items == nil {
					t.Fatalf("invalid typed inventory: %#v", result)
				}
			} else if !strings.Contains(stdout.String(), "kind: PluginList") || !strings.Contains(stdout.String(), "items: []") {
				t.Fatalf("unexpected YAML: %q", stdout.String())
			}
		})
	}
}

func TestPluginInventoryLegacyDefaultAndExplicitHuman(t *testing.T) {
	for _, args := range [][]string{
		{"plugins", "inventory"},
		{"plugins", "inventory", "-o", "table"},
		{"plugins", "inventory", "-o", "wide"},
		{"plugins", "inventory", "-o", "name"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr, nil, nil); code != 0 || stderr.Len() != 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
		if len(args) == 2 && !strings.Contains(stdout.String(), "\"schema_version\"") {
			t.Fatalf("legacy inventory format changed: %q", stdout.String())
		}
		if len(args) > 2 && args[3] == "table" && !strings.Contains(stdout.String(), "NAME") {
			t.Fatalf("table missing header: %q", stdout.String())
		}
		if len(args) > 2 && args[3] == "wide" && !strings.Contains(stdout.String(), "CAPABILITIES") {
			t.Fatalf("wide missing header: %q", stdout.String())
		}
	}
}

func TestExplicitUnsupportedOutputFailsBeforeConfig(t *testing.T) {
	for _, args := range [][]string{
		{"-o", "json", "config", "show"},
		{"config", "show", "--output=yaml"},
		{"-o", "invalid", "plugins", "inventory"},
		{"-o", "", "plugins", "inventory"},
	} {
		var stdout, stderr bytes.Buffer
		loader := &fakeConfigLoader{err: context.Canceled}
		code := Run(context.Background(), args, &stdout, &stderr, nil, loader)
		if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "output") {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
		if strings.Contains(stderr.String(), "configuration:") {
			t.Fatalf("loaded config despite invalid output request: %q", stderr.String())
		}
	}
}
