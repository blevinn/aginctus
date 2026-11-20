"""Opt-in, host-local LiteLLM API-key-backed model registration (#122).

Never prints an admin/provider secret or an untrusted HTTP response. This tool
changes only LiteLLM's database-backed model registry, not Incus or Compose.
"""
import argparse
import ipaddress
import json
import os
import re
import stat
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

ALIAS = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
MODEL = re.compile(r"^openai/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
PRIVATE_NETS = tuple(ipaddress.ip_network(x) for x in (
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8"
))
# Canonical OpenCode Go API documented at opencode.ai/v2/docs/console/go.
# Only native Responses models: other Go models use Chat Completions/Messages.
GO_RESPONSES_BASE = "https://opencode.ai/inference/go/openai/v1"
GO_RESPONSES_MODELS = frozenset({
    "grok-4.7", "gpt-6-luna", "gpt-5.6-luna",
    "muse-spark-1.3-contributor", "muse-spark-1.2-contributor",
})


def registration_route(provider, source):
    if not MODEL.fullmatch(source):
        raise RegistrationError("invalid OpenAI-compatible upstream source")
    if provider == "openai":
        return None
    if provider == "opencode-go" and source.removeprefix("openai/") in GO_RESPONSES_MODELS:
        return GO_RESPONSES_BASE
    raise RegistrationError("provider/model is not a supported native Responses route")



class RegistrationError(Exception):
    """Safe-for-operator diagnostic (never derived from response bodies)."""


def private_endpoint(value):
    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError:
        raise RegistrationError("invalid private gateway endpoint") from None
    if (parsed.scheme != "http" or parsed.username is not None or
            parsed.password is not None or parsed.path not in ("", "/") or
            parsed.query or parsed.fragment or port != 4000):
        raise RegistrationError("endpoint must be an HTTP IPv4 address on private port 4000")
    try:
        ip = ipaddress.IPv4Address(parsed.hostname)
    except (ipaddress.AddressValueError, TypeError, ValueError):
        raise RegistrationError("endpoint must use a literal private IPv4 address") from None
    if not any(ip in network for network in PRIVATE_NETS):
        raise RegistrationError("endpoint must be private or host-loopback")
    return f"http://{ip}:4000"


def protected_read(path):
    path = Path(path).expanduser()
    if str(path).startswith("/nix/store/"):
        raise RegistrationError("credential files must not reside in the Nix store")
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            meta = os.fstat(fd)
            if (not stat.S_ISREG(meta.st_mode) or
                    stat.S_IMODE(meta.st_mode) & 0o077 or
                    meta.st_uid != os.geteuid() or meta.st_size > 65536):
                raise RegistrationError("credential file must be owned by caller, regular, and mode 0600 or stricter")
            with os.fdopen(fd, "rb", closefd=False) as stream:
                data = stream.read(65537)
            if len(data) > 65536:
                raise RegistrationError("credential file is too large")
            return data
        finally:
            os.close(fd)
    except (OSError, ValueError):
        raise RegistrationError("cannot safely read credential file") from None


def credential(data):
    try:
        value = data.decode("utf-8").strip()
    except UnicodeDecodeError:
        raise RegistrationError("invalid credential encoding") from None
    if not value or len(value) > 4096 or any(ord(c) < 33 or ord(c) > 126 for c in value):
        raise RegistrationError("invalid credential content")
    return value


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RegistrationError("gateway redirected an administrative request")


def api_request(opener, endpoint, path, master_key, payload=None):
    headers = {"Authorization": "Bearer " + master_key, "Accept": "application/json"}
    if payload is not None:
        headers["Content-Type"] = "application/json"
    request = Request(endpoint + path, method="POST" if payload is not None else "GET",
                      headers=headers, data=json.dumps(payload).encode() if payload is not None else None)
    try:
        with opener.open(request, timeout=10) as response:
            if response.status not in (200, 201):
                raise RegistrationError(f"gateway rejected {path} (HTTP {response.status})")
            body = response.read(1048577)
        if len(body) > 1048576:
            raise RegistrationError("gateway response exceeded safe size")
        return json.loads(body)
    except HTTPError as exc:
        raise RegistrationError(f"gateway rejected {path} (HTTP {exc.code})") from None
    except (URLError, TimeoutError, OSError):
        raise RegistrationError(f"gateway {path} unavailable; registration outcome may require inspection") from None
    except (ValueError, UnicodeDecodeError):
        raise RegistrationError(f"gateway {path} returned invalid JSON") from None


def aliases(info):
    rows = info.get("data") if isinstance(info, dict) else None
    if not isinstance(rows, list):
        raise RegistrationError("gateway model listing has an unsupported format")
    result = set()
    for row in rows:
        if not isinstance(row, dict) or not isinstance(row.get("id"), str):
            raise RegistrationError("gateway model listing has an unsupported entry")
        result.add(row["id"])
    return result


def register(endpoint, alias, source, master, upstream, opener=None, api_base=None):
    if opener is None:
        opener = build_opener(ProxyHandler({}), NoRedirect())
    existing = aliases(api_request(opener, endpoint, "/v1/models", master))
    if alias in existing:
        raise RegistrationError("model alias already exists; refusing to overwrite or silently reuse")
    params = {"model": source, "api_key": upstream}
    if api_base is not None:
        # The value is selected only by registration_route(), never by user URL.
        params["api_base"] = api_base
    api_request(opener, endpoint, "/model/new", master, {
        "model_name": alias,
        "litellm_params": params,
        "model_info": {"mode": "responses"},
    })
    # Never retry POST automatically. An uncertain response might still have
    # committed an encrypted provider key in the LiteLLM database.
    if alias not in aliases(api_request(opener, endpoint, "/v1/models", master)):
        raise RegistrationError("registration unconfirmed; inspect gateway models before retrying")


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--endpoint", required=True, help="Private numeric IPv4 gateway endpoint, HTTP port 4000")
    p.add_argument("--alias", required=True, help="Explicit workload-facing model name")
    p.add_argument("--source", required=True, help="LiteLLM upstream, e.g. openai/gpt-6-luna")
    p.add_argument("--provider", choices=("openai", "opencode-go"), default="openai",
                   help="Upstream route; OpenCode Go uses a fixed, trusted Responses API base")
    p.add_argument("--gateway-id", default="local", help="Existing gateway secrets state ID")
    p.add_argument("--upstream-key-file", help="Protected external API-key file (0600)")
    p.add_argument("--apply", action="store_true", help="Persist a model in the running LiteLLM database")
    args = p.parse_args(argv)
    try:
        endpoint = private_endpoint(args.endpoint)
        if not ALIAS.fullmatch(args.alias):
            raise RegistrationError("invalid explicit model alias")
        api_base = registration_route(args.provider, args.source)
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]*", args.gateway_id):
            raise RegistrationError("invalid gateway ID")
        if not args.apply:
            route = args.provider if api_base is None else "opencode-go native Responses"
            print(f"DRY RUN: would register {args.alias} -> {args.source} ({route}) on private LiteLLM; no credentials read")
            return 0
        if not args.upstream_key_file:
            raise RegistrationError("--upstream-key-file is required with --apply")
        root = Path(os.environ.get("XDG_STATE_HOME", str(Path.home() / ".local/state")))
        state = json.loads(protected_read(root / "aginctus/gateway" / args.gateway_id / "secrets.json"))
        if not isinstance(state, dict) or not all(isinstance(state.get(k), str) and state[k] for k in
                                                    ("AGINCTUS_GATEWAY_MASTER_KEY", "AGINCTUS_GATEWAY_SALT_KEY")):
            raise RegistrationError("existing protected gateway secret state is incomplete")
        master = credential(state["AGINCTUS_GATEWAY_MASTER_KEY"].encode())
        upstream = credential(protected_read(args.upstream_key_file))
        if upstream == master:
            raise RegistrationError("upstream credential cannot equal LiteLLM master credential")
        register(endpoint, args.alias, args.source, master, upstream, api_base=api_base)
        print("PASS: LiteLLM model alias registered in database (upstream inference NOT yet verified)")
        return 0
    except (RegistrationError, json.JSONDecodeError) as exc:
        msg = str(exc) if isinstance(exc, RegistrationError) else "gateway state is not valid JSON"
        print("FAIL: " + msg, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
