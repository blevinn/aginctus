package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/statevolume"
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
	ensureCredentialStorage = func(_ context.Context, v statevolume.GatewayCredentials) error {
		calls++
		if v.Pool != "default" || v.GatewayID != "local" {
			t.Fatalf("unexpected volume: %#v", v)
		}
		return nil
	}
	var out, errs bytes.Buffer
	if got := runGateway(context.Background(), []string{"credential-storage", "ensure"}, &out, &errs, nil, effective); got != 0 {
		t.Fatalf("exit %d: %s", got, errs.String())
	}
	if calls != 1 || !strings.Contains(out.String(), "unmounted") {
		t.Fatalf("calls=%d output=%q", calls, out.String())
	}

	ensureCredentialStorage = func(context.Context, statevolume.GatewayCredentials) error { return errors.New("foreign volume") }
	out.Reset()
	errs.Reset()
	if got := runGateway(context.Background(), []string{"credential-storage", "ensure"}, &out, &errs, nil, effective); got != 1 || !strings.Contains(errs.String(), "foreign volume") {
		t.Fatalf("unexpected error behavior: exit=%d error=%q", got, errs.String())
	}
}
