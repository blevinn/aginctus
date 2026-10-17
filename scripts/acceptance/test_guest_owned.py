import importlib.util
from pathlib import Path
import unittest
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
                                        "-u", "agent", "--", "python3"])
            self.assertEqual(args[-2:], ["stage", identity])
            self.assertNotIn("sh", args)

if __name__ == "__main__":
    unittest.main()
