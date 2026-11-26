"""Hostless tests: no QEMU, KVM or Incus invocation."""
import importlib.util
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "kvm_probe", Path(__file__).resolve().parents[2] / "internal/testing/testenv/nix/kvm_probe.py"
)
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class ProbeTest(unittest.TestCase):
    def test_fixed_boot_sector(self):
        sector = probe.boot_sector()
        self.assertEqual(len(sector), 512)
        self.assertEqual(sector[-2:], b"\x55\xaa")
        self.assertEqual(sector[:6], bytes.fromhex("baf400b02aee"))

    @patch.object(probe.subprocess, "run")
    def test_only_exact_guest_exit_passes(self, run):
        run.return_value.returncode = 85
        self.assertTrue(probe.probe("/nix/store/qemu/bin/qemu"))
        argv = run.call_args.args[0]
        self.assertIn("pc,accel=kvm", argv)
        self.assertIn("none", argv[argv.index("-nic") + 1:])
        self.assertNotIn("tcg", " ".join(argv))
        self.assertEqual(run.call_args.kwargs["timeout"], 15)
        self.assertEqual(run.call_args.kwargs["env"], {"LC_ALL": "C"})
        for code in (0, 1, -9, 84, 86):
            run.return_value.returncode = code
            self.assertFalse(probe.probe("/qemu"))

    @patch.object(probe.subprocess, "run")
    def test_error_and_timeout_fail_closed(self, run):
        self.assertFalse(probe.probe("qemu"))
        run.assert_not_called()
        run.side_effect = OSError()
        self.assertFalse(probe.probe("/qemu"))
        run.side_effect = subprocess.TimeoutExpired("/qemu", 15)
        self.assertFalse(probe.probe("/qemu"))


if __name__ == "__main__":
    unittest.main()
