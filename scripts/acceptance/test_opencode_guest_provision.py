"""Hermetic regression tests for scripts/acceptance/opencode-guest-provision.py."""
import importlib.util
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "acceptance" / "opencode-guest-provision.py"


def load_module():
    spec = importlib.util.spec_from_file_location(
        "opencode_guest_provision", SCRIPT,
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class IncusExecRetryTests(unittest.TestCase):
    def setUp(self):
        self.mod = load_module()

    def test_succeeds_on_first_attempt(self):
        completed = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="EXISTS /run/current-system/sw/bin/aginctus-opencode-gateway-provision\n",
        )
        with patch.object(self.mod.subprocess, "run", return_value=completed) as run:
            result, attempts = self.mod.incus_exec_retry("vm", "default", payload="probe")
        self.assertEqual(attempts, 1)
        self.assertIn("EXISTS", result.stdout)
        self.assertEqual(run.call_count, 1)

    def test_retries_until_marker_appears(self):
        completed_first = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="MISSING /run/current-system/sw/bin/aginctus-opencode-gateway-provision\n",
        )
        completed_second = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="EXISTS /run/current-system/sw/bin/aginctus-opencode-gateway-provision\n",
        )
        with patch.object(
            self.mod.subprocess, "run",
            side_effect=[completed_first, completed_second],
        ) as run:
            result, attempts = self.mod.incus_exec_retry(
                "vm", "default", payload="probe", attempts=5, sleep_seconds=0,
            )
        self.assertEqual(attempts, 2)
        self.assertIn("EXISTS", result.stdout)
        self.assertEqual(run.call_count, 2)

    def test_retries_transient_incus_exec_failures(self):
        # The historical bug: a transient CalledProcessError during the boot
        # window aborted the readiness loop. The new helper must absorb
        # these and continue until either the marker is reported or the
        # attempt budget is exhausted.
        transient = subprocess.CalledProcessError(
            returncode=1, cmd=["incus"], output="", stderr="",
        )
        ready = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="EXISTS /run/current-system/sw/bin/aginctus-opencode-gateway-provision\n",
        )
        with patch.object(
            self.mod.subprocess, "run",
            side_effect=[transient, ready],
        ) as run:
            result, attempts = self.mod.incus_exec_retry(
                "vm", "default", payload="probe", attempts=5, sleep_seconds=0,
            )
        self.assertEqual(attempts, 2)
        self.assertIn("EXISTS", result.stdout)
        self.assertEqual(run.call_count, 2)

    def test_retries_transient_timeout_expired(self):
        # TimeoutExpired carries no returncode; the helper must still
        # continue retrying instead of aborting.
        timeout = subprocess.TimeoutExpired(
            cmd=["incus"], timeout=120, output="", stderr="",
        )
        ready = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="EXISTS /run/current-system/sw/bin/aginctus-opencode-gateway-provision\n",
        )
        with patch.object(
            self.mod.subprocess, "run",
            side_effect=[timeout, ready],
        ) as run:
            result, attempts = self.mod.incus_exec_retry(
                "vm", "default", payload="probe", attempts=5, sleep_seconds=0,
            )
        self.assertEqual(attempts, 2)
        self.assertEqual(run.call_count, 2)

    def test_raises_after_persistent_exec_failures(self):
        # Every attempt raises; the helper must fail closed with a
        # message that mentions the attempt count.
        persistent = subprocess.CalledProcessError(
            returncode=1, cmd=["incus"], output="", stderr="",
        )
        with patch.object(
            self.mod.subprocess, "run", side_effect=persistent,
        ), self.assertRaises(RuntimeError) as ctx:
            self.mod.incus_exec_retry(
                "vm", "default", payload="probe", attempts=3, sleep_seconds=0,
            )
        self.assertIn("3 attempts", str(ctx.exception))

    def test_raises_when_marker_never_appears(self):
        # ``incus exec`` keeps succeeding but the marker never appears.
        ready = subprocess.CompletedProcess(
            args=[], returncode=0, stdout="MISSING /run/current-system/sw/bin/aginctus-opencode-gateway-provision\n",
        )
        with patch.object(
            self.mod.subprocess, "run", return_value=ready,
        ), self.assertRaises(RuntimeError) as ctx:
            self.mod.incus_exec_retry(
                "vm", "default", payload="probe", attempts=3, sleep_seconds=0,
            )
        self.assertIn("did not activate", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()