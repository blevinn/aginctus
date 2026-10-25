"""In-guest synthetic LiteLLM Responses smoke, NEVER a real provider request.

Only run in an explicitly disposable NixOS VM. Inspect real Incus networks,
launch the test-only upstream on the guest's private egress bridge, register
a synthetic-only model with the real LiteLLM and require a scoped virtual key.
All admin/provider tokens stay in process memory; stdout has fixed safe labels.
"""
from __future__ import annotations

import ipaddress
import json
import secrets
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

# Both scripts are pinned separately into the guest Nix store. Validate
# consistency with the synthetic upstream in hostless protocol tests.
TOKEN = "synthetic-fixture-only"

ALIAS = "aginctus-vm-synthetic"
STATE = Path("/run/aginctus-vm-gateway-state/aginctus/gateway/local/secrets.json")
OPENER = build_opener(ProxyHandler({}))
MAX_RESULT = 1048576


class Failure(Exception):
    pass


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise Failure("synthetic gateway refused redirect")


OPENER.add_handler(NoRedirect())


def incus(*argv):
    result = subprocess.run(["incus", *argv], check=False, capture_output=True,
                            timeout=20, text=True)
    if result.returncode:
        raise Failure("guest Incus topology unavailable")
    return result.stdout.strip()


def network_ipv4(name):
    try:
        address = ipaddress.IPv4Interface(incus("network", "get", name, "ipv4.address"))
    except (ValueError, ipaddress.AddressValueError) as exc:
        raise Failure("guest Incus network has invalid IPv4") from exc
    if not address.ip.is_private or address.ip.is_loopback:
        raise Failure("guest Incus network is not private")
    return address


def lite_ipv4(management):
    state = json.loads(incus("query", "--project", "aginctus-gateway",
                            "/1.0/instances/litellm-1/state"))
    networks = state.get("network")
    if not isinstance(networks, dict):
        raise Failure("real LiteLLM instance has no observed guest NICs")
    addresses = []
    for nic in networks.values():
        for addr in nic.get("addresses", []):
            if addr.get("family") != "inet":
                continue
            try:
                candidate = ipaddress.IPv4Address(addr["address"])
            except (KeyError, ValueError):
                continue
            if candidate in management.network and candidate != management.ip:
                addresses.append(candidate)
    if len(addresses) != 1:
        raise Failure("could not resolve unique LiteLLM management address")
    return str(addresses[0])


def request(base, path, token=None, body=None):
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if body is not None:
        headers["Content-Type"] = "application/json"
    req = Request(base + path, data=json.dumps(body).encode() if body is not None else None,
                  headers=headers, method="POST" if body is not None else "GET")
    try:
        with OPENER.open(req, timeout=12) as response:
            raw = response.read(MAX_RESULT + 1)
            if len(raw) > MAX_RESULT:
                raise Failure("synthetic gateway result over limit")
            if response.status not in (200, 201):
                raise Failure("synthetic gateway HTTP status not successful")
            return raw
    except HTTPError as exc:
        raise Failure(f"synthetic gateway operation HTTP {exc.code}") from None
    except (URLError, TimeoutError, OSError):
        raise Failure("synthetic gateway not reachable") from None


def model_ids(raw):
    try:
        body = json.loads(raw)
        return {row["id"] for row in body["data"]}
    except (KeyError, TypeError, ValueError):
        raise Failure("synthetic gateway models listing invalid") from None


def verify_stream(raw, nonce):
    try:
        s = raw.decode("utf-8")
    except UnicodeDecodeError:
        raise Failure("synthetic Responses stream invalid encoding") from None
    completed = False
    done = False
    witnessed = False
    for block in s.replace("\r\n", "\n").split("\n\n"):
        if not block.strip():
            continue
        if block.strip() == "data: [DONE]":
            if not completed or done:
                raise Failure("synthetic Responses DONE ordering invalid")
            done = True
            continue
        events = [x[6:].strip() for x in block.splitlines() if x.startswith("event:")]
        fields = [x[5:].strip() for x in block.splitlines() if x.startswith("data:")]
        if len(events) != 1 or len(fields) != 1 or done or completed:
            raise Failure("synthetic Responses SSE framing invalid")
        try:
            body = json.loads(fields[0])
        except ValueError:
            raise Failure("synthetic Responses JSON invalid") from None
        if not isinstance(body, dict) or body.get("type") != events[0]:
            raise Failure("synthetic Responses event mismatch")
        if events[0] == "response.completed":
            if body.get("response", {}).get("status") != "completed":
                raise Failure("synthetic Responses incomplete")
            completed = True
        if "synthetic-confirmed:" + nonce in fields[0]:
            witnessed = True
    if not completed or not done or not witnessed:
        raise Failure("synthetic Responses completion/nonce not observed")


