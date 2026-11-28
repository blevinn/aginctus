"""Hostless contract checks for the fixed managed regression launcher."""

import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from tests.vm_harness import managed_regression as regression
from tests.vm_harness import runner

REVISION = "a" * 40
DIGEST = "b" * 64
FINGERPRINT = "c" * 64
PROVENANCE = runner.Provenance(REVISION, False, DIGEST)
DRV = "/nix/store/" + "a" * 32 + "-vm-test.drv"
OUTPUT = "/nix/store/" + "b" * 32 + "-vm-test"


def receipt(scenario, extra):
    if scenario == "independent-recovery":
        return dict(regression.RECOVERY)
    required = dict.fromkeys((*regression.LIFECYCLE, *extra), "pass")
    checks = required | {
        name: "not_verified" for name in regression.UNVERIFIED if name not in required
    }
    return {
        "schema_version": 1,
        "profile": "managed_worker_fixture",
        "evidence_tier": "disposable_guest_no_live_provider",
        "scenario": scenario,
        "status": "pass",
        "source_revision": REVISION,
        "flake_lock_sha256": DIGEST,
        "image_fingerprint": FINGERPRINT,
        "host_teardown": "not_verified",
        "agent_admission": "not_verified",
        "results": [{"id": key, "status": value} for key, value in checks.items()],
    }


class ManagedRegressionTests(unittest.TestCase):
    def test_all_receipt_contracts(self):
        for _, scenario, extra in regression.SUITE:
            if scenario is not None:
                with self.subTest(scenario=scenario):
                    record = receipt(scenario, extra)
                    self.assertEqual(
                        regression.validate_receipt(record, scenario, extra, REVISION, DIGEST), record,
                    )

    def test_rejects_receipt_identity_scope_and_gate_errors(self):
        scenario = "payload-transfer"
        extra = ("payload_transfer",)
        for change in (
            {"source_revision": "d" * 40},
            {"flake_lock_sha256": "d" * 64},
            {"scenario": "worker-lifecycle"},
            {"schema_version": True},
            {"host_teardown": "pass"},
            {"image_fingerprint": "invalid"},
            {"secret": "CANARY"},
            {"results": []},
        ):
            record = receipt(scenario, extra)
            record.update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                regression.validate_receipt(record, scenario, extra, REVISION, DIGEST)
        for change in ("duplicate", "failed", "unverified", "unknown"):
            record = receipt(scenario, extra)
            checks = record["results"]
            if change == "duplicate":
                checks.append(checks[0])
            elif change == "failed":
                checks[0]["status"] = "fail"
            elif change == "unverified":
                checks[0]["status"] = "not_verified"
            else:
                checks.append({"id": "unreviewed", "status": "pass"})
            with self.subTest(change=change), self.assertRaises(ValueError):
                regression.validate_receipt(record, scenario, extra, REVISION, DIGEST)

    def test_receipt_reader_rejects_duplicate_keys_and_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "testenv-fixture-receipt.json"
            path.write_text('{"status":"pass","status":"fail"}')
            with self.assertRaises(ValueError):
                regression.read_receipt(root, path.name)
            path.unlink()
            path.symlink_to(root / "missing")
            with self.assertRaises(OSError):
                regression.read_receipt(root, path.name)

    def test_fixed_suite_and_fail_closed_reporting(self):
        records = [receipt(scenario, extra) for _, scenario, extra in regression.SUITE if scenario is not None]
        for fail in (False, True):
            results = list(records)
            if fail:
                results[1] = {**results[1], "source_revision": "d" * 40}
            with (
                self.subTest(fail=fail),
                patch.object(regression, "evaluate", return_value=DRV) as evaluate,
                patch.object(regression, "build", return_value=(OUTPUT, "not_verified")),
                patch.object(regression, "read_receipt", side_effect=results),
                contextlib.redirect_stdout(io.StringIO()) as stdout,
                contextlib.redirect_stderr(io.StringIO()),
            ):
                code = regression.run(Path("."), {}, lambda: PROVENANCE)
            report = json.loads(stdout.getvalue())
            self.assertEqual(code, int(fail))
            self.assertEqual(report["status"], "fail" if fail else "pass")
            self.assertEqual(
                [call.args[2] for call in evaluate.call_args_list],
                [target for target, _, _ in regression.SUITE[:3 if fail else len(regression.SUITE)]],
            )
            self.assertEqual(report["results"][2]["status"], "fail" if fail else "pass")
            self.assertEqual(report["results"][-1]["status"], "not_run" if fail else "pass")

    def test_dirty_source_refuses_before_evaluation(self):
        with (
            patch.object(regression, "evaluate") as evaluate,
            contextlib.redirect_stdout(io.StringIO()) as stdout,
        ):
            code = regression.run(Path("."), {}, lambda: runner.Provenance(REVISION, True, DIGEST))
        self.assertEqual(code, 1)
        self.assertEqual(json.loads(stdout.getvalue())["failure_stage"], "source")
        evaluate.assert_not_called()

    def test_store_paths_are_single_fixed_paths(self):
        self.assertEqual(regression.store_path(DRV, derivation=True), DRV)
        self.assertEqual(regression.store_path(OUTPUT), OUTPUT)
        for value in ("/tmp/probe", OUTPUT + "/file", OUTPUT + "\n" + OUTPUT, DRV):
            with self.subTest(value=value), self.assertRaises(ValueError):
                regression.store_path(value)

    def test_runner_dispatches_only_fixed_regression(self):
        with (
            patch.object(regression, "run", return_value=0) as run,
            contextlib.redirect_stderr(io.StringIO()),
        ):
            self.assertEqual(runner.main(["managed-regression"]), 0)
            with self.assertRaises(SystemExit):
                runner.main(["managed-regression", "smoke"])
        run.assert_called_once()


if __name__ == "__main__":
    unittest.main()
