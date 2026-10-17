#!/usr/bin/env python3
"""Issue or revoke one short-lived, model-scoped LiteLLM pilot key (#122).

Dry-run by default. Never prints provider, master or virtual key material.
The pinned LiteLLM v1.103.0 /key/generate and /key/delete APIs own key state.
"""
import argparse
from datetime import datetime, timedelta, timezone
import ipaddress
import json
import math
import os
from pathlib import Path
import re
import secrets
import stat
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

ALIAS = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
KEY_ALIAS = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
NETS = tuple(ipaddress.ip_network(s) for s in (
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8"))
KEY_BUDGET_USD = 1.0
KEY_DURATION = "1h"
KEY_ROUTES = ["llm_api_routes", "/v1/models"]


class KeyErrorSafe(Exception):
    """All messages are predetermined and contain no raw remote content."""


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        raise KeyErrorSafe("gateway redirected a key-management request")


def endpoint(value):
    try:
        p = urlsplit(value)
        ip = ipaddress.IPv4Address(p.hostname)
        valid = (p.scheme == "http" and p.port == 4000
                 and p.username is None and p.password is None
                 and p.path in ("", "/") and not p.query and not p.fragment
                 and any(ip in network for network in NETS))
    except (TypeError, ValueError):
        raise KeyErrorSafe("endpoint must be private numeric IPv4 HTTP port 4000") from None
    if not valid:
        raise KeyErrorSafe("endpoint must be private numeric IPv4 HTTP port 4000")
    return f"http://{ip}:4000"


def protected_read(path):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            mode = os.fstat(fd)
            if (not stat.S_ISREG(mode.st_mode) or mode.st_uid != os.geteuid()
                    or stat.S_IMODE(mode.st_mode) & 0o077 or mode.st_size > 65536):
                raise KeyErrorSafe("secret file must be owner-only, regular and no larger than 64KiB")
            with os.fdopen(fd, "rb", closefd=False) as stream:
                data = stream.read(65537)
            if len(data) > 65536:
                raise KeyErrorSafe("secret file is too large")
            return data
        finally:
            os.close(fd)
    except OSError:
        raise KeyErrorSafe("cannot safely read protected credential file") from None


def ascii_secret(data):
    try:
        val = data.decode("ascii").strip()
    except UnicodeDecodeError:
        raise KeyErrorSafe("invalid protected credential encoding") from None
    if not 16 <= len(val) <= 4096 or not val.isprintable() or any(c.isspace() for c in val):
        raise KeyErrorSafe("invalid protected credential")
    return val


def read_master(gateway_id):
    state_dir = Path(os.environ.get("XDG_STATE_HOME", str(Path.home() / ".local/state")))
    state_path = state_dir / "aginctus/gateway" / gateway_id / "secrets.json"
    if str(state_path).startswith("/nix/store/"):
        raise KeyErrorSafe("secret files cannot live in Nix store")
    try:
        state = json.loads(protected_read(state_path))
    except (ValueError, UnicodeError):
        raise KeyErrorSafe("invalid protected gateway state") from None
    if not isinstance(state, dict) or not isinstance(state.get("AGINCTUS_GATEWAY_MASTER_KEY"), str):
        raise KeyErrorSafe("protected gateway master key is missing")
    return ascii_secret(state["AGINCTUS_GATEWAY_MASTER_KEY"].encode("utf-8"))


def secure_output_path(raw, creating):
    path = Path(raw)
    if not path.is_absolute() or str(path).startswith("/nix/store/"):
        raise KeyErrorSafe("key output must be an absolute path outside Nix store")
    if path.parent.is_symlink():
        raise KeyErrorSafe("key output parent cannot be a symlink")
    try:
        parent = path.parent.stat()
    except OSError:
        raise KeyErrorSafe("key output parent directory is missing") from None
    if (not stat.S_ISDIR(parent.st_mode) or parent.st_uid != os.geteuid()
            or stat.S_IMODE(parent.st_mode) & 0o077):
        raise KeyErrorSafe("key output parent must be caller-owned mode 0700")
    if creating and path.exists():
        raise KeyErrorSafe("refusing to overwrite an existing key file")
    return path


def api(opener, base, path, master, payload=None):
    headers = {"Authorization": "Bearer " + master, "Accept": "application/json"}
    if payload is not None:
        headers["Content-Type"] = "application/json"
    req = Request(base + path, headers=headers, data=json.dumps(payload).encode() if payload is not None else None,
                  method="POST" if payload is not None else "GET")
    try:
        with opener.open(req, timeout=15) as response:
            if response.status not in (200, 201):
                raise KeyErrorSafe(f"gateway rejected {path} request")
            raw = response.read(1048577)
        if len(raw) > 1048576:
            raise KeyErrorSafe("gateway JSON response exceeds 1MiB")
        return json.loads(raw)
    except HTTPError as exc:
        raise KeyErrorSafe(f"gateway rejected {path} (HTTP {exc.code})") from None
    except (OSError, URLError, TimeoutError):
        raise KeyErrorSafe(f"gateway {path} request failed; remote outcome may be uncertain") from None
    except (ValueError, UnicodeError):
        raise KeyErrorSafe(f"gateway {path} returned invalid JSON") from None


def models(doc):
    items = doc.get("data") if isinstance(doc, dict) else None
    if not isinstance(items, list) or any(not isinstance(v, dict) or not isinstance(v.get("id"), str) for v in items):
        raise KeyErrorSafe("unsupported gateway model-list format")
    return {item["id"] for item in items}


def validate_grant(data, requested_model, master):
    if not isinstance(data, dict):
        raise KeyErrorSafe("invalid generated key response")
    key = data.get("key")
    if not isinstance(key, str) or not key.startswith("sk-"):
        raise KeyErrorSafe("generated key is missing or invalid")
    if key == master:
        raise KeyErrorSafe("generated key must differ from gateway master")
    try:
        key = ascii_secret(key.encode("ascii"))
    except UnicodeError:
        raise KeyErrorSafe("generated key contains invalid encoding") from None
    if data.get("models") != [requested_model]:
        raise KeyErrorSafe("generated key does not confirm exact model restriction")
    budget = data.get("max_budget")
    if (isinstance(budget, bool) or not isinstance(budget, (int, float))
            or not math.isfinite(budget) or not 0 < budget <= KEY_BUDGET_USD):
        raise KeyErrorSafe("generated key budget is not tightly bounded")
    expires = data.get("expires")
    if not isinstance(expires, str):
        raise KeyErrorSafe("generated key expiration is not present")
    try:
        when = datetime.fromisoformat(expires.replace("Z", "+00:00"))
    except ValueError:
        raise KeyErrorSafe("invalid generated key expiration") from None
    now = datetime.now(timezone.utc)
    if when.tzinfo is None or not now < when <= now + timedelta(minutes=65):
        raise KeyErrorSafe("generated key expiration is not within pilot limit")
    return key


def run_issue(opener, base, alias, master, output):
    if alias not in models(api(opener, base, "/v1/models", master)):
        raise KeyErrorSafe("selected alias is not registered in LiteLLM")
    key_alias = "aginctus-pilot-" + secrets.token_hex(8)
    # Explicitly lock down routing, alias, expiration, concurrency and budget.
    # No retry after /key/generate; it may have persisted an unknown remote key.
    request = {
        "models": [alias], "duration": KEY_DURATION,
        "max_budget": KEY_BUDGET_USD, "max_parallel_requests": 1,
        "rpm_limit": 5, "key_alias": key_alias,
        "allowed_routes": KEY_ROUTES,
    }
    # A preexisting file must cause failure BEFORE requesting a remote key.
    fd = os.open(output, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    created = False
    try:
        try:
            response = api(opener, base, "/key/generate", master, request)
        except KeyErrorSafe:
            # A timeout may follow a successful remote commit. This alias is nonsecret.
            raise KeyErrorSafe(f"key generation uncertain; inspect and revoke remote alias {key_alias} before retrying") from None
        # A remote key may already exist if any step below fails. If possible,
        # preserve it for explicit revocation rather than discarding it.
        value = response.get("key") if isinstance(response, dict) else None
        if isinstance(value, str) and value.startswith("sk-") and value != master:
            try:
                value = ascii_secret(value.encode("ascii"))
            except UnicodeError:
                raise KeyErrorSafe("generated key contains invalid encoding") from None
            with os.fdopen(fd, "wb", closefd=False) as out:
                out.write((value + "\n").encode("ascii"))
                out.flush()
                os.fsync(out.fileno())
            created = True
        key = validate_grant(response, alias, master)
        if not created or key != value:
            raise KeyErrorSafe("generated key could not be saved")
    finally:
        os.close(fd)
        if not created:
            os.unlink(output)
    # Re-check with restricted key without printing its value. Keep the key
    # file if the scope check fails so an operator can explicitly revoke.
    try:
        visible = models(api(opener, base, "/v1/models", key))
    except KeyErrorSafe:
        raise KeyErrorSafe("key was created but alias scope could not be checked; revoke using saved file") from None
    if visible != {alias}:
        raise KeyErrorSafe("key was created but model-list scope mismatch; revoke using saved file")
    return key_alias


def run_revoke(opener, base, master, output):
    key = ascii_secret(protected_read(output))
    if key == master:
        raise KeyErrorSafe("refusing to revoke gateway master")
    try:
        response = api(opener, base, "/key/delete", master, {"keys": [key]})
    except KeyErrorSafe:
        raise KeyErrorSafe("key deletion uncertain; retain protected file for investigation") from None
    if (not isinstance(response, dict) or not isinstance(response.get("deleted_keys"), list)
            or not response["deleted_keys"]):
        raise KeyErrorSafe("key deletion unconfirmed; retain protected file")
    # Some LiteLLM versions report a hashed ID, so verify revocation with an
    # authenticated endpoint rather than comparing returned secret text.
    req = Request(base + "/v1/models", headers={"Authorization": "Bearer " + key})
    try:
        with opener.open(req, timeout=10):
            raise KeyErrorSafe("revoked key still accepted; retain protected file")
    except HTTPError as exc:
        if exc.code not in (401, 403):
            raise KeyErrorSafe("key revocation not confirmed; retain protected file") from None
    except (URLError, TimeoutError, OSError):
        raise KeyErrorSafe("key revocation cannot be confirmed; retain protected file") from None
    os.unlink(output)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--alias", required=True, help="Exactly one authorized registered LiteLLM model alias")
    parser.add_argument("--output", required=True, help="Absolute key path inside existing owner-only directory")
    parser.add_argument("--gateway-id", default="local")
    action = parser.add_mutually_exclusive_group()
    action.add_argument("--issue", action="store_true", help="Generate one short-lived bounded key; stores secret in output file")
    action.add_argument("--revoke", action="store_true", help="Revoke the saved key and remove its local file")
    args = parser.parse_args(argv)
    try:
        base = endpoint(args.endpoint)
        if not ALIAS.fullmatch(args.alias) or not KEY_ALIAS.fullmatch(args.gateway_id):
            raise KeyErrorSafe("invalid alias or gateway identifier")
        if not args.issue and not args.revoke:
            if not Path(args.output).is_absolute() or str(args.output).startswith("/nix/store/"):
                raise KeyErrorSafe("key output must be an absolute path outside Nix store")
            print(f"DRY RUN: would manage one model-scoped pilot key for {args.alias}; no secrets, files or network accessed")
            return 0
        path = secure_output_path(args.output, creating=not args.revoke)
        master = read_master(args.gateway_id)
        opener = build_opener(ProxyHandler({}), NoRedirect())
        if args.issue:
            key_alias = run_issue(opener, base, args.alias, master, path)
            print(f"PASS: restricted LiteLLM key saved (owner-only); remote key alias {key_alias}; expires within 1h; no key printed")
        else:
            run_revoke(opener, base, master, path)
            print("PASS: restricted LiteLLM key revoked and local key file removed")
        return 0
    except (KeyErrorSafe, OSError) as exc:
        if isinstance(exc, OSError):
            print("FAIL: protected key file operation failed; remote key outcome may require inspection", file=sys.stderr)
        else:
            print("FAIL: " + str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
