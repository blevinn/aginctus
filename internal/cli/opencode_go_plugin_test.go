package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestOpenCodeGoPluginRendering(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := Run(context.Background(), append([]string{"gateway"}, args...), &out, &errs, nil, &config.Loader{})
		return code, out.String() + errs.String()
	}
	for _, provider := range []string{"first", "second"} {
		code, out := run("provider", "apply", "opencode-go", "--provider", provider,
			"--config", `{"models":{"coding":{"source":"openai/grok-4.7"}}}`)
		if code != 0 {
			t.Fatalf("apply: %d %s", code, out)
		}
	}
	if code, out := run("model", "render", "coding"); code == 0 || !strings.Contains(out, "ambiguous") {
		t.Fatalf("ambiguous model: %d %s", code, out)
	}
	code, out := run("model", "render", "coding", "--provider", "second")
	if code != 0 || !strings.Contains(out, `"api_base": "https://opencode.ai/inference/go/openai/v1"`) ||
		!strings.Contains(out, `"mode": "responses"`) || strings.Contains(out, "api_key") {
		t.Fatalf("plugin rendering: %d %s", code, out)
	}
	if code, out := run("provider", "apply", "opencode-go", "--provider", "bad",
		"--config", `{"models":{"coding":{"source":"openai/not-a-supported-model"}}}`); code == 0 {
		t.Fatalf("unsupported source: %s", out)
	}
}
