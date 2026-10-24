"""Explicit live LiteLLM Responses smoke test (#122); never logs secret/model text."""
import argparse
import ipaddress
import json
import os
import re
import secrets
import stat
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

LIMIT = 8 * 1024 * 1024
ALIAS = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")


class SmokeError(Exception):
    pass


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise SmokeError("unexpected gateway redirect")


def endpoint(value):
    try:
        parsed = urlsplit(value)
        address = ipaddress.IPv4Address(parsed.hostname)
        if (parsed.scheme != "http" or parsed.port != 4000 or
                parsed.username or parsed.password or parsed.path not in ("", "/") or
                parsed.query or parsed.fragment):
            raise SmokeError("gateway must be a private HTTP IPv4 port 4000 endpoint")
    except (ValueError, TypeError):
        raise SmokeError("gateway must be a private HTTP IPv4 port 4000 endpoint") from None
    if not (address.is_loopback or any(address in ipaddress.ip_network(n) for n in
                                         ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"))):
        raise SmokeError("refusing a nonprivate gateway endpoint")
    return f"http://{address}:4000"


def protected_bytes(path):
    if str(path).startswith("/nix/store/"):
        raise SmokeError("secrets cannot live in the Nix store")
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or
                    info.st_mode & 0o077 or info.st_size > 65536):
                raise SmokeError("secret must be an owner-only regular file")
            data = os.read(fd, 65537)
        finally:
            os.close(fd)
    except OSError:
        raise SmokeError("cannot safely read secret file") from None
    return data


def secret_file(path):
    data = protected_bytes(path)
    try:
        value = data.decode("ascii").strip()
    except UnicodeDecodeError:
        raise SmokeError("credential is not ASCII") from None
    if not value or len(value) > 4096 or not value.isprintable() or any(ch.isspace() for ch in value):
        raise SmokeError("credential format invalid")
    return value


def classify_http_error(response):
    """Return a fixed, nonsecret diagnostic label for a bounded error body.

    Neither arbitrary provider response text nor HTTP reason phrases belong in
    operator output; these can include prompts, tokens, or attacker content.
    """
    try:
        raw = response.read(16385)
        if len(raw) > 16384:
            return "unclassified"
        payload = json.loads(raw)
    except (OSError, ValueError, UnicodeDecodeError, TypeError):
        return "unclassified"
    if not isinstance(payload, dict):
        return "unclassified"
    error = payload.get("error")
    if not isinstance(error, dict):
        return "unclassified"
    fields = (error.get("type"), error.get("code"), error.get("message"))
    text = " ".join(v.lower() for v in fields if isinstance(v, str))
    if "missingsessionid" in text or (
        "x-opencode-session" in text and "missing" in text
    ):
        return "missing-session-id"
    if "unsupportedparamserror" in text or "unsupported parameter" in text:
        return "unsupported-parameter"
    if "model_not_found" in text or "unknown model" in text:
        return "unknown-model"
    if "authenticationerror" in text or "invalid api key" in text:
        return "provider-authentication"
    return "unclassified"


def request(opener, host, route, key=None, body=None, diagnose=False):
    headers = {"Accept": "application/json"}
    if key is not None:
        headers["Authorization"] = "Bearer " + key
    if body is not None:
        headers["Content-Type"] = "application/json"
    req = Request(host + route, data=json.dumps(body).encode() if body is not None else None,
                  method="POST" if body is not None else "GET", headers=headers)
    try:
        with opener.open(req, timeout=20) as response:
            if response.status != 200:
                raise SmokeError(f"unexpected gateway HTTP {response.status} on {route}")
            data = response.read(LIMIT + 1)
        if len(data) > LIMIT:
            raise SmokeError("gateway payload exceeds 8MiB")
        return 200, data
    except HTTPError as exc:
        if diagnose:
            category = classify_http_error(exc)
            raise SmokeError(f"upstream Responses request failed (HTTP {exc.code}; {category})") from None
        return exc.code, b""
    except (URLError, TimeoutError, OSError):
        raise SmokeError(f"gateway {route} unavailable") from None


def model_ids(data):
    try:
        doc = json.loads(data)
    except (UnicodeDecodeError, ValueError):
        raise SmokeError("invalid model listing JSON") from None
    rows = doc.get("data") if isinstance(doc, dict) else None
    if not isinstance(rows, list) or any(not isinstance(r, dict) or not isinstance(r.get("id"), str)
                                        for r in rows):
        raise SmokeError("unexpected model listing shape")
    return {row["id"] for row in rows}


def verify_sse(data, expected_marker):
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        raise SmokeError("invalid Responses stream UTF-8") from None
    event_count = 0
    completed = False
    seen_output = False
    done = False
    blocks = re.split(r"\r?\n\r?\n", text)
    for block in blocks:
        if not block.strip():
            continue
        name = None
        data_lines = []
        for line in block.splitlines():
            if line.startswith("event:"):
                name = line[6:].strip()
            elif line.startswith("data:"):
                data_lines.append(line[5:].lstrip(" "))
            elif line.startswith(":"):
                continue
            elif line.strip():
                raise SmokeError("unrecognized SSE line")
        if not data_lines:
            continue
        value = "\n".join(data_lines)
        if value == "[DONE]":
            if not completed or done:
                raise SmokeError("Responses DONE marker before terminal completion")
            done = True
            continue
        if done or completed:
            raise SmokeError("Responses event after terminal completion")
        try:
            obj = json.loads(value)
        except ValueError:
            raise SmokeError("invalid Responses SSE JSON") from None
        if not isinstance(obj, dict) or not isinstance(obj.get("type"), str) or not obj["type"].startswith("response."):
            raise SmokeError("invalid Responses event type")
        if name is not None and name != obj["type"]:
            raise SmokeError("Responses SSE event/type mismatch")
        event_count += 1
        typ = obj["type"]
        if typ in ("response.failed", "response.incomplete", "response.error"):
            raise SmokeError("Responses stream terminated in failure")
        if typ == "response.output_text.done" and isinstance(obj.get("text"), str):
            seen_output |= expected_marker in obj["text"]
        if typ == "response.output_item.done" and isinstance(obj.get("item"), dict):
            content = obj["item"].get("content")
            if isinstance(content, list):
                for part in content:
                    if isinstance(part, dict) and part.get("type") == "output_text" and isinstance(part.get("text"), str):
                        seen_output |= expected_marker in part["text"]
        if typ == "response.completed":
            if not isinstance(obj.get("response"), dict) or obj["response"].get("status") != "completed":
                raise SmokeError("Responses terminal state is not completed")
            completed = True
    if not completed or not seen_output:
        raise SmokeError("no completed Responses output containing requested nonce")
    return event_count


def run(host, alias, workload_key, master_key, opener=None, marker=None):
    if workload_key == master_key:
        raise SmokeError("workload key equals gateway master; refusing inference")
    opener = opener or build_opener(ProxyHandler({}), NoRedirect())
    if request(opener, host, "/health/liveliness")[0] != 200:
        raise SmokeError("LiteLLM application health is not ready")
    for key in (None, "sk-invalid-aginctus-" + secrets.token_hex(12)):
        status, _ = request(opener, host, "/v1/models", key)
        if status not in (401, 403):
            raise SmokeError("LiteLLM model listing did not reject missing/invalid key")
    status, models = request(opener, host, "/v1/models", workload_key)
    if status != 200 or alias not in model_ids(models):
        raise SmokeError("workload credential does not authorize selected alias")
    marker = marker or "AGINCTUS_OK_" + secrets.token_hex(12)
    status, data = request(opener, host, "/v1/responses", workload_key, {
        "model": alias,
        "input": "Return exactly this identifier, and nothing else: " + marker,
        "stream": True,
        "max_output_tokens": 80,
        "user": "aginctus-smoke-" + marker.lower(),
    }, diagnose=True)
    if status != 200:
        raise SmokeError(f"upstream Responses request failed (HTTP {status})")
    count = verify_sse(data, marker)
    return marker, count


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--endpoint", required=True)
    p.add_argument("--alias", required=True)
    p.add_argument("--workload-key-file", required=True)
    p.add_argument("--gateway-id", default="local")
    p.add_argument("--run", action="store_true", help="Opt in to a paid real inference request")
    args = p.parse_args(argv)
    try:
        host = endpoint(args.endpoint)
        if not ALIAS.fullmatch(args.alias) or not ALIAS.fullmatch(args.gateway_id):
            raise SmokeError("invalid selected model alias or gateway ID")
        if not args.run:
            print("DRY RUN: no secrets read, no network requests; --run is required for live inference")
            return 0
        state_home = Path(os.environ.get("XDG_STATE_HOME", str(Path.home() / ".local/state")))
        raw = Path(state_home / "aginctus/gateway" / args.gateway_id / "secrets.json")
        try:
            state = json.loads(protected_bytes(raw))
        except (ValueError, UnicodeDecodeError):
            raise SmokeError("invalid protected gateway state JSON") from None
        master = state.get("AGINCTUS_GATEWAY_MASTER_KEY") if isinstance(state, dict) else None
        if not isinstance(master, str) or not master:
            raise SmokeError("protected gateway state has no master key")
        workload_key = secret_file(args.workload_key_file)
        marker, event_count = run(host, args.alias, workload_key, master)
        print(f"PASS: authenticated LiteLLM Responses stream completed with requested output; "
              f"{event_count} events; gateway correlation identifier {marker}; "
              "provider provenance and egress require separate gateway-side evidence")
        return 0
    except SmokeError as exc:
        print("FAIL: " + str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
