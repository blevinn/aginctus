"""No host, Incus, VM or provider is needed to test coverage composition."""

import contextlib
import io
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.acceptance import local_report
from scripts.checks import ci_summary, coverage, preflight_report
from scripts.vm_harness import runner


def hermetic(*, failure=""):
    results = [
        {
            "id": name,
            "status": "fail" if name == failure else "pass",
            "exit_code": 17 if name == failure else 0,
            "duration_ms": 5,
            "rerun": f"just check {name}",
        }
        for name in ci_summary.EXPECTED_SUITES
    ]
    return {
        "schema_version": 1,
        "profile": "hermetic",
        "status": "fail" if failure else "pass",
        "summary": {"passed": 8 - int(bool(failure)), "failed": int(bool(failure)),
                    "errors": 0},
        "results": results,
    }


def pilot(check, code=0):
    with patch.object(
        preflight_report.subprocess, "run",
        return_value=subprocess.CompletedProcess([], code),
    ):
        return preflight_report.run_preflight(check, {})


def save(path, record):
    path.write_text(json.dumps(record), encoding="utf-8")
    return path


class CoverageTests(unittest.TestCase):
    def test_unprovided_evidence_is_not_run_even_on_clean_host(self):
        result = coverage.normalize_sources()
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["summary"], {
            "passed": 0, "failed": 0, "errors": 0, "not_run": 9
        })
        self.assertTrue(all(row["status"] == "not_run" for row in result["results"]))
        self.assertFalse(result["complete_acceptance"])
        self.assertEqual(result["upstream_inference"], "not_verified")

    def test_complete_lower_tiers_never_verify_live(self):
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root)
            h = save(directory / "hermetic.json", hermetic())
            p = save(directory / "pilot.json", pilot("pilot-topology"))
            o = save(directory / "runtime.json", pilot("opencode-runtime"))
            vm_root = directory / "vm"
            path = runner.write_report(
                "smoke", 0,
                runner.Provenance("a" * 40, False, "b" * 64), vm_root
            )
            combined = coverage.normalize_sources(
                hermetic_json=h, vm_run_id=path.parent.name,
                pilot_topology_json=p, opencode_runtime_json=o,
                vm_root=vm_root
            )
            self.assertEqual(combined["status"], "incomplete")
            self.assertEqual(combined["summary"], {
                "passed": 4, "failed": 0, "errors": 0, "not_run": 5
            })
            self.assertEqual(combined["results"][1]["detail"], "vm/smoke")
            self.assertEqual(
                [x["status"] for x in combined["results"][5:]],
                ["not_run"] * 4,
            )
            self.assertFalse(combined["complete_acceptance"])
            self.assertNotIn(str(directory), json.dumps(combined))

    def test_actual_report_failure_cannot_be_counted_as_pass(self):
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root)
            h = save(directory / "failed.json", hermetic(failure="lint"))
            p = save(directory / "pilot.json", pilot("pilot-topology", code=13))
            report = coverage.normalize_sources(
                hermetic_json=h, pilot_topology_json=p
            )
            self.assertEqual(report["status"], "fail")
            self.assertEqual(report["summary"]["failed"], 2)
            self.assertEqual(report["summary"]["not_run"], 7)
            self.assertEqual(report["results"][1]["status"], "not_run")

    def test_invalid_or_injected_sources_fail_closed_and_do_not_echo(self):
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root)
            sample = hermetic()
            sample["status"] = "pass"
            sample["results"][0]["id"] = "TOPSECRET_SCRIPT_INJECTION"
            bad = save(directory / "bad.json", sample)
            result = coverage.normalize_sources(hermetic_json=bad)
            self.assertEqual(result["status"], "error")
            self.assertEqual(result["results"][0]["status"], "error")
            self.assertNotIn("TOPSECRET", json.dumps(result))
            for value in (
                {"upstream_inference": "verified"},
                {"status": "pass", "summary": {"passed": 0}},
                {"profile": "live_acceptance"},
                {"results": [{"id": "live/upstream_inference", "status": "pass"}]},
            ):
                with self.subTest(value=value):
                    source = pilot("opencode-runtime")
                    source.update(value)
                    save(bad, source)
                    current = coverage.normalize_sources(opencode_runtime_json=bad)
                    self.assertEqual(current["status"], "error")
                    self.assertEqual(current["results"][3]["status"], "error")
                    self.assertTrue(all(
                        row["status"] == "not_run" for row in current["results"][4:]
                    ))

    def test_bounded_file_input_rejects_symlink_and_oversize(self):
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root)
            original = save(directory / "real.json", hermetic())
            link = directory / "alias.json"
            link.symlink_to(original)
            huge = directory / "huge.json"
            huge.write_bytes(b"x" * (coverage.MAX_INPUT_BYTES + 1))
            for path in (link, huge, directory / "missing.json"):
                with self.subTest(path=path):
                    result = coverage.normalize_sources(hermetic_json=path)
                    self.assertEqual(result["status"], "error")
                    self.assertEqual(result["results"][0]["evidence"], "invalid")
                    self.assertNotIn(str(path), json.dumps(result))

    def test_explicit_cli_report_is_one_object_and_never_acceptance_success(self):
        with tempfile.TemporaryDirectory() as root:
            path = save(Path(root) / "report.json", hermetic())
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                code = coverage.main(
                    ["--hermetic-json", str(path), "--format", "json"]
                )
            self.assertEqual(code, 1)
            self.assertEqual(len(output.getvalue().splitlines()), 1)
            doc = json.loads(output.getvalue())
            self.assertEqual(doc["summary"]["passed"], 1)
            self.assertEqual(doc["status"], "incomplete")
            self.assertFalse(doc["complete_acceptance"])
            self.assertNotIn(str(path), output.getvalue())
            with (
                patch.object(coverage.vm_evidence, "read_private_result") as vm,
                patch.object(coverage, "read_input") as reader,
                contextlib.redirect_stdout(io.StringIO()),
            ):
                self.assertEqual(coverage.main(["--format", "json"]), 1)
            vm.assert_not_called()
            reader.assert_not_called()

    def write_local_result(self, directory, **changes):
        root = Path(directory) / "acceptance-evidence"
        root.mkdir(mode=0o700, exist_ok=True)
        parameters = {
            "run_id": "aginctus-acceptance.ABCD1234",
            "mode": "mock",
            "exit_code": 0,
            "fixture": "pass",
            "tests": "pass",
            "stop": "pass",
            "close": "pass",
        }
        parameters.update(changes)
        result = local_report.record(root, **parameters)
        return root, result.parent.name, result

    def test_partial_local_result_adds_only_local_checks_not_live_evidence(self):
        with tempfile.TemporaryDirectory() as folder:
            root, run_id, result_path = self.write_local_result(folder)
            result = coverage.normalize_sources(
                local_acceptance_run_id=run_id, local_root=root
            )
            self.assertEqual(result["results"][4], {
                "id": "acceptance/local_checks",
                "status": "pass",
                "evidence": "local_report",
                "detail": "mock",
            })
            self.assertEqual(result["status"], "incomplete")
            self.assertEqual(result["summary"], {
                "passed": 1, "failed": 0, "errors": 0, "not_run": 8
            })
            self.assertEqual(
                [row["status"] for row in result["results"][5:]],
                ["not_run"] * 4,
            )
            self.assertFalse(result["complete_acceptance"])
            self.assertEqual(result["upstream_inference"], "not_verified")
            self.assertNotIn(str(result_path), json.dumps(result))

    def test_failed_local_acceptance_is_fail_even_with_passing_tests(self):
        with tempfile.TemporaryDirectory() as folder:
            root, run_id, _ = self.write_local_result(
                folder, mode="live", exit_code=1, stop="fail"
            )
            result = coverage.normalize_sources(
                local_acceptance_run_id=run_id, local_root=root
            )
            self.assertEqual(result["status"], "fail")
            self.assertEqual(result["results"][4]["status"], "fail")
            self.assertEqual(result["results"][4]["detail"], "live_partial")
            self.assertTrue(all(
                row["status"] == "not_run" for row in result["results"][5:]
            ))
            self.assertFalse(result["complete_acceptance"])

    def test_invalid_local_file_stays_error_and_never_populates_live_rows(self):
        with tempfile.TemporaryDirectory() as folder:
            root, run_id, path = self.write_local_result(folder)
            document = json.loads(path.read_text())
            for change in (
                {"upstream_inference": "verified"},
                {"complete_acceptance": True},
                {"results": [{"id": "live/upstream_inference", "status": "pass"}]},
            ):
                with self.subTest(change=change):
                    path.write_text(json.dumps({**document, **change}))
                    report = coverage.normalize_sources(
                        local_acceptance_run_id=run_id, local_root=root
                    )
                    self.assertEqual(report["status"], "error")
                    self.assertEqual(report["results"][4], {
                        "id": "acceptance/local_checks",
                        "status": "error",
                        "evidence": "invalid",
                    })
                    self.assertTrue(all(
                        row["status"] == "not_run"
                        for row in report["results"][5:]
                    ))
            path.write_text(json.dumps(document))
            path.chmod(0o644)
            report = coverage.normalize_sources(
                local_acceptance_run_id=run_id, local_root=root
            )
            self.assertEqual(report["results"][4]["status"], "error")

    def test_local_run_is_never_discovered_automatically(self):
        with tempfile.TemporaryDirectory() as folder:
            root, run_id, _ = self.write_local_result(folder)
            with (
                patch.object(coverage.local_acceptance, "read_private_result") as reader,
                patch.dict("os.environ", {
                    "AGINCTUS_ACCEPTANCE_REPORT_ROOT": str(root)
                }),
            ):
                missing = coverage.normalize_sources()
            reader.assert_not_called()
            self.assertEqual(missing["results"][4]["status"], "not_run")
            with patch.dict("os.environ", {
                "AGINCTUS_ACCEPTANCE_REPORT_ROOT": str(root)
            }):
                selected = coverage.normalize_sources(local_acceptance_run_id=run_id)
            self.assertEqual(selected["results"][4]["status"], "pass")

    def test_preflight_result_strictly_checks_selected_identity(self):
        valid = pilot("pilot-topology")
        self.assertEqual(coverage.validate_preflight(valid, "pilot-topology"), "pass")
        with self.assertRaises(ValueError):
            coverage.validate_preflight(valid, "opencode-runtime")
        for change in (
            {"target": "bad\nSECRETS"},
            {"schema_version": True},
            {"upstream_inference": "verified"},
            {"verification_tier": "live_provider"},
            {"duration_ms": -3},
        ):
            with self.subTest(change=change), self.assertRaises(ValueError):
                coverage.validate_preflight({**valid, **change}, "pilot-topology")


if __name__ == "__main__":
    unittest.main()
