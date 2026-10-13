package cli

import (
	"bytes"
	"context"
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
	cases := []struct {
		args   []string
		want   string
		status int
	}{
		{[]string{"provider", "chatgpt", "register", "--dry-run"}, "\"alias\": \"chatgpt\"", 0},
		{[]string{"provider", "chatgpt", "register", "--alias=personal", "--dry-run"}, "\"alias\": \"personal\"", 0},
		{[]string{"model", "chatgpt/gpt-6-sol", "register", "--dry-run"}, "\"mode\": \"responses\"", 0},
		{[]string{"model", "coding", "register", "--source=chatgpt/gpt-6-sol", "--provider=chatgpt", "--dry-run"}, "\"model_name\": \"coding\"", 0},
		{[]string{"provider", "chatgpt", "register"}, "registered locally", 0},
		{[]string{"provider", "chatgpt", "register"}, "already registered", 0},
		{[]string{"model", "chatgpt/gpt-6-sol", "register"}, "not implemented", 2},
		{[]string{"model", "bad", "register", "--source=openai/gpt-5", "--dry-run"}, "only the default", 2},
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
