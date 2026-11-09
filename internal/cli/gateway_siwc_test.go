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
	"github.com/blevinn/aginctus/internal/gateway"
)

func TestSIWCProviderOperationsUseConfiguredGatewayWithoutLocalCredentials(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", root)
	file := filepath.Join(t.TempDir(), "configuration.json")
	if err := os.WriteFile(file, []byte(`{"gateway":{"compose":{"project":"fixture-gateway"},"providers":{"personal":{"plugin":"openai-siwc"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	previous := exchangeGatewaySIWC
	t.Cleanup(func() { exchangeGatewaySIWC = previous })
	calls := 0
	exchangeGatewaySIWC = func(_ context.Context, attachment gateway.CredentialAttachment, update gateway.CredentialUpdate) error {
		calls++
		if attachment.ComposeProject != "fixture-gateway" {
			t.Fatal("ignored normal configuration selection")
		}
		_, err := update(json.RawMessage(`{"version":1,"host_id":"urn:uuid:00000000-0000-4000-8000-000000000001","accounts":{"personal":{"client_id":"oaiapp_fixture","scopes":[]},"other":{"client_id":"oaiapp_other","scopes":[]}},"routes":{}}`))
		return err
	}
	run := func(args ...string) (int, string) {
		var out, errs bytes.Buffer
		args = append([]string{"--configuration-file", file, "gateway"}, args...)
		code := Run(context.Background(), args, &out, &errs, nil, &config.Loader{})
		return code, out.String() + errs.String()
	}
	for _, args := range [][]string{{"provider", "login", "personal"}, {"provider", "login", "personal", "--consent-connected-app"}, {"provider", "login", "personal", "--consent-background"}} {
		if code, out := run(args...); code == 0 || !strings.Contains(out, "requires --consent-connected-app") {
			t.Fatalf("consent validation: %d %s", code, out)
		}
	}
	if calls != 0 {
		t.Fatal("opened gateway credential state before consent validation")
	}
	if code, out := run("provider", "status", "personal"); code != 0 || !strings.Contains(out, "signed_out") || strings.Contains(out, "other") {
		t.Fatalf("provider status: %d %s", code, out)
	}
	if code, out := run("provider", "logout", "--provider", "personal"); code != 0 {
		t.Fatalf("provider logout: %d %s", code, out)
	}
	if calls != 2 {
		t.Fatal("provider operations did not dispatch through managed gateway")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("created a host credential cache: %v", err)
	}
	for _, args := range [][]string{{"siwc", "init"}, {"siwc", "bind"}, {"siwc", "export"}, {"provider", "login", "personal", "--state-dir", "/private/extra-state"}} {
		if code, out := run(args...); code == 0 {
			t.Fatalf("accepted private-state workflow: %s", out)
		}
	}
}

func TestSIWCLoginHelpIsOfflineAndShowsConsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", root)
	var out, errs bytes.Buffer
	code := Run(context.Background(), []string{"--configuration-file", "/absent/configuration.json", "gateway", "provider", "login", "--help"}, &out, &errs, nil, &config.Loader{})
	if code != 0 || !strings.Contains(out.String(), "--consent-background") {
		t.Fatalf("nested help: %d %s %s", code, out.String(), errs.String())
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("help initialized state")
	}
}
