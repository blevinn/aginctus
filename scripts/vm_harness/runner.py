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
import tempfile
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
STORE_ROOT = Path("/nix/store")
SCENARIOS = {
    "smoke": "vm-harness-smoke",
    "storage-network": "vm-harness-storage-network",
    "runtime-lifecycle": "vm-harness-runtime-lifecycle",
    "gateway-topology": "vm-harness-gateway-topology",
    "synthetic-gateway": "vm-harness-synthetic-gateway",
    "synthetic-tool-gateway": "vm-harness-synthetic-tool-gateway",
    "opencode-tool-loopback": "vm-harness-opencode-tool-loopback",
    "ownership-recovery": "vm-harness-ownership-recovery",
}
NETWORKED_SCENARIOS = {"gateway-topology", "synthetic-gateway", "synthetic-tool-gateway"}
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
    suffix = ".driver" if interactive or scenario in NETWORKED_SCENARIOS else ""
    return [
        "nix",
        "--quiet",
        "build",
        "--no-link",
        "--no-update-lock-file",
        "--print-out-paths",
        f".#{target}{suffix}",
    ]


def driver_from_build(output: str) -> Path:
    paths = output.strip().splitlines()
    if len(paths) != 1:
        raise ValueError("expected exactly one VM driver store path")
    store_path = Path(paths[0])
    if not store_path.is_absolute() or not store_path.is_relative_to(STORE_ROOT):
        raise ValueError("VM driver is not in the Nix store")
    try:
        store_path = store_path.resolve(strict=True)
        driver = (store_path / "bin" / "nixos-test-driver").resolve(strict=True)
    except (OSError, RuntimeError) as exc:
        raise ValueError("VM driver executable is missing or invalid") from exc
    if not store_path.is_relative_to(STORE_ROOT) or not driver.is_relative_to(
        STORE_ROOT
    ):
        raise ValueError("VM driver resolves outside the Nix store")
    if not driver.is_file() or not os.access(driver, os.X_OK):
        raise ValueError("VM driver executable is missing")
    return driver


@dataclass(frozen=True)
class Provenance:
    git_revision: str | None = None
    git_dirty: bool | None = None
    flake_lock_sha256: str | None = None


def capture_provenance(env: dict[str, str]) -> Provenance:
    """Capture best-effort source metadata before launching the build."""

    def git_output(*arguments: str) -> str | None:
        try:
            result = subprocess.run(
                ["git", *arguments],
                cwd=ROOT,
                env=env,
                text=True,
                capture_output=True,
                check=False,
            )
        except OSError:
            return None
        return result.stdout.strip() if result.returncode == 0 else None

    revision = git_output("rev-parse", "HEAD")
    # Nix's Git source includes tracked changes, not untracked worker files.
    status = git_output("status", "--porcelain=v1", "--untracked-files=no")
    try:
        lock_hash = hashlib.sha256((ROOT / "flake.lock").read_bytes()).hexdigest()
    except OSError:
        lock_hash = None
    return Provenance(revision, None if status is None else bool(status), lock_hash)


def write_report(
    scenario: str,
    exit_code: int,
    provenance: Provenance,
    output_root: Path,
    *,
    launch_failed: bool = False,
    runtime_failed: bool = False,
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
    record = {
        "schema_version": 1,
        "run_id": run_id,
        "scenario": scenario,
        "status": "pass" if exit_code == 0 else "fail",
        "exit_code": exit_code,
        "created_utc": datetime.now(timezone.utc).isoformat(),
        "failure_stage": "launch"
        if launch_failed
        else ("runtime" if runtime_failed else ("build" if exit_code else None)),
        "git_revision": provenance.git_revision,
        "git_dirty": provenance.git_dirty,
        "flake_lock_sha256": provenance.flake_lock_sha256,
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
        provenance = capture_provenance(env) if args.mode == "test" else Provenance()
        command = build_arguments(args.scenario, args.mode == "debug")
        launch_failed = False
        runtime_failed = False
        try:
            result = subprocess.run(
                command,
                cwd=ROOT,
                env=env,
                text=True,
                capture_output=True,
                check=False,
            )
        except OSError as exc:
            if args.mode != "test":
                raise
            launch_failed = True
            result = subprocess.CompletedProcess(
                command, 1, "", f"VM launcher failed: {exc}\n"
            )
        if (
            args.mode == "test"
            and args.scenario in NETWORKED_SCENARIOS
            and result.returncode == 0
        ):
            driver = driver_from_build(result.stdout)
            # OCI provisioning needs worker network access, unavailable in a
            # Nix build sandbox. Run only the immutable allowlisted driver.
            with tempfile.TemporaryDirectory(prefix="aginctus-vm-") as state:
                try:
                    result = subprocess.run(
                        [str(driver), "--no-interactive"],
                        cwd=state,
                        env=env,
                        text=True,
                        capture_output=True,
                        check=False,
                        timeout=900,
                    )
                except subprocess.TimeoutExpired:
                    result = subprocess.CompletedProcess(
                        [], 1, "", "VM runtime timed out\n"
                    )
                except OSError:
                    result = subprocess.CompletedProcess(
                        [], 1, "", "VM runtime could not start\n"
                    )
            runtime_failed = result.returncode != 0
        if args.mode == "test":
            report = write_report(
                args.scenario,
                result.returncode,
                provenance,
                ROOT / ".vm-harness-results",
                launch_failed=launch_failed,
                runtime_failed=runtime_failed,
            )
            print(f"VM evidence: {report}")
        if result.returncode != 0:
            print(result.stderr, file=sys.stderr, end="")
            return result.returncode
        if args.mode == "test":
            if args.scenario in NETWORKED_SCENARIOS:
                print(f"PASS: networked VM scenario: {args.scenario}")
                return 0
            print(f"PASS: VM test derivation: {result.stdout.strip()}")
            return 0
        driver = driver_from_build(result.stdout)
        print(
            "Interactive VM driver: Python runs as your worker account.",
            file=sys.stderr,
        )
        print(
            "Use machine.succeed(...) for guest-only diagnostics; never use pilot secrets.",
            file=sys.stderr,
        )
        return subprocess.run(
            [str(driver), "--interactive"], cwd=ROOT, env=env, check=False
        ).returncode
    except (OSError, ValueError) as exc:
        print(f"VM launcher failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
