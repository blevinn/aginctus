"""Hostless receipt projection tests."""
import importlib.util
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location(
    "fixture_receipt",
    Path(__file__).resolve().parents[2] / "internal/testing/testenv/nix/fixture_receipt.py",
)
receipt = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(receipt)


def report_for(scenario):
    checks = [{"id": key, "status": "pass"} for key in receipt.REQUIRED_LIFECYCLE]
    checks += [{"id": key, "status": "not_verified"} for key in receipt.UNVERIFIED]
    checks.append({"id": "native_instance_scenario", "status": "pass" if scenario else "not_verified"})
    return {
        "schema_version": 1,
        "profile": "dedicated_worker",
        "evidence_tier": "disposable_guest_no_live_provider",
        "status": "pass",
        "scenario": scenario,
        "image_fingerprint": "a" * 64,
        "results": checks,
        "handles": [{"policy_mac": "private"}],
        "unexpected": "private",
    }


class FixtureReceiptTest(unittest.TestCase):
    def test_scope_and_redaction(self):
        for scenario in ("", "native-instance"):
            result = receipt.fixture_receipt(report_for(scenario), scenario, "rev", "b" * 64, "a" * 64)
            self.assertEqual(result["status"], "pass")
            self.assertEqual(result["host_teardown"], "not_verified")
            self.assertEqual(result["agent_admission"], "not_verified")
            self.assertNotIn("private", str(result))
            self.assertNotIn("handles", result)

    def test_rejects_failed_or_mismatched_receipt(self):
        for key, value in (
            ("status", "fail"),
            ("image_fingerprint", "c" * 64),
            ("scenario", ""),
            ("profile", "hermetic"),
        ):
            report = report_for("native-instance")
            report[key] = value
            with self.assertRaises(ValueError):
                receipt.fixture_receipt(report, "native-instance", "rev", "b" * 64, "a" * 64)

    def test_rejects_missing_duplicate_or_overclaimed_checks(self):
        for mutation in ("missing", "duplicate", "overclaimed"):
            report = report_for("native-instance")
            if mutation == "missing":
                report["results"].pop(0)
            elif mutation == "duplicate":
                report["results"].append(report["results"][0])
            else:
                report["results"][-2]["status"] = "pass"
            with self.assertRaises(ValueError):
                receipt.fixture_receipt(report, "native-instance", "rev", "b" * 64, "a" * 64)


if __name__ == "__main__":
    unittest.main()
