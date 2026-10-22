"""Validate that the namespaced Just CI aggregate dispatches every subrecipe.

Run the real outer Just parser but replace the inner `just` on PATH with an
inert logger. No Go/Nix/Incus command can execute via this harness.
"""

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXPECTED = [
    "check go",
    "check lint",
    "check python",
    "check shell-tests",
    "check shell",
    "check markdown",
    "check nix",
]


class JustCheckDispatchTests(unittest.TestCase):
    def run_dispatch(self, recipe):
        executable = shutil.which("just")
        if executable is None:
            self.skipTest("just is not installed")
        with tempfile.TemporaryDirectory() as tmp:
            bin_dir = Path(tmp)
            logger = bin_dir / "just"
            logger.write_text(
                "#!/bin/sh\n"
                'test "$AGINCTUS_INCUS_INTEGRATION" = 0 || exit 31\n'
                'test "$AGINCTUS_INCUS_DISPOSABLE" = NO || exit 32\n'
                'test "$GOPROXY" = off || exit 33\n'
                'test "$GOTOOLCHAIN" = local || exit 34\n'
                'test -z "$AGINCTUS_ACCEPTANCE_LIVE" || exit 35\n'
                'test -z "$AGINCTUS_ACCEPTANCE_LIVE_HOOKS" || exit 36\n'
                'printf "%s\\n" "$*" >> "$CHECK_DISPATCH_LOG"\n',
                encoding="utf-8",
            )
            logger.chmod(0o700)
            log = bin_dir / "calls"
            env = dict(os.environ)
            env["PATH"] = f"{bin_dir}{os.pathsep}{env['PATH']}"
            env["CHECK_DISPATCH_LOG"] = str(log)
            # Caller opt-ins must be overridden by the aggregate.
            env["AGINCTUS_INCUS_INTEGRATION"] = "1"
            env["AGINCTUS_INCUS_DISPOSABLE"] = "YES"
            env["AGINCTUS_ACCEPTANCE_LIVE"] = "1"
            result = subprocess.run(
                [executable, "check", recipe],
                cwd=ROOT,
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(log.read_text(encoding="utf-8").splitlines(), EXPECTED)

    def test_ci_dispatch_and_safety_override(self):
        self.run_dispatch("ci")

    def test_all_dispatches_same_suite(self):
        self.run_dispatch("all")


if __name__ == "__main__":
    unittest.main()
