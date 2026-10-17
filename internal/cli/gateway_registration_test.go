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
		{[]string{"model", "chatgpt/gpt-6-sol", "register"}, "registered locally", 0},
		{[]string{"model", "chatgpt/gpt-6-sol", "register"}, "already registered", 0},
		{[]string{"model", "bad", "register", "--source=openai/gpt-5", "--dry-run"}, "model provider alias", 2},
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
    if err != nil { t.Fatal(err) }
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
    check([]string{"model", "list"}, "coding\\tchatgpt/sample\\tpersonal", 0)
    check([]string{"model", "coding", "show"}, "authorization and activation pending", 0)
    check([]string{"model", "coding", "register", "--source=chatgpt/different", "--provider=personal"}, "another registration", 1)
    check([]string{"model", "../escape", "register", "--source=chatgpt/sample", "--provider=personal"}, "invalid ChatGPT", 2)
    check([]string{"model", "wrong", "register", "--source=openai/model", "--provider=personal"}, "invalid ChatGPT", 2)
}
