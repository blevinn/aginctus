import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("inference_preflight", Path(__file__).with_name("opencode-inference-preflight.py"))
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

CONFIG = '{"endpoint":"http://litellm.internal:4000/v1","model":"approved-model","protocol":"responses"}'

class ReadinessTests(unittest.TestCase):
    def fake(self, *args):
        if args[:2] == ("stat", "-c") and args[2] == "%a %U %F":
            return "600 agent regular file\n"
        if args[:2] == ("stat", "-c"):
            return "regular file\n"
        if "cat" in args:
            return CONFIG
        return "10.42.40.4 litellm.internal\n"

    def test_phase_error_suppresses_subprocess_detail(self):
        with self.assertRaisesRegex(RuntimeError, "readiness phase failed: credential metadata") as caught:
            mod.stage("credential metadata", lambda: (_ for _ in ()).throw(RuntimeError("token secret")))
        self.assertNotIn("secret", str(caught.exception))

    def test_valid_readonly_metadata(self):
        with patch.object(mod, "guest", side_effect=self.fake) as guest:
            self.assertEqual(mod.check(), "approved-model")
            self.assertFalse(any(mod.KEY in a and "cat" in a for call in guest.call_args_list for a in [call.args]))

    def test_rejects_unsafe_key(self):
        def fake(*args):
            if args[:2] == ("stat", "-c") and args[2] == "%a %U %F":
                return "644 root regular file\n"
            return self.fake(*args)
        with patch.object(mod, "guest", side_effect=fake) as guest:
            with self.assertRaisesRegex(RuntimeError, "credential"):
                mod.check()
            self.assertEqual(guest.call_count, 1)

    def test_rejects_public_endpoint(self):
        def fake(*args):
            if "cat" in args:
                return '{"endpoint":"https://api.openai.com/v1","model":"approved","protocol":"responses"}'
            return self.fake(*args)
        with patch.object(mod, "guest", side_effect=fake), self.assertRaisesRegex(RuntimeError, "endpoint"):
            mod.check()

    def test_rejects_wrong_protocol(self):
        def fake(*args):
            if "cat" in args:
                return CONFIG.replace("responses", "chat-completions")
            return self.fake(*args)
        with patch.object(mod, "guest", side_effect=fake), self.assertRaisesRegex(RuntimeError, "Responses"):
            mod.check()

    def test_rejects_invalid_instance_without_exec(self):
        with patch.dict("os.environ", {"AGINCTUS_ACCEPTANCE_WORKLOAD":"--bad"}):
            with patch.object(mod, "guest") as guest, self.assertRaisesRegex(RuntimeError, "workload"):
                mod.check()
            guest.assert_not_called()

if __name__ == "__main__":
    unittest.main()
