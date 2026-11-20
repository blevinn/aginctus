"""Operator-only gateway spend-log correlation probe; never proves inference.

Reads one record from the private pinned LiteLLM proxy's administrative
spend-log endpoint. Even a correlated row is not a provider completion trace.
No provider request, key issuance, or acceptance-run execution occurs.
"""

import argparse
import hashlib
import ipaddress
import json
import os
import re
import stat
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

RUN_ID = re.compile(r"aginctus-acceptance\.[A-Za-z0-9]{8}\Z", re.ASCII)
SESSION = re.compile(r"ses_[A-Za-z0-9_-]{8,128}\Z", re.ASCII)
IDENTIFIER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z", re.ASCII)
REQUEST = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,127}\Z", re.ASCII)
LOG_BYTES = 65536
SECRET_BYTES = 65536
PRIVATE_NETWORKS = tuple(ipaddress.ip_network(net) for net in (
    "127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
))


class ProbeError(Exception):
    """Never includes remote or user-supplied contents."""


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise ProbeError("gateway log query redirected")


def endpoint(raw):
    try:
        parsed = urlsplit(raw)
        ip = ipaddress.IPv4Address(parsed.hostname)
        ok = (
            parsed.scheme == "http" and parsed.port == 4000
            and not parsed.username and not parsed.password
            and parsed.path in ("", "/") and not parsed.query and not parsed.fragment
            and any(ip in network for network in PRIVATE_NETWORKS)
        )
    except (TypeError, ValueError):
        raise ProbeError("invalid private gateway endpoint") from None
    if not ok:
        raise ProbeError("invalid private gateway endpoint")
    return f"http://{ip}:4000"


def _private_file(path):
    if (not path.is_absolute() or path.parent.is_symlink()
            or str(path).startswith("/nix/store/")):
        raise ProbeError("protected file path invalid")
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as stream:
            meta = os.fstat(stream.fileno())
            if (not stat.S_ISREG(meta.st_mode) or meta.st_uid != os.geteuid()
                    or meta.st_mode & 0o077 or meta.st_size > SECRET_BYTES):
                raise ProbeError("protected file permissions invalid")
            data = stream.read(SECRET_BYTES + 1)
    except OSError:
        raise ProbeError("protected file unavailable") from None
    if len(data) > SECRET_BYTES:
        raise ProbeError("protected file oversized")
    return data


def _key(data):
    try:
        key = data.decode("ascii").strip()
    except UnicodeError:
        raise ProbeError("invalid protected credential") from None
    if not 16 <= len(key) <= 4096 or not key.isprintable() or any(c.isspace() for c in key):
        raise ProbeError("invalid protected credential")
    return key


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON field")
        result[key] = value
    return result


def _master(gateway_id):
    state = Path(os.environ.get("XDG_STATE_HOME", str(Path.home() / ".local/state")))
    path = state / "aginctus" / "gateway" / gateway_id / "secrets.json"
    try:
        record = json.loads(_private_file(path), object_pairs_hook=unique_object)
    except (UnicodeError, ValueError):
        raise ProbeError("invalid protected gateway state") from None
    if not isinstance(record, dict) or not isinstance(record.get("AGINCTUS_GATEWAY_MASTER_KEY"), str):
        raise ProbeError("missing gateway administrative credential")
    return _key(record["AGINCTUS_GATEWAY_MASTER_KEY"].encode("utf-8"))


def fetch_row(url, admin_key, request_id, opener=None):
    opener = opener or build_opener(ProxyHandler({}), NoRedirect())
    request = Request(
        url + "/spend/logs?request_id=" + quote(request_id, safe=""),
        headers={"Authorization": "Bearer " + admin_key, "Accept": "application/json"},
        method="GET",
    )
    try:
        with opener.open(request, timeout=15) as response:
            if response.status != 200:
                raise ProbeError("gateway spend log query failed")
            data = response.read(LOG_BYTES + 1)
    except (HTTPError, URLError, TimeoutError, OSError):
        raise ProbeError("gateway spend log query unavailable") from None
    if len(data) > LOG_BYTES:
        raise ProbeError("oversized gateway spend log result")
    try:
        rows = json.loads(data, object_pairs_hook=unique_object)
    except (UnicodeError, ValueError):
        raise ProbeError("invalid gateway spend log JSON") from None
    if not isinstance(rows, list) or len(rows) != 1 or not isinstance(rows[0], dict):
        raise ProbeError("missing or ambiguous gateway spend log")
    return rows[0]


def expected_fields(*, request_id, run_id, session_id, model_alias, key_sha256):
    # Fixed field names only. In pinned LiteLLM 1.103.0 session_id may be
    # a trace-id fallback and end_user may come from key/customer metadata;
    # values alone cannot attest native OpenCode session/run provenance.
    return (
        ("request_id", request_id),
        ("session_id", session_id),
        ("end_user", run_id),
        ("model_group", model_alias),
        ("api_key", key_sha256),
        ("call_type", "aresponses"),
    )