def run(server_script):
    if not server_script.is_file() or not str(server_script).startswith("/nix/store/"):
        raise Failure("synthetic upstream must be the immutable packaged fixture")
    management = network_ipv4("aginctus-mgmt")
    egress = network_ipv4("aginctus-egress")
    if management.network.overlaps(egress.network):
        raise Failure("synthetic egress and management bridge overlap")

    # All data and identities in this test are guest-only and synthetic.
    subprocess.run([
        "systemd-run", "--quiet", "--unit=aginctus-vm-synthetic-upstream",
        "--property=NoNewPrivileges=yes", "--property=PrivateTmp=yes",
        "--property=ProtectSystem=strict", "--property=ProtectHome=yes",
        "--property=IPAddressDeny=any",
        "--property=IPAddressAllow=" + str(egress.network),
        sys.executable, str(server_script), "--bind", str(egress.ip),
        "--port", "9080",
    ], check=True, capture_output=True, timeout=15)
    upstream = "http://" + str(egress.ip) + ":9080"
    for _ in range(30):
        try:
            if json.loads(request(upstream, "/health"))["status"] == "synthetic-ready":
                break
        except (Failure, KeyError, ValueError):
            time.sleep(0.2)
    else:
        raise Failure("synthetic upstream did not become ready")
    if not STATE.is_file():
        raise Failure("protected guest-only gateway credential state absent")
    if STATE.stat().st_mode & 0o077:
        raise Failure("gateway credential state readable by nonowner")
    state = json.loads(STATE.read_text())
    master = state.get("AGINCTUS_GATEWAY_MASTER_KEY")
    if not isinstance(master, str) or len(master) < 10:
        raise Failure("guest gateway admin key unavailable")

    base = "http://" + lite_ipv4(management) + ":4000"
    if ALIAS in model_ids(request(base, "/v1/models", master)):
        raise Failure("synthetic alias already present; refuse adoption")
    request(base, "/model/new", master, {
        "model_name": ALIAS,
        "litellm_params": {
            "model": "openai/synthetic-test",
            "api_base": upstream + "/v1",
            "api_key": TOKEN,
        },
        "model_info": {"mode": "responses"},
    })
    for _ in range(30):
        if ALIAS in model_ids(request(base, "/v1/models", master)):
            break
        time.sleep(0.2)
    else:
        raise Failure("synthetic alias registration not confirmed")

    token_doc = json.loads(request(base, "/key/generate", master, {
        "models": [ALIAS], "duration": "15m", "max_budget": 1, "rpm_limit": 10,
    }))
    scoped = token_doc.get("key")
    if not isinstance(scoped, str) or not scoped or scoped == master:
        raise Failure("scoped synthetic workload key was not issued")

    # Negative auth controls must fail *before* any upstream request.
    for forbidden in (None, "synthetic-invalid-key"):
        try:
            request(base, "/v1/models", forbidden)
        except Failure as exc:
            if "HTTP 401" not in str(exc) and "HTTP 403" not in str(exc):
                raise
        else:
            raise Failure("unauthorized LiteLLM model listing accepted")
    if ALIAS not in model_ids(request(base, "/v1/models", scoped)):
        raise Failure("scoped workload cannot see synthetic alias")

    nonce = secrets.token_hex(12)
    raw = request(base, "/v1/responses", scoped, {
        "model": ALIAS,
        "input": "Return this deterministic fixture marker: nonce=" + nonce,
        "stream": True,
    })
    verify_stream(raw, nonce)
    observed = json.loads(request(upstream, "/_observed/" + nonce, TOKEN))
    if observed.get("count") != 1:
        raise Failure("synthetic upstream did not independently observe request")
    print("PASS: real LiteLLM scoped-key synthetic Responses route (no live provider)")
    print("NOTE: Herdr/OpenCode tool execution, gateway audit and cleanup not verified")


def main():
    try:
        if len(sys.argv) != 2:
            raise Failure("fixture invocation requires one pinned script")
        run(Path(sys.argv[1]))
    except (Failure, ValueError, json.JSONDecodeError, subprocess.SubprocessError):
        print("FAIL: synthetic gateway acceptance (details suppressed)", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
