package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatGPTRegistration(t *testing.T) {
	b, err := ChatGPTModelRegistration("aginctus-chatgpt", "chatgpt/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["model_name"] != "aginctus-chatgpt" {
		t.Fatal(got)
	}
	p := got["litellm_params"].(map[string]any)
	if p["model"] != "chatgpt/gpt-5" || len(p) != 1 {
		t.Fatal(p)
	}
	i := got["model_info"].(map[string]any)
	if i["mode"] != "responses" {
		t.Fatal(i)
	}
	if strings.Contains(string(b), "token") || strings.Contains(string(b), "api_key") {
		t.Fatal("secret reference in registration")
	}
}
func TestChatGPTRegistrationInvalid(t *testing.T) {
	for _, tc := range []struct{ alias, model string }{
		{"", "chatgpt/gpt-5"}, {"../escape", "chatgpt/gpt-5"}, {"ok", "openai/gpt-5"}, {"ok", "chatgpt/"}, {"ok", "chatgpt/a b"},
	} {
		if _, err := ChatGPTModelRegistration(tc.alias, tc.model); err == nil {
			t.Fatalf("accepted %#v", tc)
		}
	}
}