def check_row(row, *, request_id, run_id, session_id, model_alias, key_sha256):
    """Require exact logged fields; do not infer upstream completion."""
    expected = expected_fields(
        request_id=request_id, run_id=run_id, session_id=session_id,
        model_alias=model_alias, key_sha256=key_sha256,
    )
    if any(row.get(field) != wanted for field, wanted in expected):
        raise ProbeError("gateway spend log correlation incomplete")


def diagnose_row(row, *, request_id, run_id, session_id, model_alias, key_sha256):
    """Bounded field-level diagnostic; NEVER prints values or grants a pass."""
    expected = expected_fields(
        request_id=request_id, run_id=run_id, session_id=session_id,
        model_alias=model_alias, key_sha256=key_sha256,
    )
    fields = []
    for name, wanted in expected:
        actual = row.get(name)
        status = (
            "missing" if actual is None or actual == "" else
            "match" if actual == wanted else "mismatch"
        )
        fields.append({"id": name, "status": status})
    status = row.get("status")
    # In LiteLLM the spend-log status is derived from callback metadata.
    # A "success" label is NOT an independent provider completion receipt.
    label = (
        "success_label" if status == "success" else
        "failure_label" if status == "failure" else "unrecognized"
    )
    return {
        "schema_version": 1,
        "profile": "gateway_spend_diagnostic",
        "status": "incomplete",
        "gateway_log_correlated": "not_verified",
        "fields": fields,
        "spend_log_status": label,
        "upstream_inference": "not_verified",
        "agent_tool_execution": "not_verified",
        "complete_acceptance": False,
    }


def diagnostic_error():
    return {
        "schema_version": 1,
        "profile": "gateway_spend_diagnostic",
        "status": "error",
        "gateway_log_correlated": "not_verified",
        "fields": [
            {"id": field, "status": "not_run"}
            for field in (
                "request_id", "session_id", "end_user", "model_group",
                "api_key", "call_type",
            )
        ],
        "spend_log_status": "unrecognized",
        "upstream_inference": "not_verified",
        "agent_tool_execution": "not_verified",
        "complete_acceptance": False,
    }


def inspect(*, host, gateway_id, request_id, run_id, session_id, model_alias,
            key_file, opener=None, diagnose=False):
    if (not IDENTIFIER.fullmatch(gateway_id) or not REQUEST.fullmatch(request_id)
            or not RUN_ID.fullmatch(run_id) or not SESSION.fullmatch(session_id)
            or not IDENTIFIER.fullmatch(model_alias)):
        raise ProbeError("invalid gateway evidence selector")
    host = endpoint(host)
    # Read keys only after validating selectors and host. Never expose either.
    expected_hash = hashlib.sha256(_key(_private_file(Path(key_file))).encode("ascii")).hexdigest()
    master = _master(gateway_id)
    row = fetch_row(host, master, request_id, opener)
    selectors = {
        "request_id": request_id, "run_id": run_id,
        "session_id": session_id, "model_alias": model_alias,
        "key_sha256": expected_hash,
    }
    if diagnose:
        # Never use a diagnostics result as proof of logged request identity,
        # actual provider completion or acceptance. Its fields are hints only.
        return diagnose_row(row, **selectors)
    check_row(row, **selectors)
    return {"gateway_log_correlated": "pass", "upstream_inference": "not_verified",
            "agent_tool_execution": "not_verified", "complete_acceptance": False}


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    for option in ("endpoint", "gateway-id", "request-id", "run-id", "session-id",
                   "model-alias", "workload-key-file"):
        p.add_argument("--" + option, required=True)
    actions = p.add_mutually_exclusive_group()
    actions.add_argument("--inspect", action="store_true", help="Require exact gateway log correlation")
    actions.add_argument(
        "--diagnose", action="store_true",
        help="Read one gateway log and report only fixed missing/mismatched field labels",
    )
    args = p.parse_args(argv)
    if not (args.inspect or args.diagnose):
        print(json.dumps({"gateway_log_correlated": "not_run", "upstream_inference": "not_verified",
                          "agent_tool_execution": "not_verified", "complete_acceptance": False}))
        return 0
    try:
        report = inspect(
            host=args.endpoint, gateway_id=args.gateway_id,
            request_id=args.request_id, run_id=args.run_id,
            session_id=args.session_id, model_alias=args.model_alias,
            key_file=args.workload_key_file, diagnose=args.diagnose,
        )
    except (ProbeError, OSError, TypeError, ValueError):
        if args.diagnose:
            report = diagnostic_error()
        else:
            report = {"gateway_log_correlated": "error", "upstream_inference": "not_verified",
                      "agent_tool_execution": "not_verified", "complete_acceptance": False}
    print(json.dumps(report, separators=(",", ":")))
    if args.diagnose:
        # A successful diagnostic query is not evidence of correlation.
        return 0 if report["status"] == "incomplete" else 1
    return 0 if report["gateway_log_correlated"] == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
