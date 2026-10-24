"""Explicit, bounded reports for existing read-only pilot preflight checks.

Unlike \`just check ci\`, this operator-only entrypoint touches the configured
pilot. It never launches agent, VM, Herdr, provider, or live acceptance steps.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CHECKS = {
    "pilot-topology": "scripts/acceptance/opencode-pilot-preflight.py",
    "opencode-runtime": "scripts/acceptance/opencode-inference-preflight.py",
}
WORKLOAD = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}\Z")
TIMEOUT_SECONDS = 120
SAFE_ENV = {
    "AGINCTUS_INCUS_INTEGRATION": "0",
    "AGINCTUS_INCUS_DISPOSABLE": "NO",
    "AGINCTUS_ACCEPTANCE_LIVE": "",
    "AGINCTUS_ACCEPTANCE_LIVE_HOOKS": "",
    "AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "",
    "AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE": "",
    "AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT": "",
}


def unavailable(check: str) -> dict:
    """An unexecuted/launcher-invalid check cannot be reported as passing."""
    return {
        "schema_version": 1,
        "profile": "pilot_preflight",
        "status": "error",
        "summary": {"passed": 0, "failed": 0, "errors": 1},
        "results": [{"id": f"preflight/{check}", "status": "error", "exit_code": None}],
        "verification_tier": "read_only_pilot_preflight",
        "upstream_inference": "not_verified",
        "gateway_authorization": "not_verified",
        "egress_policy": "not_verified",
    }


def run_preflight(check: str, env: dict[str, str]) -> dict:
    """Run only one selected, fixed script and discard every output byte."""
    if check not in CHECKS:
        raise ValueError("unknown preflight selection")
    child_env = dict(env)
    child_env.update(SAFE_ENV)
    target = "configured_pilot"
    if check == "opencode-runtime":
        target = child_env.get("AGINCTUS_ACCEPTANCE_WORKLOAD", "aginctus-dev")
        if not WORKLOAD.fullmatch(target):
            return unavailable(check)
        child_env["AGINCTUS_ACCEPTANCE_WORKLOAD"] = target
    else:
        child_env.pop("AGINCTUS_ACCEPTANCE_WORKLOAD", None)
    start = time.monotonic()
    try:
        completed = subprocess.run(
            (sys.executable, str(ROOT / CHECKS[check])),
            cwd=ROOT,
            env=child_env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        status, code = "error", None
    else:
        code = completed.returncode
        status = "pass" if code == 0 else "fail"
    return {
        "schema_version": 1,
        "profile": "pilot_preflight",
        "status": status,
        "duration_ms": round((time.monotonic() - start) * 1000),
        "summary": {
            "passed": int(status == "pass"),
            "failed": int(status == "fail"),
            "errors": int(status == "error"),
        },
        "results": [
            {
                "id": f"preflight/{check}",
                "status": status,
                "exit_code": code,
                "duration_ms": round((time.monotonic() - start) * 1000),
                "rerun": f"just check preflight-report {check}",
            }
        ],
        "verification_tier": "read_only_pilot_preflight",
        "target": target,
        "upstream_inference": "not_verified",
        "gateway_authorization": "not_verified",
        "egress_policy": "not_verified",
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("check", choices=tuple(CHECKS))
    parser.add_argument("--format", choices=("text", "json"), default="text")
    args = parser.parse_args(argv)
    result = run_preflight(args.check, dict(os.environ))
    if args.format == "json":
        print(json.dumps(result, separators=(",", ":")))
    else:
        print(f"{result['status'].upper():5}  preflight/{args.check}")
        print("Verification: read-only pilot inspection only")
        print("NOT VERIFIED: gateway authorization, egress policy, upstream inference")
        if result["status"] != "pass":
            print("Inspect the native preflight explicitly for diagnostics")
    return 0 if result["status"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
