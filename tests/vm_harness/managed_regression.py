"""Fixed integrated disposable regression, invoked by the existing VM runner.

No host daemon access, arbitrary target input, or agent admission. The
aggregate report is scoped to these derivations and their fixture receipts.
"""

from __future__ import annotations

import json
import os
import re
import stat
import subprocess
import sys
import tempfile
from pathlib import Path

SUITE = (
    ("vm-harness-native-instance", None, ()),
    ("testenv-disposable-worker-fixture", "worker-lifecycle", ()),
    ("testenv-managed-native-instance", "native-instance", ("native_instance_scenario",)),
    ("testenv-independent-recovery", "independent-recovery", ()),
    ("testenv-payload-transfer", "payload-transfer", ("payload_transfer",)),
    ("testenv-nested-kvm", "nested-kvm", ("nested_kvm_guest_execution",)),
    ("testenv-project-admission", "project-admission", ("project_device_denials", "project_aggregate_limits")),
    ("testenv-runtime-resources", "runtime-resources", ("guest_cpu_count", "guest_memory_capacity", "guest_root_disk_capacity")),
    ("testenv-offline-network", "offline-network", ("offline_tcp_udp_boundary", "guest_firewall_independence")),
)
# Checked against the canonical Go catalog by TestManagedFixtureTargetParity.
MANAGED_SCENARIOS = {
    "native-instance": "testenv-managed-native-instance",
}
LIFECYCLE = (
    "project_policy", "ephemeral_lifecycle", "retained_lifecycle", "scoped_ttl_recovery",
    "root_volumes", "foreign_preserved", "instance_cleanup",
)
UNVERIFIED = (
    "crash_reboot", "nested_isolation", "storage_reclamation", "report_retention",
    "payload_transfer", "nested_kvm_guest_execution",
)
RECOVERY = {
    "schema_version": 1, "profile": "independent_worker_recovery_fixture", "status": "pass",
    "sigkill_recovery": "pass", "reboot_recovery": "pass", "root_volume_absence": "pass",
    "foreign_preserved": "pass", "host_teardown": "not_verified", "physical_reclamation": "not_verified",
}
MAX_RECEIPT = 8192
STORE_PATH = re.compile(r"/nix/store/[0-9a-z]{32}-[A-Za-z0-9+._?=-]+")
HEX64 = re.compile(r"[0-9a-f]{64}")
REVISION = re.compile(r"[0-9a-f]{40}")


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate receipt field")
        result[key] = value
    return result


