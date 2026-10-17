package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestGatewayResponsesVerifyFromStdin(t *testing.T) {
	cfg, err := (&config.Loader{}).Load(config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	old := gatewayResponsesInput
	defer func() { gatewayResponsesInput = old }()
	good := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"edit\",\"arguments\":\"{}\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := runGateway(context.Background(), args, &stdout, &stderr, nil, cfg)
		return code, stdout.String(), stderr.String()
	}
	gatewayResponsesInput = strings.NewReader(good)
	if code, out, message := run("responses", "verify", "--require-function-call"); code != 0 ||
		!strings.Contains(out, "2 events, 1 completed function calls") || message != "" {
		t.Fatalf("verification: code=%d stdout=%q stderr=%q", code, out, message)
	}
	gatewayResponsesInput = strings.NewReader(good)
	if code, _, message := run("responses", "verify", "--activate"); code != 2 ||
		!strings.Contains(message, "Usage:") {
		t.Fatalf("unsupported activation option accepted: %d %q", code, message)
	}
	secret := "SECRET_RESPONSE_PAYLOAD"
	gatewayResponsesInput = strings.NewReader("data: " + secret + "\n\n")
	if code, out, message := run("responses", "verify"); code != 1 ||
		out != "" || strings.Contains(message, secret) || !strings.Contains(message, "contract") {
		t.Fatalf("invalid stream leaked payload or passed: code=%d out=%q err=%q", code, out, message)
	}
}
