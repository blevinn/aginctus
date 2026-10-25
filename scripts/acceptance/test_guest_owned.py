import importlib.util
import shlex
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("guest_owned", Path(__file__).with_name("guest-owned.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

class GuestOwnedTests(unittest.TestCase):
    def test_rejects_unsafe_paths_without_guest_exec(self):
        with patch.object(mod.subprocess, "run") as run:
            for identity, guest in [
                ("../unsafe", "/home/agent/aginctus-acceptance/../unsafe"),
                ("aginctus-acceptance.Ab12Cd34", "/home/agent/other"),
            ]:
                with self.assertRaises(ValueError):
                    mod.run("stage", "/tmp/aginctus-acceptance.Ab12Cd34/workspace", identity, guest)
            run.assert_not_called()

    def test_invokes_guest_as_agent_without_shell(self):
        identity = "aginctus-acceptance.Ab12Cd34"
        with patch.object(mod.subprocess, "run") as run:
            mod.run("stage", "/tmp/" + identity + "/workspace", identity,
                    "/home/agent/aginctus-acceptance/" + identity)
            args = run.call_args.args[0]
            self.assertEqual(args[:9], ["incus", "exec", "aginctus-dev", "--", "runuser",
                                        "-u", "agent", "--", "/bin/sh"])
            self.assertEqual(args[-3:], ["aginctus-guest-owned", "stage", identity])
            self.assertEqual(args[9], "-c")

    def test_inspect_absent_invokes_scoped_read_only_guest_command(self):
        identity = "aginctus-acceptance.Ab12Cd34"
        base = "/tmp/" + identity + "/workspace"
        guest = mod.ROOT + "/" + identity
        with patch.object(mod.subprocess, "run") as run:
            mod.run("inspect-absent", base, identity, guest)
            self.assertEqual(
                run.call_args.args[0][-3:],
                ["aginctus-guest-owned", "inspect-absent", identity],
            )
            self.assertEqual(run.call_args.kwargs["timeout"], 60)
            self.assertTrue(run.call_args.kwargs["check"])

    def test_inspect_absent_is_read_only_and_rejects_stale_paths(self):
        identity = "aginctus-acceptance.Ab12Cd34"
        with tempfile.TemporaryDirectory() as parent:
            root = Path(parent) / "guest-parent"
            root.mkdir(mode=0o700)
            root.chmod(0o700)
            # Only the test substitutes the fixed remote parent. The real
            # helper always uses the fixed /home/agent path.
            script = mod.GUEST.replace(
                "root=/home/agent/aginctus-acceptance",
                "root=" + shlex.quote(str(root)),
            )
            self.assertNotEqual(script, mod.GUEST)

            def inspect(mode):
                return subprocess.run(
                    ["/bin/sh", "-c", script, "aginctus-guest-owned", mode, identity],
                    capture_output=True, text=True, check=False, timeout=5,
                ).returncode

            target = root / identity
            unrelated = root / "someone-else"
            unrelated.mkdir()
            self.assertEqual(inspect("inspect-absent"), 0)
            self.assertEqual(inspect("stage"), 0)
            self.assertNotEqual(inspect("inspect-absent"), 0)
            self.assertTrue(target.is_dir())
            self.assertEqual(inspect("cleanup"), 0)
            self.assertEqual(inspect("inspect-absent"), 0)
            self.assertTrue(unrelated.exists())

            # A successful external no-op cleanup command cannot trick a
            # separate filesystem readback when the directory still exists.
            target.mkdir(mode=0o700)
            self.assertNotEqual(inspect("inspect-absent"), 0)
            self.assertTrue(target.is_dir())
            target.rmdir()

            target.symlink_to(root / "missing")
            self.assertNotEqual(inspect("inspect-absent"), 0)
            self.assertTrue(target.is_symlink())
            target.unlink()

            root.chmod(0o755)
            self.assertNotEqual(inspect("inspect-absent"), 0)
            root.chmod(0o700)

            moved = Path(parent) / "real-guest-parent"
            root.rename(moved)
            root.symlink_to(moved, target_is_directory=True)
            self.assertNotEqual(inspect("inspect-absent"), 0)

if __name__ == "__main__":
    unittest.main()
