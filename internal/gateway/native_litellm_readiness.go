package gateway

import (
	"context"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

// VerifyNativeGatewayLiteLLMReadyWithClient tests the provider-independent,
// authenticated readiness route in LiteLLM v1.103.0. The fixed guest file is a
// prerequisite, not a delivery mechanism or proof of provenance. Only use with
// a separately authorized guest-exec capability on an independently pinned
// instance. No credential or response body leaves the guest process.
func VerifyNativeGatewayLiteLLMReadyWithClient(ctx context.Context, descriptor environment.Descriptor, observed environment.Binding, client GatewayPostgresProbeClient) error {
	return verifyNativeGatewayGuestReady(ctx, descriptor, observed, client, "litellm", []string{
		"/app/.venv/bin/python", "-I", "-c", liteLLMReadinessProbe,
	})
}

// HTTPConnection follows no redirects and honors no proxy environment. A
// successful public health response or a 200 with no DB is not sufficient.
const liteLLMReadinessProbe = `
import http.client, json, os, re, stat, sys

def request(headers):
    conn = http.client.HTTPConnection("127.0.0.1", 4000, timeout=3)
    try:
        conn.request("GET", "/health/readiness/details", headers=headers)
        response = conn.getresponse()
        return response.status, response.read(32769) if response.status == 200 else b""
    finally:
        conn.close()

def ready():
    fd = os.open("/run/aginctus/gateway/master-key", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as keyfile:
        info = os.fstat(keyfile.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or
            info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) not in (0o400, 0o600)):
            return False
        key = keyfile.read(257).removesuffix(b"\n")
    if not re.fullmatch(rb"[A-Za-z0-9_-]{1,255}", key):
        return False
    if request({})[0] not in (401, 403):
        return False
    status, body = request({"Authorization": "Bearer " + key.decode("ascii")})
    if status != 200 or len(body) > 32768:
        return False
    data = json.loads(body)
    return data.get("status") == "healthy" and data.get("db") == "connected"

try:
    result = ready()
except Exception:
    result = False
sys.exit(0 if result else 1)
`
