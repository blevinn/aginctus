import importlib.util
import os
import json
import tempfile
import os.path
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("herdr_smoke", Path(__file__).with_name("herdr-smoke.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)

class SmokeSafetyTests(unittest.TestCase):
    def test_python_helper_need_not_be_executable(self):
        with tempfile.TemporaryDirectory() as temp:
            helper = Path(temp) / "helper.py"
            helper.write_text("pass\\n")
            helper.chmod(0o600)
            self.assertTrue(smoke.python_helper(str(helper)))
            self.assertFalse(smoke.python_helper(str(helper) + ".missing"))

    def test_agent_mode_is_separate_opt_in(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "",
                                     "AGINCTUS_ACCEPTANCE_HERDR_AGENT_SMOKE": "I_UNDERSTAND"}):
            with patch.object(smoke, "call") as call, self.assertRaisesRegex(RuntimeError, "opt-in"):
                smoke.main()
            call.assert_not_called()

    def test_opt_in_required_before_external_commands(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": ""}), patch.object(smoke, "call") as call:
            with self.assertRaisesRegex(RuntimeError, "opt-in"):
                smoke.main()
            call.assert_not_called()

    def test_machine_profile_required(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
                                     "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": ""}), patch.object(smoke, "call") as call:
            with self.assertRaisesRegex(RuntimeError, "machine"):
                smoke.main()
            call.assert_not_called()

    def test_guest_root_is_restricted(self):
        with patch.dict(os.environ, {"AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
                                     "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "pilot",
                                     "AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT": "/tmp/unrelated"}), patch.object(smoke, "call") as call:
            with self.assertRaisesRegex(RuntimeError, "guest workspace"):
                smoke.main()
            call.assert_not_called()

    def test_managed_machine_registry_required(self):
        with patch.dict(os.environ, {
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE": "I_UNDERSTAND",
            "AGINCTUS_ACCEPTANCE_HERDR_MACHINE": "aginctus-dev",
            "AGINCTUS_ACCEPTANCE_HERDR_SMOKE_ROOT": "/home/agent/aginctus-acceptance",
        }), patch.object(smoke, "call", return_value="[]") as call:
            with self.assertRaisesRegex(RuntimeError, "not registered"):
                smoke.main()
            self.assertEqual(call.call_args.args[:4], ("incus", "exec", "aginctus-herdr", "--"))

if __name__ == "__main__":
    unittest.main()
