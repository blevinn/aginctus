"""Read-only #124 runtime readiness gate; never reads the workload key."""
import ipaddress
import json
import os
import re
import subprocess
import sys
from urllib.parse import urlsplit

INSTANCE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")
ALIAS = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
CONFIG = "/var/lib/aginctus/opencode/gateway.json"
KEY = "/var/lib/aginctus/opencode/workload-key"
PRIVATE = tuple(ipaddress.IPv4Network(prefix) for prefix in
                ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"))


def guest(*argv):
    # Runtime provisioning targets the default Incus project explicitly.
    workload = os.environ.get("AGINCTUS_ACCEPTANCE_WORKLOAD", "aginctus-dev")
    try:
        result = subprocess.run(
            ["incus", "--project", "default", "exec", workload, "--", *argv],
            capture_output=True, text=True, timeout=25, check=True)
        return result.stdout
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError("guest command failed") from None


def stage(label, fn):
    try:
        return fn()
    except (RuntimeError, ValueError, KeyError, TypeError, json.JSONDecodeError):
        raise RuntimeError("readiness phase failed: " + label) from None


def check():
    workload = os.environ.get("AGINCTUS_ACCEPTANCE_WORKLOAD", "aginctus-dev")
    if not INSTANCE.fullmatch(workload):
        raise RuntimeError("invalid managed workload name")

    # Metadata as root; the workload key itself is never opened or printed.
    metadata = stage("credential metadata",
                     lambda: guest("stat", "-c", "%a %U %F", "--", KEY)).strip()
    if metadata != "600 agent regular file":
        raise RuntimeError("unsafe or absent workload credential metadata")
    stage("credential accessibility",
          lambda: guest("runuser", "-u", "agent", "--", "test", "-r", KEY))
    cfg_meta = stage("gateway configuration metadata",
                     lambda: guest("stat", "-c", "%F", "--", CONFIG)).strip()
    if cfg_meta != "regular file":
        raise RuntimeError("missing regular gateway configuration")
    cfg = stage("gateway configuration readability",
                lambda: json.loads(guest("runuser", "-u", "agent", "--", "cat", CONFIG)))
    if not isinstance(cfg, dict) or set(cfg) != {"endpoint", "model", "protocol"}:
        raise RuntimeError("invalid gateway configuration fields")
    if cfg["protocol"] != "responses":
        raise RuntimeError("Responses API not configured")
    if not isinstance(cfg["endpoint"], str) or not isinstance(cfg["model"], str):
        raise RuntimeError("invalid gateway configuration types")
    if not ALIAS.fullmatch(cfg["model"]):
        raise RuntimeError("invalid model selection")

    # The managed v0.1 provisioning CLI derives a private IPv4 on the
    # management NIC and writes http://IP:4000/v1, not a DNS hostname.
    endpoint = cfg["endpoint"]
    if any(ch.isspace() for ch in endpoint):
        raise RuntimeError("invalid private gateway endpoint")
    try:
        url = urlsplit(endpoint)
        port = url.port
    except ValueError:
        raise RuntimeError("invalid private gateway endpoint") from None
    if (url.scheme != "http" or not url.hostname or url.username is not None
            or url.password is not None or url.path != "/v1"
            or url.query or url.fragment or port != 4000):
        raise RuntimeError("invalid private gateway endpoint")
    try:
        address = ipaddress.IPv4Address(url.hostname)
    except ipaddress.AddressValueError:
        raise RuntimeError("invalid private gateway endpoint") from None
    if not any(address in network for network in PRIVATE):
        raise RuntimeError("invalid private gateway endpoint")

    # A configured private address does not prove ownership, routing, key
    # authorization, reachability or real upstream inference.
    return cfg["model"]


if __name__ == "__main__":
    try:
        check()
        print("PASS: OpenCode Responses runtime metadata and private endpoint syntax ready")
        print("NOT VERIFIED: gateway ownership/reachability, key authorization, upstream inference, agent prompt, and egress policy")
    except (RuntimeError, ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
        message = str(exc)
        known = ("credential metadata", "credential accessibility",
                 "gateway configuration metadata", "gateway configuration readability")
        label = next((name for name in known
                      if message == "readiness phase failed: " + name),
                     "configuration validation")
        print("FAIL: OpenCode inference readiness: " + label +
              " (details suppressed)", file=sys.stderr)
        sys.exit(1)
