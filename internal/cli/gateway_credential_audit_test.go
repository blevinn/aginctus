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

func TestGatewayCredentialStorageAuditCLI(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	old := auditGatewayCredentials
	defer func() { auditGatewayCredentials = old }()
	calls := 0
	auditGatewayCredentials = func(ctx context.Context, spec gateway.CredentialAttachment) error {
		calls++
		if spec.GatewayID == "" || spec.ComposeProject == "" || spec.Pool == "" {
			t.Fatalf("incomplete audited target: %+v", spec)
		}
		return nil
	}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		status := runGateway(context.Background(), args, &stdout, &stderr, nil, cfg)
		return status, stdout.String(), stderr.String()
	}
	if code, output, message := run("credential-storage", "audit"); code != 0 ||
		!strings.Contains(output, "owned and exclusive to LiteLLM") ||
		!strings.Contains(output, "in-guest permissions, OAuth and runtime readiness not verified") || message != "" {
		t.Fatalf("audit status=%d stdout=%q stderr=%q", code, output, message)
	}
	if calls != 1 {
		t.Fatalf("audit invoked %d times, want one", calls)
	}
	if code, _, _ := run("credential-storage", "audit", "--force"); code != 2 || calls != 1 {
		t.Fatal("accepted unsupported mutation option on read-only audit")
	}
	auditGatewayCredentials = func(_ context.Context, _ gateway.CredentialAttachment) error {
		return errors.New("unowned credential volume")
	}
	if code, output, message := run("credential-storage", "audit"); code != 1 || output != "" ||
		!strings.Contains(message, "unowned credential volume") {
		t.Fatalf("rejected audit status=%d stdout=%q stderr=%q", code, output, message)
	}
}
