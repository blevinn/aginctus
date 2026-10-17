package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/modelregistry"
	"github.com/blevinn/aginctus/internal/providerregistry"
)

func TestRenderRegisteredChatGPTModelIsReadOnly(t *testing.T) {
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

	if status, _, msg := run("model", "coding", "render"); status != 1 || !strings.Contains(msg, "not registered") {
		t.Fatalf("missing local model: status=%d stderr=%q", status, msg)
	}
	if status, _, msg := run("provider", "chatgpt", "register", "--alias=personal"); status != 0 {
		t.Fatalf("provider register: status=%d stderr=%q", status, msg)
	}
	if status, _, msg := run("model", "coding", "register", "--provider=personal", "--source=chatgpt/unverified-example"); status != 0 {
		t.Fatalf("model register: status=%d stderr=%q", status, msg)
	}

	modelPath, err := modelregistry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	providerPath, err := providerregistry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	beforeModels, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeProviders, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}

	status, output, notice := run("model", "coding", "render")
	if status != 0 || !strings.Contains(notice, "for review only") || !strings.Contains(notice, "remain inactive") {
		t.Fatalf("render status=%d stderr=%q", status, notice)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("invalid model JSON: %v", err)
	}
	if got["model_name"] != "coding" || len(got) != 3 {
		t.Fatalf("unexpected rendered model metadata: %#v", got)
	}
	params, ok := got["litellm_params"].(map[string]any)
	if !ok || params["model"] != "chatgpt/unverified-example" || len(params) != 1 {
		t.Fatalf("unexpected native provider route: %#v", params)
	}
	info, ok := got["model_info"].(map[string]any)
	if !ok || info["mode"] != "responses" || len(info) != 1 {
		t.Fatalf("not an explicit Responses route: %#v", info)
	}
	if strings.Contains(strings.ToLower(output), "token") || strings.Contains(strings.ToLower(output), "api_key") ||
		strings.Contains(strings.ToLower(output), "credential") {
		t.Fatal("rendered output references authentication state")
	}
	afterModels, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	afterProviders, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeModels, afterModels) || !bytes.Equal(beforeProviders, afterProviders) {
		t.Fatal("read-only rendering changed registration state")
	}
	if status, _, _ := run("model", "coding", "render", "--enable"); status != 2 {
		t.Fatalf("unexpected flag accepted: %d", status)
	}
}

func TestRenderRegisteredChatGPTModelRejectsAmbiguousCredentials(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	providersPath, err := providerregistry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	modelsPath, err := modelregistry.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	_, err = providerregistry.Register(providersPath, providerregistry.Provider{Type: "chatgpt", Alias: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = modelregistry.Register(modelsPath, modelregistry.Model{Name: "coding", Provider: "personal", Source: "chatgpt/unverified"})
	if err != nil {
		t.Fatal(err)
	}
	// A second provider identity cannot safely map to the one global native
	// CHATGPT_TOKEN_DIR/auth.json in pinned LiteLLM.
	_, err = providerregistry.Register(providersPath, providerregistry.Provider{Type: "chatgpt", Alias: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderRegisteredChatGPTModel("coding"); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("accepted ambiguous credential identity: %v", err)
	}
	// Even a single provider identity cannot be inferred as another alias.
	if err := os.Remove(providersPath); err != nil {
		t.Fatal(err)
	}
	if _, err := providerregistry.Register(providersPath, providerregistry.Provider{Type: "chatgpt", Alias: "other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := renderRegisteredChatGPTModel("coding"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("accepted wrong provider identity: %v", err)
	}
	if err := os.Chmod(providersPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := renderRegisteredChatGPTModel("coding"); err == nil {
		t.Fatal("accepted insecure provider registry")
	}
}
