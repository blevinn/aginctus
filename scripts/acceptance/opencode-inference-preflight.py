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
ROOT = "/var/lib/aginctus/opencode"
CONFIG = ROOT + "/gateway.json"
KEY = ROOT + "/workload-key"
# This fixed source is interpreted on the owned guest, not derived from a
# selected path or guest-controlled value. No filenames or values are emitted.
CHECK_DIR = r'''
set -eu
root=/var/lib/aginctus/opencode
[ -d "$root" ] && [ ! -L "$root" ] || exit 1
[ ! -L "$root/gateway.json" ] && [ ! -L "$root/workload-key" ] || exit 1
[ -z "$(find "$root" -mindepth 1 -maxdepth 1 ! -name gateway.json ! -name workload-key -print -quit)" ] || exit 1
'''
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

    # Scope only the provisioner's known runtime directory, not all possible
    # secret mounts/process environments. Never open the key or emit filenames.
    directory = stage(
        "credential directory metadata",
        lambda: guest("stat", "-c", "%a %U %F", "--", ROOT),
    ).strip()
    if directory != "700 agent directory":
        raise RuntimeError("unsafe credential runtime directory")
    stage(
        "credential directory contents",
        lambda: guest("runuser", "-u", "agent", "--", "/bin/sh", "-c", CHECK_DIR),
    )
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

    # A closed, private runtime directory does not rule out credentials in
    # other mount points, process environments or user homes. The endpoint
    # does not prove gateway ownership, routing or real inference.
    return cfg["model"]


if __name__ == "__main__":
    try:
        check()
        print("PASS: OpenCode Responses runtime metadata and private endpoint syntax ready")
        print("NOT VERIFIED: other guest credentials/process environments, gateway ownership, key authorization, upstream inference and egress policy")
    except (RuntimeError, ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
        message = str(exc)
        known = ("credential directory metadata", "credential directory contents",
                 "credential metadata", "credential accessibility",
                 "gateway configuration metadata", "gateway configuration readability")
        label = next((name for name in known
                      if message == "readiness phase failed: " + name),
                     "configuration validation")
        print("FAIL: OpenCode inference readiness: " + label +
              " (details suppressed)", file=sys.stderr)
        sys.exit(1)
