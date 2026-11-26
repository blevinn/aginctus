"""Hostless contract tests for direct guest fixture command semantics."""

import importlib.util
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "direct_guest",
    Path(__file__).resolve().parents[2] / "internal/testing/testenv/nix/direct-guest.py",
)
guest = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guest)


class GuestFixtureTest(unittest.TestCase):
    @patch.object(guest.subprocess, "run")
    def test_command_result_and_denial(self, run):
        fixture = guest.GuestFixture()
        run.return_value = subprocess.CompletedProcess([], 0, "ok\n", "")
        self.assertEqual(fixture.succeed("fixed command", timeout=3), "ok\n")
        self.assertEqual(
            run.call_args.args[0],
            ["/run/current-system/sw/bin/bash", "-c", "fixed command"],
        )
        self.assertEqual(run.call_args.kwargs["timeout"], 3)
        with self.assertRaises(RuntimeError):
            fixture.fail("fixed command")
        run.return_value.returncode = 1
        self.assertEqual(fixture.fail("fixed command"), "ok\n")
        with self.assertRaises(RuntimeError):
            fixture.succeed("fixed command")
        run.return_value.returncode = -9
        with self.assertRaises(RuntimeError):
            fixture.fail("fixed command")

    @patch.object(guest.subprocess, "run")
    def test_timeout_is_not_expected_denial(self, run):
        run.side_effect = subprocess.TimeoutExpired("fixed", 1)
        with self.assertRaises(subprocess.TimeoutExpired):
            guest.GuestFixture().fail("fixed", timeout=1)

    @patch.object(guest.subprocess, "run")
    def test_readiness_uses_fixed_unit(self, run):
        run.return_value = subprocess.CompletedProcess([], 0, "", "")
        fixture = guest.GuestFixture()
        fixture.wait_for_unit("incus.service")
        self.assertEqual(run.call_args.args[0][-1], "systemctl is-active --quiet incus.service")
        with self.assertRaises(RuntimeError):
            fixture.wait_for_unit("foreign.service")


if __name__ == "__main__":
    unittest.main()
