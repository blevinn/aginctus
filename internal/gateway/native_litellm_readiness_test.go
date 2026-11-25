package gateway

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLiteLLMGuestProbeRequiresProtectedKeyAuthAndDatabase(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required to exercise the embedded guest probe")
	}
	// Exercise the exact production script with a temporary file and in-process
	// responses. No sockets, provider clients or Incus privileges are involved.
	const harness = `
import http.client, os, sys
path, scenario = sys.argv[1:3]
original_open = os.open
def open_key(name, flags):
    assert name == "/run/aginctus/gateway/master-key"
    return original_open(path, flags)
os.open = open_key
class Response:
    def __init__(self, status, body): self.status, self.body = status, body
    def read(self, limit): return self.body[:limit]
class Connection:
    def __init__(self, host, port, timeout):
        assert (host, port, timeout) == ("127.0.0.1", 4000, 3)
    def request(self, method, route, headers):
        assert (method, route) == ("GET", "/health/readiness/details")
        self.auth = headers.get("Authorization")
    def getresponse(self):
        if self.auth is None:
            return Response(200 if scenario == "public" else 401, b"{}")
        if scenario == "wrong-key" or self.auth != "Bearer synthetic-fixture-key":
            return Response(403, b"")
        if scenario == "redirect": return Response(302, b"")
        if scenario == "oversized": return Response(200, b" " * 33000)
        if scenario == "disconnected": return Response(200, b'{"status":"healthy","db":"disconnected"}')
        return Response(200, b'{"status":"healthy","db":"connected"}')
    def close(self): pass
http.client.HTTPConnection = Connection
`
	for _, tc := range []string{"ready", "public", "wrong-key", "redirect", "oversized", "disconnected", "mode", "symlink", "missing"} {
		t.Run(tc, func(t *testing.T) {
			dir := t.TempDir()
			key := filepath.Join(dir, "key")
			mode := os.FileMode(0o600)
			if tc == "mode" {
				mode = 0o644
			}
			if tc != "missing" {
				if err := os.WriteFile(key, []byte("synthetic-fixture-key\n"), mode); err != nil {
					t.Fatal(err)
				}
			}
			if tc == "symlink" {
				link := filepath.Join(dir, "link")
				if err := os.Symlink(key, link); err != nil {
					t.Fatal(err)
				}
				key = link
			}
			var exit *exec.ExitError
			out, err := exec.CommandContext(context.Background(), python, "-I", "-c", harness+liteLLMReadinessProbe, key, tc).CombinedOutput()
			if len(out) != 0 || (tc == "ready" && err != nil) || (tc != "ready" && (!errors.As(err, &exit) || exit.ExitCode() != 1)) {
				t.Fatalf("unexpected probe result: %v (%d diagnostic bytes)", err, len(out))
			}
		})
	}
}
