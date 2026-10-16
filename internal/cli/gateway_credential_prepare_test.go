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

func TestGatewayCredentialPreparationCommand(t *testing.T) {
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	original := prepareGatewayCredentials
	t.Cleanup(func() { prepareGatewayCredentials = original })
	called := 0
	prepareGatewayCredentials = func(_ context.Context, a gateway.CredentialAttachment) error {
		called++
		if a.GatewayID != "local" || a.ComposeProject != "aginctus-gateway" || a.Pool != "default" {
			t.Fatalf("wrong preparation target: %#v", a)
		}
		return nil
	}
	var out, errs bytes.Buffer
	rc := runGateway(context.Background(), []string{"credential-storage", "prepare"}, &out, &errs, nil, cfg)
	if rc != 0 || called != 1 || !strings.Contains(out.String(), "OAuth not initiated") ||
		!strings.Contains(out.String(), "not activated") {
		t.Fatalf("rc=%d called=%d stdout=%q stderr=%q", rc, called, out.String(), errs.String())
	}
	prepareGatewayCredentials = func(context.Context, gateway.CredentialAttachment) error {
		return errors.New("untrusted mount")
	}
	out.Reset()
	errs.Reset()
	rc = runGateway(context.Background(), []string{"credential-storage", "prepare"}, &out, &errs, nil, cfg)
	if rc != 1 || !strings.Contains(errs.String(), "untrusted mount") || out.Len() != 0 {
		t.Fatalf("rc=%d stdout=%q stderr=%q", rc, out.String(), errs.String())
	}
}
