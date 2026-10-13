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

func TestGatewayOAuthStorageEnsure(t *testing.T) {
    loader := &config.Loader{}
    effective, err := loader.Load(config.Options{})
    if err != nil { t.Fatal(err) }

    original := ensureOAuthStorage
    t.Cleanup(func() { ensureOAuthStorage = original })
    calls := 0
    ensureOAuthStorage = func(_ context.Context, v statevolume.OAuthCredentials) error {
        calls++
        if v.Pool != "default" || v.GatewayID != "local" { t.Fatalf("unexpected volume: %#v", v) }
        return nil
    }
    var out, errs bytes.Buffer
    if got := runGateway(context.Background(), []string{"oauth-storage", "ensure"}, &out, &errs, nil, effective); got != 0 {
        t.Fatalf("exit %d: %s", got, errs.String())
    }
    if calls != 1 || !strings.Contains(out.String(), "unmounted") { t.Fatalf("calls=%d output=%q", calls, out.String()) }

    ensureOAuthStorage = func(context.Context, statevolume.OAuthCredentials) error { return errors.New("foreign volume") }
    out.Reset(); errs.Reset()
    if got := runGateway(context.Background(), []string{"oauth-storage", "ensure"}, &out, &errs, nil, effective); got != 1 || !strings.Contains(errs.String(), "foreign volume") {
        t.Fatalf("unexpected error behavior: exit=%d error=%q", got, errs.String())
    }
}
