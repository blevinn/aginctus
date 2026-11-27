"""Sanitized evidence projection for the trusted disposable worker fixture.

The returned object describes observed guest-side checks only. It is not
a fresh-build attestation, host cleanup proof, or authorization token.
"""

import re as receipt_re

REQUIRED_LIFECYCLE = (
    "project_policy",
    "ephemeral_lifecycle",
    "retained_lifecycle",
    "scoped_ttl_recovery",
    "root_volumes",
    "foreign_preserved",
    "instance_cleanup",
)
UNVERIFIED = (
    "crash_reboot",
    "nested_isolation",
    "storage_reclamation",
    "report_retention",
)


def fixture_receipt(
    report, scenario, source_revision, lock_sha256, image_fingerprint, *,
    require_payload_transfer=False, require_nested_kvm=False,
):
    if (
        report.get("schema_version") != 1
        or report.get("profile") != "dedicated_worker"
        or report.get("evidence_tier") != "disposable_guest_no_live_provider"
        or report.get("status") != "pass"
        or report.get("scenario", "") != scenario
        or report.get("image_fingerprint") != image_fingerprint
        or not receipt_re.fullmatch(r"[0-9a-f]{64}", image_fingerprint)
        or not receipt_re.fullmatch(r"[0-9a-f]{64}", lock_sha256)
    ):
        raise ValueError("worker fixture receipt identity or status unverified")
    if scenario not in ("", "native-instance"):
        raise ValueError("unreviewed fixture scenario")
    checks = {}
    for item in report["results"]:
        key = item["id"]
        if key in checks:
            raise ValueError("duplicate worker evidence check")
        checks[key] = item["status"]
    required = list(REQUIRED_LIFECYCLE)
    if scenario:
        required.append("native_instance_scenario")
    elif checks.get("native_instance_scenario") != "not_verified":
        raise ValueError("unexpected scenario evidence")
    if require_payload_transfer:
        if scenario:
            raise ValueError("payload fixture must use the base image lifecycle")
        required.append("payload_transfer")
    elif checks.get("payload_transfer") != "not_verified":
        raise ValueError("unexpected payload evidence")
    if require_nested_kvm:
        if scenario or require_payload_transfer:
            raise ValueError("nested KVM fixture must use the base image lifecycle alone")
        required.append("nested_kvm_guest_execution")
    elif checks.get("nested_kvm_guest_execution") != "not_verified":
        raise ValueError("unexpected nested KVM evidence")
    if any(checks.get(key) != "pass" for key in required):
        raise ValueError("worker lifecycle or scenario incomplete")
    if any(checks.get(key) != "not_verified" for key in UNVERIFIED):
        raise ValueError("unexpected evidence scope")
    unverified = list(UNVERIFIED)
    if not require_payload_transfer:
        unverified.append("payload_transfer")
    if not require_nested_kvm:
        unverified.append("nested_kvm_guest_execution")
    fixture_scenario = scenario or "worker-lifecycle"
    if require_payload_transfer:
        fixture_scenario = "payload-transfer"
    if require_nested_kvm:
        fixture_scenario = "nested-kvm"
    return {
        "schema_version": 1,
        "profile": "managed_worker_fixture",
        "evidence_tier": "disposable_guest_no_live_provider",
        "scenario": fixture_scenario,
        "status": "pass",
        "source_revision": source_revision,
        "flake_lock_sha256": lock_sha256,
        "image_fingerprint": image_fingerprint,
        "results": [
            {"id": key, "status": "pass"} for key in required
        ] + [
            {"id": key, "status": "not_verified"} for key in unverified
        ],
        "host_teardown": "not_verified",
        "agent_admission": "not_verified",
    }
