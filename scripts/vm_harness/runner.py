#!/usr/bin/env python3
"""Opt-in NixOS VM test launcher; never connects to host Incus.

The interactive NixOS test driver is a Python REPL running as the calling
UNPRIVILEGED Unix account. It is NOT a security sandbox for that account:
run untrusted agents as a separate worker account, ideally on a dedicated host.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import secrets
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = {
    "smoke": "vm-harness-smoke",
    "storage-network": "vm-harness-storage-network",
    "runtime-lifecycle": "vm-harness-runtime-lifecycle",
    "ownership-recovery": "vm-harness-ownership-recovery",
}
# Explicit allowlist: do not propagate secrets, Incus remotes, Nix build hooks,
# QEMU arguments, inherited integration flags or SSH agents to test processes.
PASSTHROUGH = ("PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TERM")


def clean_environment(source: dict[str, str]) -> dict[str, str]:
    clean = {key: source[key] for key in PASSTHROUGH if key in source}
    clean["AGINCTUS_INCUS_INTEGRATION"] = "0"
    clean["AGINCTUS_INCUS_DISPOSABLE"] = "NO"
    clean["AGINCTUS_ACCEPTANCE_LIVE"] = ""
    return clean


def build_arguments(scenario: str, interactive: bool) -> list[str]:
    target = SCENARIOS[scenario]
    suffix = ".driver" if interactive else ""
    return [
        "nix", "build", "--no-link", "--no-update-lock-file",
        "--print-out-paths", f".#{target}{suffix}",
    ]


def driver_from_build(output: str) -> Path:
    paths = output.strip().splitlines()
    if len(paths) != 1:
        raise ValueError("expected exactly one VM driver store path")
    store_path = Path(paths[0])
    if not store_path.is_absolute() or not store_path.is_relative_to("/nix/store"):
        raise ValueError("VM driver is not in the Nix store")
    driver = store_path / "bin" / "nixos-test-driver"
    if not driver.is_file():
        raise ValueError("VM driver executable is missing")
    return driver


def write_report(
    scenario: str, exit_code: int, revision: str | None, output_root: Path
) -> Path:
    """Write synthetic-only, independently owned outcome metadata.

    Do not copy VM stdout/stderr, provider payloads, headers or credentials.
    A green Nix derivation never proves live upstream model inference.
    """
    if output_root.is_symlink():
        raise ValueError("refusing symlinked VM evidence root")
    output_root.mkdir(mode=0o700, exist_ok=True)
    if not output_root.is_dir():
        raise ValueError("VM evidence root is not a directory")
    run_id = secrets.token_hex(12)
    run_dir = output_root / run_id
    run_dir.mkdir(mode=0o700)
    lock_path = ROOT / "flake.lock"
    lock_hash = hashlib.sha256(lock_path.read_bytes()).hexdigest() if lock_path.is_file() else None
    record = {
        "schema_version": 1,
        "run_id": run_id,
        "scenario": scenario,
        "status": "pass" if exit_code == 0 else "fail",
        "exit_code": exit_code,
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "git_revision": revision,
        "flake_lock_sha256": lock_hash,
        "verification_tier": "disposable_guest_no_live_provider",
        "upstream_inference": "not_verified",
        "cleanup": "not_verified",
    }
    report = run_dir / "result.json"
    fd = os.open(report, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as out:
        json.dump(record, out, indent=2, sort_keys=True)
        out.write("\n")
    return report


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("test", "debug"))
    parser.add_argument("scenario", choices=tuple(SCENARIOS))
    args = parser.parse_args(argv)

    if args.mode == "debug" and not sys.stdin.isatty():
        parser.error("debug requires an interactive terminal")
    env = clean_environment(dict(os.environ))
    try:
        result = subprocess.run(
            build_arguments(args.scenario, args.mode == "debug"),
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        if args.mode == "test":
            git = subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=ROOT, env=env,
                text=True, capture_output=True, check=False,
            )
            revision = git.stdout.strip() if git.returncode == 0 else None
            report = write_report(
                args.scenario, result.returncode, revision,
                ROOT / ".vm-harness-results",
            )
            print(f"VM evidence: {report}")
        if result.returncode != 0:
            print(result.stderr, file=sys.stderr, end="")
            return result.returncode
        if args.mode == "test":
            print(f"PASS: VM test derivation: {result.stdout.strip()}")
            return 0
        driver = driver_from_build(result.stdout)
        print("Interactive VM driver: Python runs as your worker account.", file=sys.stderr)
        print("Use machine.succeed(...) for guest-only diagnostics; never use pilot secrets.", file=sys.stderr)
        return subprocess.run(
            [str(driver), "--interactive"], cwd=ROOT, env=env, check=False
        ).returncode
    except (OSError, ValueError) as exc:
        print(f"VM launcher failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
