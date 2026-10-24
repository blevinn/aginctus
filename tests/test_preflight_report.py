"""Hostless checks for explicitly invoked, redacted pilot preflight reporting."""

import contextlib
import io
import json
import subprocess
import unittest
from unittest.mock import patch

from scripts.checks import preflight_report


class PreflightReportTests(unittest.TestCase):
    def test_fixed_allowlist_disables_live_opt_ins_and_suppresses_output(self):
        poisoned = {
            "AGINCTUS_INCUS_INTEGRATION": "1",
            "AGINCTUS_INCUS_DISPOSABLE": "YES",
            "AGINCTUS_ACCEPTANCE_LIVE": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_LIVE_HOOKS": "/untrusted/hooks",
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_HERDR_AGENT_PROMPT": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_WORKLOAD": "safe-workload",
        }
        done = subprocess.CompletedProcess([], 0)
        with patch.object(preflight_report.subprocess, "run", return_value=done) as run:
            result = preflight_report.run_preflight("opencode-runtime", poisoned)
        self.assertEqual(result["status"], "pass")
        self.assertEqual(result["target"], "safe-workload")
        self.assertEqual(result["summary"], {"passed": 1, "failed": 0, "errors": 0})
        args, kwargs = run.call_args
        self.assertEqual(len(args[0]), 2)
        self.assertTrue(
            args[0][1].endswith("/scripts/acceptance/opencode-inference-preflight.py")
        )
        self.assertEqual(kwargs["stdin"], subprocess.DEVNULL)
        self.assertEqual(kwargs["stdout"], subprocess.DEVNULL)
        self.assertEqual(kwargs["stderr"], subprocess.DEVNULL)
        self.assertIs(kwargs["check"], False)
        self.assertEqual(kwargs["timeout"], preflight_report.TIMEOUT_SECONDS)
        for key, expected in preflight_report.SAFE_ENV.items():
            self.assertEqual(kwargs["env"][key], expected)

    def test_no_selection_or_arbitrary_script_can_run(self):
        with patch.object(
            preflight_report.subprocess, "run"
        ) as run, self.assertRaises(ValueError):
            preflight_report.run_preflight("live", {})
        run.assert_not_called()
        for args in ([], ["../../etc/passwd"]):
            with (
                self.subTest(args=args),
                patch.object(preflight_report.subprocess, "run") as run,
                contextlib.redirect_stderr(io.StringIO()),
                self.assertRaises(SystemExit),
            ):
                preflight_report.main(args)
            run.assert_not_called()

    def test_invalid_target_fails_closed_without_execution(self):
        for target in ("", "../../other", "-unsafe", "evil value", "bad\nname"):
            with self.subTest(target=target):
                with patch.object(preflight_report.subprocess, "run") as run:
                    result = preflight_report.run_preflight(
                        "opencode-runtime", {"AGINCTUS_ACCEPTANCE_WORKLOAD": target}
                    )
                self.assertEqual(result["status"], "error")
                if target:
                    self.assertNotIn(target, json.dumps(result))
                run.assert_not_called()

    def test_nonzero_is_failure_without_child_diagnostics(self):
        completed = subprocess.CompletedProcess([], 37, stdout="SECRET_CANARY")
        with patch.object(preflight_report.subprocess, "run", return_value=completed):
            record = preflight_report.run_preflight("pilot-topology", {})
        self.assertEqual(record["status"], "fail")
        self.assertEqual(record["results"][0]["exit_code"], 37)
        self.assertNotIn("SECRET_CANARY", json.dumps(record))
        self.assertEqual(record["target"], "configured_pilot")

    def test_timeout_or_launch_error_are_not_pass(self):
        for problem in (
            FileNotFoundError("SECRET_CANARY"),
            subprocess.TimeoutExpired([], 1),
        ):
            with self.subTest(problem=type(problem).__name__):
                with patch.object(preflight_report.subprocess, "run", side_effect=problem):
                    record = preflight_report.run_preflight("pilot-topology", {})
                self.assertEqual(record["status"], "error")
                self.assertIsNone(record["results"][0]["exit_code"])
                self.assertNotIn("SECRET_CANARY", json.dumps(record))

    def test_one_json_object_and_no_inference_claim(self):
        done = subprocess.CompletedProcess([], 0)
        with patch.object(preflight_report.subprocess, "run", return_value=done):
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                exit_code = preflight_report.main(["pilot-topology", "--format", "json"])
        self.assertEqual(exit_code, 0)
        self.assertEqual(len(output.getvalue().splitlines()), 1)
        document = json.loads(output.getvalue())
        self.assertEqual(document["profile"], "pilot_preflight")
        self.assertEqual(document["upstream_inference"], "not_verified")
        self.assertEqual(document["gateway_authorization"], "not_verified")
        self.assertEqual(document["egress_policy"], "not_verified")


if __name__ == "__main__":
    unittest.main()
