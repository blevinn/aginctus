import importlib.util
import os
import json
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("herdr_smoke", Path(__file__).with_name("herdr-smoke.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)

class SmokeSafetyTests(unittest.TestCase):
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
