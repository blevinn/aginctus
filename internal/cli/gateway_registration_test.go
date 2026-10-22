package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestGatewayRegistrationDryRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Dry-run preview matches the commit path: the provider alias must
	// already exist in the local provider registry. Pre-register the
	// chatgpt alias so the model-register preview cases have a valid
	// provider to resolve.
	if got := runGateway(context.Background(),
		[]string{"provider", "chatgpt", "register"},
		&bytes.Buffer{}, &bytes.Buffer{}, nil, cfg); got != 0 {
		t.Fatalf("seed chatgpt provider: %d", got)
	}
	cases := []struct {
		args   []string
		want   string
		status int
	}{
		{[]string{"provider", "chatgpt", "register", "--dry-run"}, "\"alias\": \"chatgpt\"", 0},
		{[]string{"provider", "chatgpt", "register", "--alias=personal", "--dry-run"}, "\"alias\": \"personal\"", 0},
		{[]string{"model", "chatgpt/gpt-6-sol", "register", "--dry-run"}, "\"mode\": \"responses\"", 0},
		{[]string{"model", "coding", "register", "--source=chatgpt/gpt-6-sol", "--provider=chatgpt", "--dry-run"}, "\"model_name\": \"coding\"", 0},
		{[]string{"provider", "chatgpt", "register"}, "already registered", 0},
		{[]string{"model", "chatgpt/gpt-6-sol", "register"}, "registered locally", 0},
		{[]string{"model", "chatgpt/gpt-6-sol", "register"}, "already registered", 0},
		{[]string{"model", "bad", "register", "--source=openai/gpt-5", "--dry-run"}, "--provider=ALIAS", 2},
		// Defaulted chatgpt provider alias must be verified against the
		// local registry, not silently accepted. The seed step above
		// registered ``chatgpt``; this second dry-run uses an unregistered
		// ``--source=openai/...`` so it should fail to resolve a provider.
		{[]string{"model", "untouched", "register", "--source=openai/gpt-5", "--dry-run"}, "--provider=ALIAS", 2},
		// Dry-run with an explicit but unregistered alias is rejected
		// consistently with the commit path.
		{[]string{"model", "missing", "register", "--source=chatgpt/foo", "--provider=ghost", "--dry-run"}, "model provider alias", 2},
	}
	for _, tc := range cases {
		var out, errs bytes.Buffer
		got := runGateway(context.Background(), tc.args, &out, &errs, nil, cfg)
		if got != tc.status {
			t.Fatalf("%v: got %d stderr=%s", tc.args, got, errs.String())
		}
		if !strings.Contains(out.String()+errs.String(), tc.want) {
			t.Fatalf("%v: expected %q in %q %q", tc.args, tc.want, out.String(), errs.String())
		}
	}
}

func TestProviderListAndShow(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	check := func(args []string, want string, status int) {
		t.Helper()
		var out, errs bytes.Buffer
		got := runGateway(context.Background(), args, &out, &errs, nil, cfg)
		if got != status || !strings.Contains(out.String()+errs.String(), want) {
			t.Fatalf("%v got %d out %q err %q", args, got, out.String(), errs.String())
		}
	}
	check([]string{"provider", "personal", "show"}, "not registered", 1)
	check([]string{"provider", "chatgpt", "register", "--alias=personal"}, "registered locally", 0)
	check([]string{"provider", "list"}, "personal", 0)
	check([]string{"provider", "personal", "show"}, "authentication pending", 0)
}

func TestGatewayModelRegistryAndAliasPreflight(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	check := func(args []string, want string, status int) {
		t.Helper()
		var out, errs bytes.Buffer
		got := runGateway(context.Background(), args, &out, &errs, nil, cfg)
		if got != status || !strings.Contains(out.String()+errs.String(), want) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q, expected %q", args, got, out.String(), errs.String(), want)
		}
	}
	check([]string{"model", "list"}, "", 0)
	check([]string{"model", "coding", "show"}, "not registered", 1)
	check([]string{"model", "coding", "register", "--source=chatgpt/sample", "--provider=personal"}, "not registered locally", 1)
	check([]string{"provider", "chatgpt", "register", "--alias=personal"}, "registered locally", 0)
	check([]string{"model", "coding", "register", "--source=chatgpt/sample", "--provider=personal", "--dry-run"}, "\"model_name\": \"coding\"", 0)
	check([]string{"model", "coding", "register", "--source=chatgpt/sample", "--provider=personal"}, "authorization and activation pending", 0)
	check([]string{"model", "coding", "register", "--source=chatgpt/sample", "--provider=personal"}, "already registered", 0)
	check([]string{"model", "list"}, "coding\tchatgpt/sample\tpersonal", 0)
	check([]string{"model", "coding", "show"}, "authorization and activation pending", 0)
	check([]string{"model", "coding", "register", "--source=chatgpt/different", "--provider=personal"}, "another registration", 1)
	check([]string{"model", "../escape", "register", "--source=chatgpt/sample", "--provider=personal"}, "invalid chatgpt", 2)
	check([]string{"model", "wrong", "register", "--source=openai/model", "--provider=personal"}, "invalid chatgpt", 2)
}

