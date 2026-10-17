#!/usr/bin/env python3
"""Read-only pilot gate for model-backed #124 runs; never reads the workload key."""
import json
import os
import re
import subprocess
import sys
from urllib.parse import urlsplit

INSTANCE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")
CONFIG = "/var/lib/aginctus/opencode/gateway.json"
KEY = "/var/lib/aginctus/opencode/workload-key"

def guest(*argv):
    try:
        result = subprocess.run(["incus", "exec", os.environ.get(
            "AGINCTUS_ACCEPTANCE_WORKLOAD", "aginctus-dev"), "--", *argv],
            capture_output=True, text=True, timeout=25, check=True)
        return result.stdout
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError("guest readiness check failed") from None

def check():
    workload = os.environ.get("AGINCTUS_ACCEPTANCE_WORKLOAD", "aginctus-dev")
    if not INSTANCE.fullmatch(workload):
        raise RuntimeError("invalid managed workload name")
    # Metadata is checked by root. The secret itself is NEVER opened/read.
    metadata = guest("stat", "-c", "%a %U %F", "--", KEY).strip()
    if metadata != "600 agent regular file":
        raise RuntimeError("unsafe or absent workload credential metadata")
    guest("runuser", "-u", "agent", "--", "test", "-r", KEY)
    cfg_meta = guest("stat", "-c", "%F", "--", CONFIG).strip()
    if cfg_meta != "regular file":
        raise RuntimeError("missing regular gateway configuration")
    cfg = json.loads(guest("runuser", "-u", "agent", "--", "cat", CONFIG))
    if not isinstance(cfg, dict) or set(cfg) != {"endpoint", "model", "protocol"}:
        raise RuntimeError("invalid gateway configuration fields")
    if cfg["protocol"] != "responses":
        raise RuntimeError("Responses API not configured")
    if not isinstance(cfg["endpoint"], str) or not isinstance(cfg["model"], str):
        raise RuntimeError("invalid gateway configuration types")
    url = urlsplit(cfg["endpoint"])
    if (url.scheme != "http" or not url.hostname or url.username or url.password
            or url.path != "/v1" or url.query or url.fragment or not url.port):
        raise RuntimeError("invalid private gateway endpoint")
    if not cfg["model"] or cfg["model"].strip() != cfg["model"]:
        raise RuntimeError("invalid model selection")
    # DNS only; this is not proof the gateway is reachable, let alone inference.
    host = url.hostname
    if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}", host):
        raise RuntimeError("invalid gateway hostname")
    guest("runuser", "-u", "agent", "--", "getent", "hosts", host)
    return cfg["model"]

if __name__ == "__main__":
    try:
        model = check()
        print("PASS: private OpenCode Responses runtime files and guest DNS ready")
        print("NOT VERIFIED: actual gateway reachability, upstream inference, agent prompt, and egress policy")
    except (RuntimeError, ValueError, KeyError, TypeError, json.JSONDecodeError):
        print("FAIL: OpenCode inference runtime readiness (details suppressed)", file=sys.stderr)
        sys.exit(1)