def read_receipt(output, name):
    if name not in ("testenv-fixture-receipt.json", "testenv-recovery-receipt.json"):
        raise ValueError("unrecognized receipt filename")
    directory = os.open(output, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_RECEIPT:
                raise ValueError("invalid fixture receipt type or size")
            with os.fdopen(os.dup(fd), "rb") as source:
                payload = source.read(MAX_RECEIPT + 1)
        finally:
            os.close(fd)
    finally:
        os.close(directory)
    if len(payload) > MAX_RECEIPT:
        raise ValueError("oversized fixture receipt")
    return json.loads(payload, object_pairs_hook=_unique_object)


def validate_receipt(record, scenario, extra, revision, lock_digest):
    if not isinstance(record, dict) or type(record.get("schema_version")) is not int:
        raise ValueError("invalid receipt")
    if scenario == "independent-recovery":
        if record != RECOVERY:
            raise ValueError("invalid independent recovery evidence")
        return record
    fields = {
        "schema_version", "profile", "evidence_tier", "scenario", "status",
        "source_revision", "flake_lock_sha256", "image_fingerprint", "results",
        "host_teardown", "agent_admission",
    }
    if (
        set(record) != fields or record["schema_version"] != 1
        or record["profile"] != "managed_worker_fixture"
        or record["evidence_tier"] != "disposable_guest_no_live_provider"
        or record["scenario"] != scenario or record["status"] != "pass"
        or record["source_revision"] != revision or record["flake_lock_sha256"] != lock_digest
        or record["host_teardown"] != "not_verified" or record["agent_admission"] != "not_verified"
        or not isinstance(record["image_fingerprint"], str)
        or not HEX64.fullmatch(record["image_fingerprint"])
        or not isinstance(record["results"], list)
    ):
        raise ValueError("fixture receipt identity or scope mismatch")
    required = dict.fromkeys((*LIFECYCLE, *extra), "pass")
    expected = required | {name: "not_verified" for name in UNVERIFIED if name not in required}
    observed = {}
    for check in record["results"]:
        if not isinstance(check, dict) or set(check) != {"id", "status"}:
            raise ValueError("invalid fixture gate")
        key = check["id"]
        if not isinstance(key, str) or key in observed:
            raise ValueError("duplicate or invalid fixture gate")
        observed[key] = check["status"]
    if observed != expected:
        raise ValueError("fixture gates incomplete or out of scope")
    return record


def store_path(value, *, derivation=False):
    path = value.strip()
    if not STORE_PATH.fullmatch(path) or path.endswith(".drv") != derivation:
        raise ValueError("expected one Nix store path")
    return path


def evaluate(root, env, target):
    result = subprocess.run(
        ["nix", "eval", "--no-update-lock-file", "--raw", f".#{target}.drvPath"],
        cwd=root, env=env, capture_output=True, text=True, timeout=120, check=True,
    )
    return store_path(result.stdout, derivation=True)


def build(root, env, drv):
    # Separate stdout (one output path) from streamed diagnostics. Observe
    # the matching derivation's driver log, not arbitrary success strings.
    prefix = Path(drv).name[33:-4] + "> "
    saw_command = False
    saw_completion = False
    command = [
        "timeout", "--kill-after=15s", "90m", "nix", "build", "--no-link",
        "--option", "sandbox", "true", "--option", "builders", "",
        "--option", "substitute", "true", "--max-jobs", "4", "--cores", "8",
        "--print-out-paths", "-L", drv + "^out",
    ]
    with tempfile.TemporaryFile() as stdout, subprocess.Popen(
        command, cwd=root, env=env, stdout=stdout, stderr=subprocess.PIPE,
    ) as process:
        try:
            while line := process.stderr.readline(8192):
                text = line.decode("utf-8", errors="replace")
                sys.stderr.write(text)
                if text.startswith(prefix):
                    saw_command |= "machine: must succeed:" in text
                    saw_completion |= "test script finished in" in text
            if process.wait():
                raise ValueError("fixture build failed")
        finally:
            if process.poll() is None:
                process.terminate()
        stdout.seek(0)
        raw_output = stdout.read(4097)
        if len(raw_output) > 4096:
            raise ValueError("oversized build output")
        output = store_path(raw_output.decode("ascii"))
    freshness = "fresh" if saw_command and saw_completion else "not_verified"
    return output, freshness


def run(root, env, provenance):
    return _run(root, env, provenance, SUITE, "managed_fixture_regression")


def run_scenario(root, env, provenance, scenario):
    target = MANAGED_SCENARIOS.get(scenario)
    if target is None:
        raise ValueError("managed scenario unavailable")
    selection = tuple(item for item in SUITE if item[:2] == (target, scenario))
    if len(selection) != 1:
        raise ValueError("managed scenario receipt contract unavailable")
    return _run(root, env, provenance, selection, "managed_fixture_scenario")


def _run(root, env, provenance, suite, profile):
    initial = provenance()
    revision = initial.git_revision
    digest = initial.flake_lock_sha256
    report = {
        "schema_version": 1, "profile": profile, "status": "fail",
        "source_revision": revision, "flake_lock_sha256": digest,
        "host_teardown": "not_verified", "agent_admission": "not_verified",
        "results": [{"target": target, "status": "not_run"} for target, _, _ in suite],
    }
    if (
        initial.git_dirty is not False or not isinstance(revision, str) or not REVISION.fullmatch(revision)
        or not isinstance(digest, str) or not HEX64.fullmatch(digest)
    ):
        report["failure_stage"] = "source"
        print(json.dumps(report, sort_keys=True))
        return 1
    base_fingerprint = None
    for entry, (target, scenario, extra) in zip(report["results"], suite, strict=True):
        stage = "source"
        try:
            if provenance() != initial:
                raise ValueError("source changed")
            stage = "evaluate"
            entry["derivation"] = evaluate(root, env, target)
            stage = "build"
            entry["output"], entry["freshness"] = build(root, env, entry["derivation"])
            stage = "source"
            if provenance() != initial:
                raise ValueError("source changed")
            if scenario is not None:
                stage = "receipt"
                name = ("testenv-recovery-receipt.json" if scenario == "independent-recovery"
                        else "testenv-fixture-receipt.json")
                record = validate_receipt(read_receipt(entry["output"], name), scenario, extra, revision, digest)
                if scenario not in ("independent-recovery", "native-instance"):
                    fingerprint = record["image_fingerprint"]
                    if base_fingerprint is not None and base_fingerprint != fingerprint:
                        raise ValueError("base image fingerprint mismatch")
                    base_fingerprint = fingerprint
                entry["receipt"] = record
            entry["status"] = "pass"
            print(f"PASS: {target} ({entry['freshness']})", file=sys.stderr)
        except (OSError, ValueError, subprocess.SubprocessError, KeyboardInterrupt):
            entry["status"] = "fail"
            entry["failure_stage"] = stage
            print(f"FAIL: {target} ({stage})", file=sys.stderr)
            break
    if all(entry["status"] == "pass" for entry in report["results"]):
        report["status"] = "pass"
    print(json.dumps(report, sort_keys=True))
    return 0 if report["status"] == "pass" else 1
