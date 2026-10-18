package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
)

func TestGatewayCredentialStorageEnsure(t *testing.T) {
	loader := &config.Loader{}
	effective, err := loader.Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}

	original := ensureCredentialStorage
	t.Cleanup(func() { ensureCredentialStorage = original })
	calls := 0
	ensureCredentialStorage = func(_ context.Context, a gateway.CredentialAttachment) error {
		calls++
		if a.Pool != "default" || a.GatewayID != "local" || a.ComposeProject != "aginctus-gateway" {
			t.Fatalf("unexpected credential target: %#v", a)
		}
		return nil
	}
	var out, errs bytes.Buffer
	if got := runGateway(context.Background(), []string{"credential-storage", "ensure"}, &out, &errs, nil, effective); got != 0 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	if calls != 1 || !strings.Contains(out.String(), "Compose project \"aginctus-gateway\"") {
		t.Fatalf("calls=%d output=%q", calls, out.String())
	}

	ensureCredentialStorage = func(context.Context, gateway.CredentialAttachment) error { return errors.New("foreign volume") }
	out.Reset()
	errs.Reset()
	if got := runGateway(context.Background(), []string{"credential-storage", "ensure"}, &out, &errs, nil, effective); got != 1 || !strings.Contains(errs.String(), "foreign volume") {
		t.Fatalf("unexpected error behavior: exit=%d error=%q", got, errs.String())
	}
}

func TestCredentialStorageEnsureValidatesProjectBeforeMutation(t *testing.T) {
	original := ensureCredentialStorage
	t.Cleanup(func() { ensureCredentialStorage = original })
	calls := 0
	ensureCredentialStorage = func(context.Context, gateway.CredentialAttachment) error {
		calls++
		return nil
	}
	for _, project := range []string{"default", "", "project/invalid"} {
		cfg, err := (&config.Loader{}).Load(config.Options{
			Overrides: []string{"gateway.compose.project=" + project},
		})
		if err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		code := runGateway(context.Background(), []string{"credential-storage", "ensure"}, &out, &errs, nil, cfg)
		if code != 1 || calls != 0 || !strings.Contains(errs.String(), "credential storage configuration") {
			t.Fatalf("project %q: exit=%d calls=%d stderr=%q", project, code, calls, errs.String())
		}
	}
}