func TestOpenCodeGoRegistrationOffline(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := runGateway(context.Background(), args, &out, &errs, nil, cfg)
		return code, out.String(), errs.String()
	}
	if code, preview, errs := run("provider", "opencode-go", "register", "--alias=go", "--dry-run"); code != 0 ||
		!strings.Contains(preview, "opencode-go") {
		t.Fatalf("provider dry run: code=%d output=%q err=%q", code, preview, errs)
	}
	if code, out, errs := run("provider", "list"); code != 0 || out != "" {
		t.Fatalf("dry run mutated provider registry: %d %q %q", code, out, errs)
	}
	if code, _, errs := run("provider", "opencode-go", "register", "--alias=go"); code != 0 {
		t.Fatalf("provider registration: %d %q", code, errs)
	}
	if code, out, _ := run("provider", "list"); code != 0 || !strings.Contains(out, "go\topencode-go\t") {
		t.Fatalf("provider list: %d %q", code, out)
	}
	if code, out, _ := run("provider", "go", "show"); code != 0 || !strings.Contains(out, "authentication pending") {
		t.Fatalf("provider show: %d %q", code, out)
	}
	for _, source := range []string{
		"openai/gpt-6-luna", "openai/gpt-5.6-luna", "openai/grok-4.7",
		"openai/muse-spark-1.3-contributor", "openai/muse-spark-1.2-contributor",
	} {
		if code, preview, errs := run("model", "coding", "register", "--provider=go", "--source="+source, "--dry-run"); code != 0 ||
			!strings.Contains(preview, "https://opencode.ai/inference/go/openai/v1") {
			t.Fatalf("Go model %q dry-run: code=%d preview=%q err=%q", source, code, preview, errs)
		}
	}
	if code, out, _ := run("model", "list"); code != 0 || out != "" {
		t.Fatalf("dry-run mutated model registry: %d %q", code, out)
	}
	for _, tc := range []struct {
		args   []string
		status int
	}{
		{[]string{"model", "coding", "register", "--source=openai/gpt-6-luna", "--provider=missing", "--dry-run"}, 2},
		{[]string{"model", "coding", "register", "--source=openai/gpt-4o", "--provider=go", "--dry-run"}, 2},
		{[]string{"model", "coding", "register", "--source=chatgpt/gpt-5", "--provider=go", "--dry-run"}, 2},
		{[]string{"model", "../bad", "register", "--source=openai/gpt-6-luna", "--provider=go", "--dry-run"}, 2},
		{[]string{"provider", "opencode-go", "register", "--alias=../bad"}, 2},
		{[]string{"provider", "anthropic", "register"}, 2},
	} {
		if code, _, _ := run(tc.args...); code != tc.status {
			t.Fatalf("unexpected status for %v: %d", tc.args, code)
		}
	}
	if code, _, errs := run("model", "coding", "register", "--provider=go", "--source=openai/gpt-6-luna"); code != 0 {
		t.Fatalf("model registration: %d %q", code, errs)
	}
	if code, out, _ := run("model", "coding", "register", "--provider=go", "--source=openai/gpt-6-luna"); code != 0 ||
		!strings.Contains(out, "already registered") {
		t.Fatalf("model idempotency: %d %q", code, out)
	}
	if code, out, _ := run("model", "coding", "show"); code != 0 || !strings.Contains(out, "openai/gpt-6-luna") {
		t.Fatalf("model show: %d %q", code, out)
	}
}

func TestOpenCodeGoHelpIsDiscoverable(t *testing.T) {
	for _, usage := range []func(io.Writer){printGatewayUsage, printUsage} {
		var out bytes.Buffer
		usage(&out)
		if !strings.Contains(out.String(), "gateway provider opencode-go register") ||
			!strings.Contains(out.String(), "--source=chatgpt/MODEL|openai/MODEL") {
			t.Fatalf("missing OpenCode Go CLI usage: %s", out.String())
		}
	}
}
